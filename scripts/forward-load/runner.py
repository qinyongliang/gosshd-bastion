#!/usr/bin/env python3
"""Real server/Agent processes on isolated veth IPs; no production endpoints."""
import argparse, concurrent.futures, hashlib, http.client, http.server, json, os
from pathlib import Path
import signal, socket, subprocess, sys, tempfile, time, uuid

class Backend(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        count = int(self.path.split("?", 1)[1].split("=", 1)[1])
        self.send_response(201)
        self.send_header("Content-Length", str(count))
        self.send_header("X-Body-SHA256", hashlib.sha256(body).hexdigest())
        self.send_header("X-Observed-Path", self.path)
        self.end_headers(); self.wfile.write(b"r" * count)
    def log_message(self, *_): pass

class API:
    def __init__(self, host, port): self.host, self.port, self.cookie = host, port, ""
    def request(self, method, path, data=None):
        conn = http.client.HTTPConnection(self.host, self.port, timeout=15)
        try:
            conn.request(method, path, None if data is None else json.dumps(data).encode(),
                         {"Content-Type":"application/json", "Cookie":self.cookie})
            response = conn.getresponse()
            if response.getheader("Set-Cookie"): self.cookie = response.getheader("Set-Cookie").split(";", 1)[0]
            body = response.read()
            if response.status >= 400: raise RuntimeError(f"API {method} {path}: {response.status} {body[:512]!r}")
            return json.loads(body) if body else None
        finally: conn.close()

def wait_until(predicate, timeout, description):
    deadline = time.monotonic()+timeout; error = None
    while time.monotonic() < deadline:
        try:
            result = predicate()
            if result: return result
        except Exception as exc: error = str(exc)
        time.sleep(.15)
    raise RuntimeError(f"timeout waiting for {description}; last error={error}")

def percentile(values, fraction):
    values = sorted(values)
    return values[min(len(values)-1, int((len(values)-1)*fraction))] if values else 0

def load_phase(name, host, port, concurrency, requests, size, fresh=False):
    jobs = [list(range(worker, requests, concurrency)) for worker in range(concurrency)]
    upload = b"upload"*128; digest = hashlib.sha256(upload).hexdigest(); expected = b"r"*size
    def worker(indices):
        times, errors, conn = [], [], None
        try:
            for index in indices:
                if conn is None: conn = http.client.HTTPConnection(host, port, timeout=20)
                path = f"/api/{name}/{index}?size={size}"; start = time.perf_counter()
                try:
                    headers = {"Host":"load.test", "Content-Type":"application/octet-stream"}
                    if fresh: headers["Connection"] = "close"
                    conn.request("POST", path, upload, headers); response = conn.getresponse(); body = response.read()
                    if (response.status != 201 or body != expected or response.getheader("X-Body-SHA256") != digest
                        or response.getheader("X-Observed-Path") != path[4:]):
                        raise RuntimeError(f"wrong status/body/hash/path: {response.status}, bytes={len(body)}")
                    times.append((time.perf_counter()-start)*1000)
                    if fresh: conn.close(); conn = None
                except Exception as exc:
                    errors.append({"request":index,"error":str(exc)}); conn.close(); conn = None
        finally:
            if conn: conn.close()
        return times, errors
    started = time.perf_counter(); latencies, errors = [], []
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        for times, failures in pool.map(worker, jobs): latencies.extend(times); errors.extend(failures)
    duration = time.perf_counter()-started
    return {"name":name,"concurrency":concurrency,"requests":requests,"success":len(latencies),"failures":len(errors),
            "first_errors":errors[:10],"body_bytes":size,"fresh_client_connections":fresh,"elapsed_seconds":duration,
            "rps":len(latencies)/duration,"MiB_per_second":len(latencies)*size/duration/1024**2,
            "p50_ms":percentile(latencies,.5),"p95_ms":percentile(latencies,.95),"p99_ms":percentile(latencies,.99),
            "expected_up":len(latencies)*len(upload),"expected_down":len(latencies)*size}

class Lab:
    def __init__(self, args):
        self.args = args; self.tag = "gf"+uuid.uuid4().hex[:6]
        self.codes={"hub":"h","entry":"a","exit":"z"}
        self.names = {role:self.tag+code for role,code in self.codes.items()}
        self.procs, self.handles, self.owned_names = [], [], []
        self.labels = {}
        self.root = Path(tempfile.mkdtemp(prefix="gosshd-forward-"))
        self.results = Path(args.results).resolve(); self.results.mkdir(parents=True,exist_ok=True)
        self.report = {"schema":1,"environment":{"platform":sys.platform,"kernel":os.uname().release,"cpu_count":os.cpu_count(),
                       "topology":"isolated veth bridge; distinct real Agent processes/namespaces","backend":"Go HTTP fixture" if args.backend_binary else "Python stdlib","load_generator":"Python stdlib ThreadPoolExecutor"},"phases":[],"checks":{}}
        self.server = None
    def cmd(self,*args,namespace=None,check=True):
        argv = ["ip","netns","exec",namespace,*args] if namespace else list(args)
        result=subprocess.run(argv,text=True,capture_output=True)
        if check and result.returncode: raise RuntimeError(f"command {argv}: {result.stderr.strip()}")
        return result
    def setup(self):
        if os.geteuid()!=0 or len(json.loads(self.cmd("ip","-j","addr").stdout))!=1:
            raise RuntimeError("refusing setup unless root in an otherwise empty outer network namespace")
        self.cmd("ip","link","set","lo","up"); self.bridge = self.tag+"b"
        self.cmd("ip","link","add",self.bridge,"type","bridge")
        self.cmd("ip","addr","add","10.231.42.254/24","dev",self.bridge); self.cmd("ip","link","set",self.bridge,"up")
        for number,(role,name) in enumerate(self.names.items(),1):
            self.cmd("ip","netns","add",name); self.owned_names.append(name)
            outside,inside = self.tag+self.codes[role]+"o", self.tag+self.codes[role]+"i"
            self.cmd("ip","link","add",outside,"type","veth","peer","name",inside)
            self.cmd("ip","link","set",outside,"master",self.bridge); self.cmd("ip","link","set",outside,"up")
            self.cmd("ip","link","set",inside,"netns",name)
            self.cmd("ip","link","set",inside,"name","eth0",namespace=name)
            self.cmd("ip","addr","add",f"10.231.42.{number}/24","dev","eth0",namespace=name)
            self.cmd("ip","link","set","eth0","up",namespace=name); self.cmd("ip","link","set","lo","up",namespace=name)
        self.hub,self.entry,self.exit = "10.231.42.1","10.231.42.2","10.231.42.3"
        self.spawn("backend",self.names["exit"],[str(Path(self.args.backend_binary).resolve())] if self.args.backend_binary else [sys.executable,str(Path(__file__).resolve()),"--backend"])
    def spawn(self,label,namespace,argv):
        handle = (self.results/(label+".log")).open("ab"); self.handles.append(handle)
        process = subprocess.Popen(["ip","netns","exec",namespace,*argv],cwd=self.root,stdout=handle,
                    stderr=subprocess.STDOUT,start_new_session=True)
        self.procs.append(process); self.labels[label]=process; return process
    def start_server(self):
        self.server = self.spawn("server",self.names["hub"],[str(Path(self.args.server).resolve()),
            "-http-listen","0.0.0.0:18280","-ssh-listen","0.0.0.0:18222","-database-path",str(self.root/"server.db"),
            "-host-key-path",str(self.root/"host.key"),"-bootstrap-admin-password","load-only-password","-tunnel-stun-servers",""])
        def listening():
            with socket.create_connection((self.hub,18280),timeout=1): return True
        wait_until(listening,30,"server listener"); self.api = API(self.hub,18280)
        self.api.request("POST","/api/auth/login",{"email":"admin","password":"load-only-password"})
    def configure(self):
        me = self.api.request("GET","/api/me"); self.org = me["organizations"][0]["id"]
        for role in ("entry","exit"):
            token = self.api.request("POST","/api/agent-enrollments",{"owner_type":"organization","owner_id":self.org,"label":"load-"+role})["token"]
            directory = self.root/role; directory.mkdir()
            self.spawn(role,self.names[role],[str(Path(self.args.agent).resolve()),"-server",f"http://{self.hub}:18280",
                 "-enrollment-token",token,"-id-file",str(directory/"agent.json"),"-root",str(directory)])
        def ready():
            files = [self.root/role/"agent.json" for role in ("entry","exit")]
            return all(file.exists() and json.loads(file.read_text()).get("target_id") for file in files)
        wait_until(ready,45,"both enrolled Agents")
        ids = {role:json.loads((self.root/role/"agent.json").read_text())["target_id"] for role in ("entry","exit")}
        config = {"organization_id":self.org,"protocol":"http","name":"isolated-load","entry_target_id":ids["entry"],
            "listen_host":"0.0.0.0","listen_port":18080,"duration_seconds":0,"http":{"servers":[{"id":"site","name":"load",
            "hosts":["load.test","alias.test"],"locations":[{"id":"api","name":"API","match":"^~","path":"/api/",
              "exit_target_id":ids["exit"],"scheme":"http","host":"127.0.0.1","port":19090,"strip_prefix":True,"preserve_host":False}]}]}}
        self.config = self.api.request("POST","/api/tunnels",config)["tunnel"]
        self.api.request("POST","/api/tunnels/"+self.config["id"]+"/enable",{})
        wait_until(lambda:self.status().get("transport")=="direct",45,"real WebRTC direct path")
        self.report["checks"]["selected_path"] = self.status().get("paths",[])
    def status(self):
        return next(t for t in self.api.request("GET","/api/tunnels?organization_id="+self.org)["tunnels"] if t["id"]==self.config["id"])
    def logs(self): return self.api.request("GET","/api/tunnels/"+self.config["id"]+"/logs?limit=500")
    def phase(self,name,concurrency,requests,size,fresh=False):
        before=self.process_samples()
        result = load_phase(name,self.entry,18080,concurrency,requests,size,fresh)
        after=self.process_samples();result["resources"]={}
        for label,sample in after.items():
            previous=before.get(label,{})
            result["resources"][label]={**sample,"cpu_seconds":max(0,sample["cpu_total_seconds"]-previous.get("cpu_total_seconds",sample["cpu_total_seconds"]))}
        result["transport_after"] = self.status().get("transport") if self.server and self.server.poll() is None else None
        self.report["phases"].append(result); print(json.dumps(result),flush=True)
        if result["failures"]: raise RuntimeError(f"phase {name}: {result['failures']} failed requests")
    def process_samples(self):
        samples={}
        for label,process in self.labels.items():
            try:
                fields=Path(f"/proc/{process.pid}/stat").read_text().split();status=Path(f"/proc/{process.pid}/status").read_text().splitlines()
                rss=next(int(line.split()[1]) for line in status if line.startswith("VmRSS:"))
                samples[label]={"pid":process.pid,"cpu_total_seconds":(int(fields[13])+int(fields[14]))/os.sysconf("SC_CLK_TCK"),"rss_kib":rss,"threads":int(fields[19])}
            except (OSError,StopIteration):pass
        return samples
    def impairment(self,enabled):
        namespace = self.names["entry"]
        if enabled:
            # ONLY UDP to the other Agent is dropped. Control TCP remains healthy.
            self.cmd("tc","qdisc","add","dev","eth0","root","handle","1:","prio",namespace=namespace)
            self.cmd("tc","qdisc","add","dev","eth0","parent","1:3","handle","30:","netem","loss","100%",namespace=namespace)
            self.cmd("tc","filter","add","dev","eth0","protocol","ip","parent","1:","prio","1","u32",
                     "match","ip","dst",self.exit+"/32","match","ip","protocol","17","0xff","flowid","1:3",namespace=namespace)
        else: self.cmd("tc","qdisc","del","dev","eth0","root",namespace=namespace)
    def stop_server(self):
        os.killpg(self.server.pid,signal.SIGTERM); self.server.wait(timeout=20)
    def run(self):
        self.setup(); self.start_server(); self.configure()
        for size in (1024,64*1024):
            for concurrency in self.args.concurrency: self.phase(f"direct-{size}-{concurrency}",concurrency,self.args.requests,size)
        self.impairment(True)
        wait_until(lambda:self.status().get("transport") in ("relay","negotiating") and self.status().get("status")=="running",40,"direct loss / relay fallback")
        self.phase("relay-fallback",32,min(self.args.requests,256),64*1024,True)
        self.impairment(False); wait_until(lambda:self.status().get("transport")=="direct",60,"direct recovery")
        self.phase("direct-recovered",32,min(self.args.requests,256),64*1024,True)
        self.stop_server(); self.phase("server-outage",32,min(self.args.requests,256),64*1024,True)
        self.start_server(); wait_until(lambda:self.status().get("status")=="running",45,"control recovery")
        self.phase("server-recovered",32,min(self.args.requests,256),1024,True)
        total = sum(p["success"] for p in self.report["phases"])
        up = sum(p["expected_up"] for p in self.report["phases"]); down = sum(p["expected_down"] for p in self.report["phases"])
        def accounted():
            data = self.logs(); stats = data["stats"]
            return data if stats["requests"]==total and stats["up"]==up and stats["down"]==down and data["active"]==0 else None
        data = wait_until(accounted,90,"exact accounting and outbox replay")
        self.report["checks"].update({"accounting":data["stats"],"edge_stats":data.get("edge_stats"),"expected_requests":total,"expected_up":up,"expected_down":down})
        ids, paths, before = set(),set(),0
        while True:
            data = self.api.request("GET",f"/api/tunnels/{self.config['id']}/logs?limit=500&before={before}"); events = data["events"]
            if not events: break
            for event in events:
                if event["id"] in ids or event["status"]!=201 or event["route_id"]!="api": raise RuntimeError("duplicate/wrong access row")
                ids.add(event["id"]); paths.add(event["path"])
            before = events[-1]["sequence"]
        self.report["checks"]["access_rows"] = len(ids)
        self.report["checks"]["outage_paths_reported"] = any("/server-outage/" in path for path in paths)
        if len(ids)!=total or not self.report["checks"]["outage_paths_reported"]: raise RuntimeError("missing access rows or outage replay")
        self.report["passed"] = True
    def cleanup(self):
        for process in reversed(self.procs):
            if process.poll() is None:
                try: os.killpg(process.pid,signal.SIGTERM)
                except ProcessLookupError: pass
        for process in reversed(self.procs):
            try: process.wait(timeout=8)
            except subprocess.TimeoutExpired: os.killpg(process.pid,signal.SIGKILL); process.wait(timeout=5)
        for handle in self.handles: handle.close()
        for name in reversed(self.owned_names): self.cmd("ip","netns","delete",name,check=False)
        self.report["run_directory"] = str(self.root)
        (self.results/"results.json").write_text(json.dumps(self.report,indent=2))

def main():
    parser=argparse.ArgumentParser(); parser.add_argument("--backend",action="store_true")
    parser.add_argument("--backend-binary"); parser.add_argument("--server"); parser.add_argument("--agent"); parser.add_argument("--results")
    parser.add_argument("--requests",type=int,default=512); parser.add_argument("--concurrency",type=int,nargs="+",default=[16,64,128])
    args=parser.parse_args()
    if args.backend: http.server.ThreadingHTTPServer(("127.0.0.1",19090),Backend).serve_forever(); return
    if not args.server or not args.agent or not args.results: parser.error("--server, --agent and --results required")
    lab=Lab(args)
    try: lab.run()
    except Exception as exc: lab.report.update({"passed":False,"error":str(exc)}); raise
    finally: lab.cleanup()
if __name__=="__main__": main()
