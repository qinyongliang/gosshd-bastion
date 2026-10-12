# Existing deployment procedure: verify archive, snapshot config, backup program/DBs, rollback on failure.
import hashlib
import http.client
import json
import os
import re
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import sys
import tarfile
import time
import urllib.parse
import urllib.request

os.umask(0o077)
tag, platform, container, binary_name, data_name, health_url = sys.argv[1:]
assert re.fullmatch(r"v0[.]1[.][0-9]+-bastion", tag), "invalid release tag"
assert platform in ("linux-amd64", "linux-arm64")
binary = Path(binary_name)
data = Path(data_name)
release = binary.parent / ("release-" + tag)
release.mkdir(exist_ok=True)
archive_name = "gosshd-" + tag + "-" + platform + ".tar.gz"
base_url = "https://gh-proxy.com/https://github.com/qinyongliang/gosshd-bastion/releases/download/" + tag + "/"
for filename in (archive_name, "checksums.txt"):
    subprocess.run(["curl", "-fL", "--connect-timeout", "15", "--max-time", "180", "--retry", "2", "--silent", "--show-error", base_url + filename, "-o", str(release / filename)], check=True)
expected = None
for line in (release / "checksums.txt").read_text().splitlines():
    parts = line.split()
    if len(parts) == 2 and parts[1].lstrip("*") == archive_name:
        expected = parts[0]
actual = hashlib.sha256((release / archive_name).read_bytes()).hexdigest()
assert expected and actual == expected, "Release archive checksum mismatch"
print("Verified release archive SHA-256:", actual, flush=True)
new_binary = binary.with_name(binary.name + "." + tag + ".new")
with tarfile.open(release / archive_name, "r:gz") as archive:
    member = archive.getmember("gosshd-" + platform + "/gosshd-server")
    assert member.isfile()
    with archive.extractfile(member) as src, new_binary.open("wb") as dst:
        shutil.copyfileobj(src, dst)
os.chmod(new_binary, 0o755)
new_hash = hashlib.sha256(new_binary.read_bytes()).hexdigest()
inspection = json.loads(subprocess.check_output(["docker", "inspect", container]))[0]
snapshot_file = release / "container-before.json"
if not snapshot_file.exists():
    snapshot_file.write_text(json.dumps(inspection, indent=2))
backup_binary = release / "gosshd-server.before"
shutil.copy2(binary, backup_binary)
database_backups = []

class DockerConnection(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect("/var/run/docker.sock")

def docker_create(name, payload):
    connection = DockerConnection("localhost", timeout=30)
    connection.request("POST", "/containers/create?name=" + urllib.parse.quote(name), json.dumps(payload), {"Content-Type": "application/json"})
    response = connection.getresponse()
    body = response.read()
    connection.close()
    if response.status != 201:
        raise RuntimeError("Docker create failed: " + str(response.status) + " " + body.decode())

config = inspection["Config"]
command = config["Cmd"]
if "--version" in command:
    command[command.index("--version") + 1] = tag
else:
    command += ["--version", tag]
payload = dict(config)
payload["HostConfig"] = inspection["HostConfig"]
rollback_name = container + ".rollback-" + tag
renamed = False
created = False
try:
    subprocess.run(["docker", "stop", container], check=True)
    for database in data.glob("*.db"):
        backup = release / (database.name + ".before")
        with sqlite3.connect("file:" + str(database) + "?mode=ro", uri=True) as src:
            if hasattr(src, "backup"):
                with sqlite3.connect(str(backup)) as dst:
                    src.backup(dst)
            else:
                subprocess.run(["sqlite3", str(database), ".backup '" + str(backup) + "'"], check=True)
        database_backups.append((database, backup))
    print("Backed up binary, container configuration, and", len(database_backups), "databases", flush=True)
    os.replace(new_binary, binary)
    subprocess.run(["docker", "rename", container, rollback_name], check=True)
    renamed = True
    docker_create(container, payload)
    created = True
    subprocess.run(["docker", "start", container], check=True)
    for attempt in range(30):
        try:
            with urllib.request.urlopen(health_url, timeout=3) as response:
                if response.read().strip() == b"ok":
                    break
        except Exception:
            pass
        time.sleep(1)
    else:
        raise RuntimeError("Health check failed after deployment")
    providers_url = health_url.rsplit("/healthz", 1)[0] + "/api/auth/providers"
    with urllib.request.urlopen(providers_url, timeout=15) as response:
        providers = json.loads(response.read())
    assert isinstance(providers, dict) and "branding" in providers, "Application initialization failed"
    running = json.loads(subprocess.check_output(["docker", "inspect", container]))[0]
    assert running["State"]["Running"]
    assert running["Config"]["Cmd"][running["Config"]["Cmd"].index("--version") + 1] == tag
    assert hashlib.sha256(binary.read_bytes()).hexdigest() == new_hash
    print("Deployed", tag, platform, "binary SHA-256:", new_hash, flush=True)
    print("Container running; health check:", health_url, "ok", flush=True)
except Exception:
    if created:
        subprocess.run(["docker", "stop", container], check=False)
        subprocess.run(["docker", "rm", container], check=False)
    shutil.copy2(backup_binary, new_binary)
    os.replace(new_binary, binary)
    for database, backup in database_backups:
        for suffix in ("-wal", "-shm"):
            sidecar = Path(str(database) + suffix)
            if sidecar.exists():
                sidecar.unlink()
        shutil.copy2(backup, database)
    if renamed:
        subprocess.run(["docker", "rename", rollback_name, container], check=True)
    subprocess.run(["docker", "start", container], check=True)
    print("Rolled back binary, databases, and container", flush=True)
    raise
