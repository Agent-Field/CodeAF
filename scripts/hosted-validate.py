#!/usr/bin/env python3
"""The Stage 1 loop on the hosted relay, with every number written down.

  scripts/hosted-validate.py <pass number> [--repo r02-mj-base] [--out DIR]

One pass: pair two machines (spark and `ssh dumb`) through the relay in CODEAF_SYNC_URL, work in a
real chat with a live model, time the instant save of each tool call, move the chat to the other
machine (a cold take through the home screen, a warm take and a take back through the same take the
home screen runs), capture the resume card, and time how fast a new chat reaches the other machine's
home screen. Then the integrity checks on the trees the pass produced. Everything lands in
<out>/pass<N>/ as raw files plus results.json.

It makes REAL MODEL CALLS (deepseek/deepseek-v4.1-flash for every role, keys from each machine's own
login environment, never read or printed here) and talks to the staging relay. It takes the lock
~/caf-bench.lock on the Mac while it runs, because its numbers are timings.

Roots: spark ~/caf-vdemo-rig, dumb ~/caf-vdemo-rig (home/, bin/, work/). Binaries and the helper
programs (s1probe, codeaf-vd) are put there by hand beforehand; see docs/BENCH-MOVE.md.
"""
import argparse
import json
import os
import re
import shlex
import statistics
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
HELP = os.path.join(HERE, "measure", "hosted")
sys.path.insert(0, HELP)
import cellread  # noqa: E402
import durable  # noqa: E402

CORPUS = "/home/santosh/codeaf-prototype/stage-k/corpus"
A_R = "/home/santosh/caf-vdemo-rig"
B_HOST = "dumb"
B_R = "/Users/santoshkumarradha/caf-vdemo-rig"
CTL = "/tmp/vd-ssh"
B_PRE = "export PATH=/opt/homebrew/bin:$PATH; "
MODEL = "deepseek/deepseek-v4.1-flash"


# ---------------------------------------------------------------- machines ----

def sh(cmd, check=True, input=None, timeout=None):
    r = subprocess.run(["bash", "-lc", cmd], capture_output=True, text=True, input=input, timeout=timeout)
    if check and r.returncode:
        raise RuntimeError(f"spark: {cmd[:160]} -> {r.returncode}: {r.stderr[-400:]}")
    return r


def bsh(cmd, check=True, timeout=None):
    r = subprocess.run(["ssh", "-o", f"ControlPath={CTL}", "-o", "BatchMode=yes", B_HOST, "bash -l -s"],
                       capture_output=True, text=True, input=B_PRE + cmd + "\n", timeout=timeout)
    if check and r.returncode:
        raise RuntimeError(f"dumb: {cmd[:160]} -> {r.returncode}: {r.stderr[-400:]}")
    return r


class Box:
    """One machine: where it keeps the rig, how to run a script on it and which tmux server is its own."""

    def __init__(self, name, root, run, tmux_sess):
        self.name, self.root, self.run, self.sess = name, root, run, tmux_sess

    def env(self, cmd):
        return f". {self.root}/env.sh; {cmd}"

    def sh(self, cmd, **k):
        return self.run(self.env(cmd), **k)

    def drive(self, *args, timeout=None):
        """tuidrive on this machine, against this machine's tmux server, parsed."""
        line = " ".join(shlex.quote(a) for a in args)
        out = self.run(f"VD_TMUX={self.sess} python3 {self.root}/bin/tuidrive.py {line}", timeout=timeout).stdout
        return json.loads(out.strip().splitlines()[-1]) if out.strip() else {}

    def cell_dir(self, workname):
        out = self.run(f"ls -td {self.root}/home/v3/projects/*{workname}/*/.cell 2>/dev/null | head -1").stdout.strip()
        return out

    def tmux(self, *args):
        return self.run(f"tmux -L {self.sess} " + " ".join(shlex.quote(a) for a in args), check=False).stdout


A = Box("spark", A_R, sh, "vda")
B = Box("dumb", B_R, bsh, "vdb")


def log(*a):
    print(time.strftime("%T"), *a, file=sys.stderr, flush=True)


class Pass:
    def __init__(self, n, repo, out):
        self.n, self.repo = n, repo
        self.dir = os.path.join(out, f"pass{n}")
        os.makedirs(self.dir, exist_ok=True)
        self.res = {"pass": n, "repo": repo, "model": MODEL, "started": time.strftime("%FT%TZ", time.gmtime())}
        self.port = 8765 + n

    def save(self, name, text):
        with open(os.path.join(self.dir, name), "w") as f:
            f.write(text if isinstance(text, str) else json.dumps(text, indent=1))

    def timed(self, key, fn):
        t0 = time.time()
        v = fn()
        self.res[key + "_s"] = round(time.time() - t0, 3)
        log(f"{key}: {self.res[key + '_s']} s")
        return v

    # ---- cleanliness: only processes whose command line is a binary of this rig ----

    def stop_rig_processes(self, box):
        """End the chat, its engine daemon and furrow, found by the rig's own binary paths and
        recorded here, never by a loose pattern."""
        box.tmux("kill-server")
        pat = f"^{box.root}/(bin/codeaf|home/bin/furrow)"
        if box is A:
            pids = sh(f"pgrep -u $(id -u) -f '{pat}' || true").stdout.split()
        else:
            pids = bsh(f"ps -axo pid=,command= | grep -E '{pat.replace('^', '')}' | grep -v grep | awk '{{print $1}}'").stdout.split()
        for pid in pids:
            box.run(f"kill {pid}", check=False)
        time.sleep(1)

    def clean(self):
        for box in (A, B):
            self.stop_rig_processes(box)
        sh(f"bash {HELP}/setup.sh a")
        sh(f"cp {HELP}/tuidrive.py {HELP}/cellread.py {HELP}/treehash.py {A_R}/bin/")
        helpers = [f"{HELP}/{f}" for f in ("setup.sh", "tuidrive.py", "cellread.py", "treehash.py")]
        subprocess.run(["scp", "-q", "-o", f"ControlPath={CTL}", *helpers, f"{B_HOST}:/tmp/"], check=True)
        bsh(f"bash /tmp/setup.sh b; mkdir -p {B_R}/bin; cp /tmp/tuidrive.py /tmp/cellread.py /tmp/treehash.py {B_R}/bin/")

    # ---- pairing: the staging relay keeps a mailbox 4 s, so the handshake is scripted ----

    def pair(self):
        sh("tmux -L vdp kill-server", check=False)
        t0 = time.time()
        sh(f"tmux -L vdp new-session -d -s vdp -x 150 -y 30 \"bash -lc '. {A_R}/env.sh; codeaf pair; echo EXIT=\\$?; sleep 120'\"")
        code = None
        while time.time() - t0 < 20:
            m = re.search(r"codeaf pair (\d\d-\d\d\d-\d\d\d)", sh("tmux -L vdp capture-pane -p -t vdp").stdout)
            if m:
                code = m.group(1)
                break
            time.sleep(0.03)
        assert code, "no code shown"
        t_code = time.time()
        join = subprocess.Popen(["ssh", "-o", f"ControlPath={CTL}", B_HOST, f"bash -l -c '. {B_R}/env.sh; codeaf pair {code}'"],
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        answered = None
        while time.time() - t_code < 30:
            out = sh("tmux -L vdp capture-pane -p -t vdp").stdout
            if "wants your chats" in out and answered is None:
                sh("tmux -L vdp send-keys -t vdp y Enter")
                answered = time.time() - t_code
            if "EXIT=" in out:
                break
            time.sleep(0.03)
        jout = join.communicate(timeout=30)[0]
        a_out = sh("tmux -L vdp capture-pane -p -t vdp").stdout
        sh("tmux -L vdp kill-server", check=False)
        self.save("pair.txt", f"A:\n{a_out}\nB:\n{jout}\n")
        ok = "paired" in a_out and "paired." in jout
        self.res["pair_s"] = round(time.time() - t0, 3)
        self.res["pair_ok"] = ok
        log("pair", self.res["pair_s"], "s ok" if ok else "FAILED")
        assert ok, "pairing failed: " + a_out + jout

    # ---- the workspace and the chats ----

    def fixture(self):
        w = f"{A_R}/work/m"
        # the corpus copies carry a `.codeaf/` of their own from earlier runs; it goes, so that any `.codeaf/` in a
        # git status afterwards is the product's own leak
        sh(f"cp -a {CORPUS}/{self.repo} {w} && rm -rf {w}/.furrow {w}/.codeaf")
        # a rebuildable install and its lock, there before the chat starts, as in a real project
        sh(f"cd {w} && printf '{{\"name\":\"vdemo\",\"version\":\"1.0.0\",\"dependencies\":{{\"left-pad\":\"1.3.0\"}}}}\n' > package.json && npm install --no-audit --no-fund 2>&1 | tail -2")
        sh(f"""cd {w}; printf '# demo secrets, keep order\\nZED=last\\n\\n# block two\\nALPHA="quoted # not a comment"\\nMID=1' > .env; chmod 600 .env
mkdir -p svc/api web/client/app; printf 'API_TOKEN=nested-1\\n# nested\\nAAA=2\\n' > svc/api/.env; chmod 640 svc/api/.env
printf 'ZZ=deep-3\\nBB=4\\n' > web/client/app/.env.local; chmod 600 web/client/app/.env.local
printf '#!/bin/sh\\necho run\\n' > run.sh; chmod 755 run.sh""")
        bsh(f"mkdir -p {B_R}/work/b && cd {B_R}/work/b && git init -q && echo b > README.md && git add . && git -c user.name=v -c user.email=v@x commit -qm init")
        self.save("a-fixture-status.txt", sh(f"cd {w}; git status --porcelain; ls -la .env svc/api/.env web/client/app/.env.local run.sh").stdout)

    def start_chat(self, box, workdir):
        box.run(f"tmux -L {box.sess} kill-server", check=False)
        box.run(f"tmux -L {box.sess} new-session -d -s {box.sess} -x 200 -y 50 \"bash -lc '. {box.root}/env.sh; cd {workdir}; exec codeaf chat'\"")
        time.sleep(7)

    def send(self, box, prompt, secs=300):
        t = box.drive("send", prompt, str(secs), timeout=secs + 60)
        if not t.get("idle_ms"):
            self.save(f"send-failed-{int(time.time())}.txt", f"{prompt}\n---\n{t.get('pane', '')}")
            raise RuntimeError("turn did not finish; the screen is in send-failed-*.txt")
        return t

    def watch_start(self, box, name, secs=5400, interval=100):
        out = f"{self.dir}/{name}.jsonl"
        r = box.sh(f"nohup s1probe watch {interval} {secs} > {out if box is A else '/tmp/' + name + '.jsonl'} 2>/dev/null < /dev/null & echo $!")
        return int(r.stdout.strip().splitlines()[-1])

    def watch_stop(self, box, pid, name):
        box.run(f"kill {pid}", check=False)
        if box is B:
            subprocess.run(["scp", "-q", "-o", f"ControlPath={CTL}", f"{B_HOST}:/tmp/{name}.jsonl", f"{self.dir}/{name}.jsonl"], check=False)

    def wait_durable(self, box, cell_id, cell_dir, timeout=3600):
        """Poll the directory until its head for the chat is the chat's last sealed turn."""
        t0 = time.time()
        while time.time() - t0 < timeout:
            last = json.loads(box.run(f"tail -n1 {cell_dir}/turns.jsonl").stdout)["id"]
            cells = json.loads(box.sh("s1probe cells").stdout)["cells"] or []
            head = next((c["head"] for c in cells if c["id"] == cell_id), None)
            if head == last:
                return round(time.time() - t0, 3)
            time.sleep(0.4)
        raise RuntimeError("never durable")

    # ---- the loop ----

    def resume(self):
        """Run the last two steps again on the state a pass left, after a failure there."""
        self.res = json.load(open(os.path.join(self.dir, "results-partial.json")))
        self.push(self.res)
        self.integrity(self.res)
        self.save("results.json", self.res)

    def card_test(self, n):
        """How often the resume card shows on a cold take through the home screen. n small chats are made on
        spark, each with a rebuildable install and a server left running, and dumb takes them one by one."""
        self.timed("clean", self.clean)
        self.timed("pair", self.pair)
        bsh(f"mkdir -p {B_R}/work/b && cd {B_R}/work/b && git init -q && echo b > README.md && git add . && git -c user.name=v -c user.email=v@x commit -qm init")
        self.start_chat(B, f"{B_R}/work/b")
        B.drive("home")
        out = []
        for i in range(1, n + 1):
            d = f"{A_R}/work/c{i}"
            sh(f"cp -a {CORPUS}/{self.repo} {d} && rm -rf {d}/.furrow {d}/.codeaf && cd {d} && printf '{{\"name\":\"vdemo\",\"version\":\"1.0.0\",\"dependencies\":{{\"left-pad\":\"1.3.0\"}}}}\n' > package.json && npm install --no-audit --no-fund >/dev/null 2>&1")
            sess = f"vdc{i}"
            box = Box("spark", A_R, sh, sess)
            sh(f"tmux -L {sess} new-session -d -s {sess} -x 200 -y 50 \"bash -lc '. {A_R}/env.sh; cd {d}; exec codeaf chat'\"")
            time.sleep(6)
            self.send(box, f"Make exactly 2 bash tool calls, one per call, no other commentary: (1) echo edited-card-{i} >> README.md ; (2) nohup python3 -m http.server {8800 + i} --bind 127.0.0.1 > /tmp/vdemo-card-{i}.log 2>&1 & . Then reply with the single word done.", 120)
            cdir = box.cell_dir(f"-work-c{i}")
            self.wait_durable(box, os.path.basename(os.path.dirname(cdir)), cdir)
            time.sleep(30)   # the chat is named a little after its first turn
            self.start_chat(B, f"{B_R}/work/b")   # a fresh window each time, as on a first take
            B.drive("home")
            take = B.drive("take", "-", timeout=900)
            bcell = os.path.basename(os.path.dirname(cdir))
            inv = json.loads(B.run(f"cat {B_R}/home/v3/projects/*/{bcell}/.cell/env/inventory.json").stdout or "{}")
            out.append({"i": i, "take_s": round((take["taken_ms"] - take["confirm_ms"]) / 1000, 3) if take.get("taken_ms") else None,
                        "listed_s": round((take["listed_ms"] - take["confirm_ms"]) / 1000, 3) if take.get("listed_ms") else None,
                        "auto_opened": take.get("auto_opened"),
                        "card": bool(take.get("resume_card_ms")), "withheld": inv.get("withheld"), "running": inv.get("running"), "error": take.get("error")})
            self.save(f"card{i}-screen.txt", take.get("resume_card") or take.get("first_screen", ""))
            log("card test", out[-1])
            sh(f"tmux -L {sess} kill-server", check=False)
        self.res["card_test"] = out
        self.save("card-test.json", out)
        self.stop_rig_processes(B)
        self.stop_rig_processes(A)

    def run(self):
        self.timed("clean", self.clean)
        self.timed("pair", self.pair)
        self.timed("fixture", self.fixture)
        self.start_chat(B, f"{B_R}/work/b")
        B.drive("home")
        self.start_chat(A, f"{A_R}/work/m")
        res = self.res
        self.first_turns(res)
        self.move(res)
        self.push(res)
        self.integrity(res)
        self.save("results.json", res)

    def first_turns(self, res):
        t1 = ("Make exactly 3 bash tool calls, one per call, no other commentary: "
              "(1) echo edited-by-validation >> README.md ; (2) echo 'print(1)' > scratch.py ; "
              f"(3) nohup python3 -m http.server {self.port} --bind 127.0.0.1 > /tmp/vdemo-srv-{self.n}.log 2>&1 & "
              ". Then reply with the single word done.")
        wpid = self.watch_start(A, "a-directory-watch", interval=200)
        r1 = self.send(A, t1)
        self.save("a-turn1-pane.txt", r1["pane"])
        res["a_cell_dir"] = cdir = A.cell_dir("-work-m")
        cell = os.path.basename(os.path.dirname(cdir))
        res["cell"] = cell
        res["first_upload_durable_s"] = self.wait_durable(A, cell, cdir)
        log("first upload durable", res["first_upload_durable_s"], "s")
        time.sleep(40)   # the chat's name is made after the first turn; let it settle before a row is looked for
        for i in range(1, 6):
            self.send(A, f"Make exactly 1 bash tool call: echo tick-{i} >> ticks.txt . Then reply with the single word done.", 120)
            self.wait_durable(A, cell, cdir)
        self.watch_stop(A, wpid, "a-directory-watch")
        rows = json.loads(sh(f"python3 {HELP}/cellread.py {cdir}").stdout)
        self.save("a-cellread.json", rows)
        res["a_latency"] = self.latency(rows, f"{self.dir}/a-directory-watch.jsonl", cell)
        self.log_latency("spark", res["a_latency"])
        self.save("a-pane-before-move.txt", A.tmux("capture-pane", "-p", "-t", A.sess))

    @staticmethod
    def log_latency(who, lat):
        log(who, "seal ms", [c["seal_ms"] for c in lat["calls"]], "durable ms", [c["durable_ms"] for c in lat["calls"]])

    def latency(self, rows, watch, cell, since_ms=0):
        """Per tool call: how long after the call ended the chat sealed (the instant save), and how long
        after the seal the relay's directory showed it (durable). The directory's clock is the relay's,
        within the offset the watch lines record between it and this machine's."""
        mine = [r for r in rows if r["sealed_ms"] >= since_ms]
        joined, lost = durable.join(mine, durable.head_changes(watch, cell))
        dur = {t["idx"]: at - t["sealed_ms"] for t, at in joined}
        skew = None
        for line in open(watch):
            try:
                rec = json.loads(line)
            except ValueError:
                continue
            skew = rec["local_ms"] - rec["now"]
            break
        return {"calls": [{"idx": r["idx"], "seal_ms": r["seal_ms"], "durable_ms": dur.get(r["idx"])} for r in mine],
                "never_durable": [t["idx"] for t in lost], "relay_clock_behind_ms": skew}

    def move(self, res):
        cell, cdir = res["cell"], res["a_cell_dir"]
        # B's home is on screen, the way a person left it.
        res["b_home_rows_before_take"] = B.drive("home")
        take = B.drive("take", "-", timeout=4000)
        self.save("b-take.json", take)
        res["cold_take"] = {k: take.get(k) for k in ("card_ms", "confirm_ms", "taken_ms", "open_ms", "resume_card_ms", "ready_ms", "row", "card", "error")}
        if take.get("taken_ms"):
            res["cold_take_s"] = round((take["taken_ms"] - take["confirm_ms"]) / 1000, 3)
            res["resume_card_shown"] = bool(take.get("resume_card_ms"))
            if take.get("listed_ms"):
                res["cold_take_listed_s"] = round((take["listed_ms"] - take["confirm_ms"]) / 1000, 3)
                res["cold_take_auto_opened"] = take.get("auto_opened")
            if take.get("resume_card_ms"):
                res["resume_card_after_open_ms"] = take["resume_card_ms"] - take["taken_ms"]
            log("cold take (confirm to chat open)", res["cold_take_s"], "s; resume card", res["resume_card_shown"])
        self.save("b-take-first-screen.txt", take.get("first_screen", ""))
        self.save("b-resume-card.txt", take.get("resume_card", "") or "(no card appeared)")
        self.save("b-screen-after-card.txt", take.get("screen_after", ""))
        assert "error" not in take, take.get("error")
        # the old window on spark goes the way a closed lid does: its process ends, nothing is typed in it
        self.stop_rig_processes(A)
        bcdir = B.run(f"ls -d {B_R}/home/v3/projects/*/{cell}/.cell").stdout.strip()
        res["b_cell_dir"] = bcdir
        self.save("b-inventory-after-take.json", B.run(f"cat {bcdir}/env/inventory.json").stdout)
        res["b_watch_started_ms"] = int(time.time() * 1000)
        bpid = self.watch_start(B, "b-directory-watch")
        try:
            for i in range(1, 4):
                self.send(B, f"Make exactly 1 bash tool call: echo b-tick-{i} >> b-ticks.txt . Then reply with the single word done.", 120)
                self.wait_durable(B, cell, bcdir)
        finally:
            self.watch_stop(B, bpid, "b-directory-watch")
        rows = json.loads(B.run(f"python3 {B_R}/bin/cellread.py {bcdir}").stdout)
        self.save("b-cellread.json", rows)
        res["b_latency"] = self.latency(rows, f"{self.dir}/b-directory-watch.jsonl", cell, since_ms=res["b_watch_started_ms"])
        self.log_latency("dumb", res["b_latency"])
        self.stop_rig_processes(B)
        # take back to spark, then change one file there, then warm take on dumb
        tb = json.loads(sh(f". {A_R}/env.sh; codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])
        self.save("a-take-back.json", tb)
        res["take_back_ms"] = tb.get("take_ms")
        res["take_back_error"] = tb.get("error")
        log("take back", tb.get("take_ms"), tb.get("error"))
        self.start_chat(A, f"{A_R}/work/m")
        op = A.drive("open-local")
        self.save("a-open-after-take-back.json", op)
        self.send(A, "Make exactly 1 bash tool call: echo a-after-takeback >> a-after.txt . Then reply with the single word done.", 120)
        self.wait_durable(A, cell, cdir)
        self.stop_rig_processes(A)
        tw = json.loads(B.sh(f"codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])
        self.save("b-warm-take.json", tw)
        res["warm_take_ms"] = tw.get("take_ms")
        res["warm_take_error"] = tw.get("error")
        log("warm take", tw.get("take_ms"), tw.get("error"))
        res["b_tree"] = os.path.join(os.path.dirname(bcdir), "work")

    # ---- push latency: a new chat on spark reaches dumb's home screen ----

    def push(self, res):
        n = 5
        offset = json.loads(sh(f"BHOST={B_HOST} python3 {HELP}/clockoff.py /tmp/vd-cm").stdout)
        res["clock_offset"] = offset
        self.start_chat(B, f"{B_R}/work/b")
        B.drive("home")
        B.run(f"nohup env VD_TMUX=vdb python3 {B_R}/bin/tuidrive.py watch-new 600 10 > /tmp/b-new-rows.jsonl 2>&1 < /dev/null & echo $!")
        time.sleep(2)
        samples = []
        for i in range(1, n + 1):
            d = f"{A_R}/work/p{i}"
            sh(f"mkdir -p {d} && cd {d} && git init -q && echo p{i} > README.md && git add . && git -c user.name=v -c user.email=v@x commit -qm init")
            sess = f"vdp{i}"
            sh(f"tmux -L {sess} new-session -d -s {sess} -x 200 -y 50 \"bash -lc '. {A_R}/env.sh; cd {d}; exec codeaf chat'\"")
            time.sleep(5)
            box = Box("spark", A_R, sh, sess)
            self.send(box, f"Make exactly 1 bash tool call: echo push-probe-{i} >> probe.txt . Then reply with the single word done.", 120)
            cdir = box.cell_dir(f"-work-p{i}")
            cid = os.path.basename(os.path.dirname(cdir))
            self.wait_durable(box, cid, cdir)
            rows = cellread.rows(cdir)
            probe = json.loads(sh(f". {A_R}/env.sh; s1probe cells").stdout)
            row = next(c for c in probe["cells"] if c["id"] == cid)
            samples.append({"i": i, "sealed_ms": rows[-1]["sealed_ms"], "cell": cid, "durable_at_spark_ms": row["durable_at"] + probe["local_ms"] - probe["now"]})
            time.sleep(6)
            sh(f"tmux -L {sess} kill-server", check=False)
        time.sleep(4)
        raw = B.run("cat /tmp/b-new-rows.jsonl").stdout
        self.save("b-new-rows.jsonl", raw)
        sightings = [json.loads(l) for l in raw.splitlines() if l.startswith("{")]
        off = offset["offset_ms"]
        # each sighting is matched to the sample sealed just before it
        out = []
        for s in samples:
            later = [x for x in sightings if x["ms"] - off >= s["sealed_ms"] - 50]
            if later:
                out.append({"i": s["i"], "seal_to_home_ms": later[0]["ms"] - off - s["sealed_ms"],
                            "durable_to_home_ms": later[0]["ms"] - off - s["durable_at_spark_ms"]})
                sightings = sightings[sightings.index(later[0]) + 1:]
        res["push_samples"] = out
        res["push_seal_to_home_ms"] = [o["seal_to_home_ms"] for o in out]
        res["push_durable_to_home_ms"] = [o["durable_to_home_ms"] for o in out]
        log("push seal-to-home ms", res["push_seal_to_home_ms"], "durable-to-home ms", res["push_durable_to_home_ms"])
        self.stop_rig_processes(B)
        self.stop_rig_processes(A)

    # ---- integrity ----

    def integrity(self, res):
        a, b = f"{A_R}/work/m", res["b_tree"]
        ig = {}
        local_b = f"{self.dir}/b-tree"
        sh(f"rm -rf {local_b}; mkdir -p {local_b}")
        subprocess.run(["rsync", "-a", "-e", f"ssh -o ControlPath={CTL}", "--rsync-path=/opt/homebrew/bin/rsync", "--exclude", ".furrow", f"{B_HOST}:{b}/", local_b + "/"], check=True)
        d = sh(f"diff -r --no-dereference -x .furrow -x .git -x node_modules {a} {local_b}", check=False)
        self.save("diff-r.txt", d.stdout + d.stderr)
        ig["diff_r_exit"], ig["diff_r_lines"] = d.returncode, len(d.stdout.splitlines())
        ha = sh(f"python3 {HELP}/treehash.py {a}").stdout
        hb = bsh(f"python3 {B_R}/bin/treehash.py {b}").stdout
        self.save("treehash-a.txt", ha)
        self.save("treehash-b.txt", hb)
        ig["treehash_equal_ignoring_node_modules"] = self.strip_nm(ha) == self.strip_nm(hb)
        modes = lambda t: sorted(l.split(" ", 2)[1:] for l in t.splitlines() if re.match(r"^[0-9a-f]{64} ", l))
        ig["modes_equal"] = modes(self.strip_nm(ha)) == modes(self.strip_nm(hb))
        secrets = {}
        for f in (".env", "svc/api/.env", "web/client/app/.env.local", "run.sh"):
            ma = sh(f"stat -c %a {a}/{f}").stdout.strip()
            mb = bsh(f"stat -f %Lp {b}/{f}").stdout.strip()
            same = sh(f"cmp {a}/{f} {local_b}/{f}", check=False).returncode == 0
            secrets[f] = {"spark": ma, "dumb": mb, "bytes_equal": same}
        ig["secrets"] = secrets
        ga = sh(f"cd {a}; git status --porcelain; echo ---; git log --oneline -3; echo ---; git diff | sha256sum").stdout
        gb = bsh(f"cd {b}; git status --porcelain; echo ---; git log --oneline -3; echo ---; git diff | sha256sum").stdout
        self.save("git-a.txt", ga)
        self.save("git-b.txt", gb)
        only_a = set(ga.split("---")[0].split("\n")) - set(gb.split("---")[0].split("\n"))
        ig["git_status_only_on_spark_before_rebuild"] = sorted(only_a - {""})
        ig["git_log_and_diff_equal"] = ga.split("---", 1)[1] == gb.split("---", 1)[1]
        ig["codeaf_in_git_status_b"] = ".codeaf" in gb.split("---")[0]
        ig["node_modules_on_b_before_rebuild"] = bsh(f"test -e {b}/node_modules && echo yes || echo no").stdout.strip()
        t0 = time.time()
        r = bsh(f"cd {b} && npm ci --no-audit --no-fund 2>&1 | tail -3")
        ig["rebuild_s"] = round(time.time() - t0, 2)
        self.save("b-npm-ci.txt", r.stdout)
        nm = "cd {}/node_modules && find . -type f | LC_ALL=C sort | xargs shasum -a 256 | shasum -a 256 | cut -d' ' -f1"
        ig["node_modules_rebuilt_equal"] = bsh(nm.format(b)).stdout.strip() == sh(nm.format(a).replace("shasum -a 256", "sha256sum")).stdout.strip()
        gb2 = bsh(f"cd {b}; git status --porcelain; echo ---; git log --oneline -3; echo ---; git diff | sha256sum").stdout
        self.save("git-b-after-rebuild.txt", gb2)
        ig["git_equal_after_rebuild"] = ga == gb2
        ig["node_modules_file_diff"] = sorted(set(sh(f"cd {a}/node_modules && find . -type f | LC_ALL=C sort").stdout.split()) ^ set(bsh(f"cd {b}/node_modules && find . -type f | LC_ALL=C sort").stdout.split()))
        res["integrity"] = ig
        log("integrity", json.dumps(ig))

    @staticmethod
    def strip_nm(t):
        return "\n".join(l for l in t.splitlines() if " node_modules/" not in l and not l.endswith(" node_modules"))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("n", type=int)
    ap.add_argument("--repo", default="r02-mj-base")
    ap.add_argument("--resume", action="store_true", help="redo push and integrity on the state the pass left")
    ap.add_argument("--card-test", type=int, default=0, help="n cold takes of small chats, counting resume cards")
    ap.add_argument("--out", default="/home/santosh/codeaf-prototype/evidence/hosted-validate")
    args = ap.parse_args()
    sh(f"ssh -MNf -o ControlPath={CTL} -o ControlPersist=3h -o BatchMode=yes {B_HOST}", check=False)
    # the Mac is shared: timings only while its lock is ours
    while bsh("mkdir ~/caf-bench.lock 2>/dev/null && echo got", check=False).stdout.strip() != "got":
        log("dumb is locked by another run; waiting 30 s")
        time.sleep(30)
    p = Pass(args.n, args.repo, args.out)
    try:
        if args.card_test:
            p.card_test(args.card_test)
        else:
            p.resume() if args.resume else p.run()
    finally:
        bsh("rmdir ~/caf-bench.lock", check=False)
        p.save("results-partial.json", p.res)


if __name__ == "__main__":
    main()
