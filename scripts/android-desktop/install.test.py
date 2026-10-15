"""Host-side routing tests with isolated id/su/package/install mocks."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).parent

class InstallRouting(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="gosshd-routing-")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for name in ["sh", "dirname", "basename", "sed", "mkdir", "chmod", "cat"]:
            (self.bin / name).symlink_to(shutil.which(name))
        self.tool("id", 'printf "%s\\n" "$TEST_UID"')
        self.tool("timeout", 'shift; exec "$@"')
        self.tool("su", 'if [ "$2" = "id -u" ];then case "$TEST_SU" in granted) echo 0;; denied) exit 1;; timeout) exit 124;; esac;else export TEST_UID=0; exec /bin/sh -c "$2";fi')
        self.tool("termux-open", 'printf "opened\\n" >> "$TEST_OPEN"')
        self.home = self.root / "home 'quoted space"
        (self.home / ".gosshd").mkdir(parents=True)
        (self.home / ".gosshd/desktop-pairing.token").write_text("a" * 43)
        self.installer = self.root / "agent 'quoted installer.sh"
        self.installer.write_text('''#!/bin/sh
printf '%s|%s|%s|%s' "$GOSSHD_DESKTOP_ENABLE" "$GOSSHD_ANDROID_BACKEND" "${GOSSHD_ANDROID_BRIDGE_TOKEN_FILE:-}" "$1" > "$TEST_RECORD"
''')
        self.apk = self.root / "companion.apk"
        self.apk.write_bytes(b"synthetic apk fixture")
        self.env = dict(os.environ, PATH=str(self.bin), HOME=str(self.home), PREFIX=str(self.bin.parent), TEST_UID="10001", TEST_SU="denied", TEST_RECORD=str(self.root / "record"), TEST_OPEN=str(self.root / "open"))

    def tool(self, name, script):
        target = self.bin / name
        target.write_text("#!/bin/sh\nset -eu\n" + script + "\n")
        target.chmod(0o700)

    def run_install(self, uid="10001", su="denied", requested="auto"):
        env = dict(self.env, TEST_UID=uid, TEST_SU=su, GOSSHD_ANDROID_INSTALL_MODE=requested)
        return subprocess.run(["/bin/sh", str(SOURCE / "install.sh"), str(self.installer), str(self.apk), "install"], env=env, capture_output=True, text=True, timeout=10)

    def assert_backend(self, result, backend):
        self.assertEqual(result.returncode, 0, result.stderr)
        record = (self.root / "record").read_text().split("|")
        self.assertEqual(record[0:2], ["1", backend])
        self.assertEqual(record[-1], "install")

    def test_existing_root(self):
        self.assert_backend(self.run_install(uid="0"), "shell")
    def test_su_granted_and_quoted_paths(self):
        self.assert_backend(self.run_install(su="granted"), "shell")
    def test_su_denied_uses_auxiliary(self):
        self.assert_backend(self.run_install(), "accessibility")
    def test_su_timeout_uses_auxiliary(self):
        self.assert_backend(self.run_install(su="timeout"), "accessibility")
    def test_missing_su_uses_auxiliary(self):
        (self.bin / "su").unlink()
        self.assert_backend(self.run_install(), "accessibility")
    def test_explicit_auxiliary_does_not_escalate(self):
        self.assert_backend(self.run_install(su="granted", requested="accessibility"), "accessibility")
    def test_explicit_root_fails_on_refusal(self):
        result = self.run_install(requested="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "record").exists())
    def test_noninteractive_missing_token_fails(self):
        (self.home / ".gosshd/desktop-pairing.token").unlink()
        result = self.run_install()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "record").exists())


class AdbActivation(unittest.TestCase):
    def test_preserves_existing_services_and_is_idempotent(self):
        with tempfile.TemporaryDirectory(prefix="gosshd-adb-activation-") as temp:
            root = Path(temp)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            state = root / "services"
            state.write_text("com.example.other/.Service")
            settings = bin_dir / "settings"
            settings.write_text('#!/bin/sh\nset -eu\nif [ "$1" = get ]; then /bin/cat "$TEST_SETTINGS"; elif [ "$3" = enabled_accessibility_services ];then printf "%s" "$4" > "$TEST_SETTINGS";fi\n')
            settings.chmod(0o700)
            for name in ["pm", "am"]:
                tool = bin_dir / name
                tool.write_text("#!/bin/sh\nexit 0\n")
                tool.chmod(0o700)
            env = dict(os.environ, PATH=str(bin_dir), TEST_SETTINGS=str(state))
            for _ in range(2):
                result = subprocess.run(["/bin/sh", str(SOURCE / "activate-adb.sh")], env=env, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(state.read_text(), "com.example.other/.Service:com.gosshd.desktop/com.gosshd.desktop.access.DesktopAccessibilityService")

if __name__ == "__main__":
    unittest.main()
