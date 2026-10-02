#!/usr/bin/env python3
"""Does a folder move byte for byte? One fixture tree of awkward things, this machine to a second one and back.

  scripts/fidelity.py run [--out DIR]     pair, build the fixture, move it there, mutate and move again, move it back

Home A is on this box, home B is on the second machine reached with ssh (a Mac on APFS is the case it was
measured on). Settings come from the environment (measure/rigenv.py, docs/testing-anywhere.md):
CODEAF_RELAY, CODEAF_SECOND_HOST, CODEAF_SECOND_ROOT, CODEAF_FIRST_ROOT and CODEAF_RIG_BIN. The fixture (measure/continuity/fidelity-fixture.sh)
holds deletes-to-come, renames-to-come, empty and deep paths, unicode and case-colliding names, two big binaries, every
kind of symlink, a hard-linked pair, odd modes, a full .git (stash, staged change, submodule) and ignored files. Each leg
takes a manifest of the whole tree on both machines (fidelity-manifest.py) and compares them (fidelity-compare.py).
Legs: 1 first move A->B; 2 deletes and renames on A, then a warm move A->B (no ghosts); 3 a change made on B by its
agent, then the move back B->A. No model reads the fixture: each turn runs one exact command to make a seal.

The moves are the Continuer.Take the home screen runs, through the private `vdemo-take` build (scripts/measure/hosted/
vdemotake.go.txt): the home screen has no door for a chat the machine already lists.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
KIT = os.path.join(HERE, "measure", "continuity")
HELP = os.path.join(HERE, "measure", "hosted")
sys.path.insert(0, HELP)
sys.path.insert(0, KIT)
import importlib.util  # noqa: E402
spec = importlib.util.spec_from_file_location("hv", os.path.join(HERE, "hosted-validate.py"))
hv = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hv)
sys.path.insert(0, os.path.join(HERE, "measure"))
import rigenv  # noqa: E402
import tuidrive as td  # noqa: E402

# The machines and folders are read by configure() once the arguments are parsed, so that `--help`
# works on a box where none of them is set.
A_ROOT = B_ROOT = B_HOST = BIN = RELAY = WS_A = A = B = None
CTL = "/tmp/vf-fid-ssh"
# FID_REUSE keeps both homes and their pairing (the relay limits pairings per network); each run then gets a new folder.
REUSE = bool(os.environ.get("FID_REUSE"))
# The four awkward cases (case and NFC/NFD collisions, a read-only directory, a 0000 file) are part of the main fixture
# now that a move carries them; FID_PLAIN=1 builds the older fixture without them.
ODD = "" if os.environ.get("FID_PLAIN") else "FIXTURE_NAME_COLLISIONS=1 FIXTURE_RO_DIR=1 FIXTURE_ZERO_MODE=1"
sh = hv.sh


def bsh(cmd, check=True, timeout=None, stdin=None):
    r = subprocess.run(["ssh", "-o", f"ControlPath={CTL}", "-o", "BatchMode=yes", B_HOST, "bash -l -s"], capture_output=True, text=True,
                       input="export PATH=/opt/homebrew/bin:$PATH; " + cmd + "\n", timeout=timeout)
    if check and r.returncode:
        raise RuntimeError(f"B: {cmd[:160]} -> {r.returncode}: {r.stderr[-400:]}")
    return r


log = hv.log


def configure():
    """Read every machine and folder setting, stopping with the name of the first one that is missing."""
    global A_ROOT, B_ROOT, B_HOST, BIN, RELAY, WS_A, A, B
    A_ROOT, B_ROOT = rigenv.get("CODEAF_FIRST_ROOT"), rigenv.get("CODEAF_SECOND_ROOT")
    B_HOST, BIN, RELAY = rigenv.get("CODEAF_SECOND_HOST"), rigenv.get("CODEAF_RIG_BIN"), rigenv.get("CODEAF_RELAY")
    WS_A = f"{A_ROOT}/work/fx{int(time.time()) if REUSE else ''}"
    A, B = hv.Box("A", A_ROOT, sh, "vfa"), hv.Box("B", B_ROOT, bsh, "vfb")


def platform(run):
    """The comparator's name for the platform a command runner works on: `mac` folds case, `linux` does not."""
    return "mac" if run("uname -s").stdout.strip() == "Darwin" else "linux"


def stop_a():
    A.tmux("kill-server")
    for line in sh("ps -u $(id -u) -o pid=,args=").stdout.splitlines():
        pid, _, args = line.strip().partition(" ")
        if args.startswith(A_ROOT + "/"):
            sh(f"kill {pid}", check=False)
    time.sleep(1.5)


def stop_b():
    B.tmux("kill-server")
    out = bsh(f"ps -axo pid=,command= | grep '{B_ROOT}/' | grep -v grep | awk '{{print $1}}'", check=False).stdout.split()
    for pid in out:
        bsh(f"kill {pid}", check=False)
    time.sleep(1.5)


class Fid:
    def __init__(self, out):
        self.dir = out
        os.makedirs(out, exist_ok=True)
        self.res = {"started": time.strftime("%FT%TZ", time.gmtime()), "binary": BIN}

    def save(self, name, text):
        with open(os.path.join(self.dir, name), "w") as f:
            f.write(text if isinstance(text, str) else json.dumps(text, indent=1))

    # ---- rig ----

    def reset(self, fixture=True):
        sh(f"mkdir -p {A_ROOT}/bin" + ("" if REUSE else f" && chmod -R u+rwx {A_ROOT}/work 2>/dev/null; bash {KIT}/rig.sh {A_ROOT} {RELAY}"))
        sh(f"cp {BIN}/codeaf {BIN}/codeaf-vd {HELP}/tuidrive.py {HELP}/treehash.py {A_ROOT}/bin/ && cp {BIN}/s1probe {A_ROOT}/bin/")
        subprocess.run(["scp", "-q", "-o", f"ControlPath={CTL}", f"{KIT}/rig.sh", f"{KIT}/fidelity-manifest.py", f"{B_HOST}:/tmp/"], check=True)
        subprocess.run(["scp", "-q", "-o", f"ControlPath={CTL}", f"{HELP}/tuidrive.py", f"{HELP}/treehash.py", f"{B_HOST}:/tmp/"], check=True)
        bsh((f"bash /tmp/rig.sh {B_ROOT} {RELAY}; " if not REUSE else "") + f"mkdir -p {B_ROOT}/bin {B_ROOT}/work/b; cp /tmp/fidelity-manifest.py /tmp/tuidrive.py /tmp/treehash.py {B_ROOT}/bin/")
        bsh(f"cd {B_ROOT}/work/b && (test -d .git || (git init -q && echo b > README.md && git add . && git -c user.name=v -c user.email=v@x commit -qm init))")
        if fixture:
            sh(f"{ODD} bash {KIT}/fidelity-fixture.sh {WS_A} > {self.dir}/fixture.log 2>&1")

    def pair(self):
        sh("tmux -L vfp kill-server", check=False)
        sh(f"tmux -L vfp new-session -d -s vfp -x 150 -y 30 \"bash -lc '. {A_ROOT}/env.sh; codeaf pair --code; echo EXIT=\\$?; sleep 60'\"")
        code, t0 = None, time.time()
        while time.time() - t0 < 60 and not code:
            m = re.search(r"codeaf pair (\d\d-\d\d\d-\d\d\d)", sh("tmux -L vfp capture-pane -p -t vfp", check=False).stdout)
            code = m and m.group(1)
            time.sleep(0.05)
        assert code, "no pairing code shown"
        join = subprocess.Popen(["ssh", "-o", f"ControlPath={CTL}", B_HOST, f"bash -l -c '. {B_ROOT}/env.sh; codeaf pair {code}'"],
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        answered = False
        while time.time() - t0 < 60:
            out = sh("tmux -L vfp capture-pane -p -t vfp", check=False).stdout
            if "wants your chats" in out and not answered:
                sh("tmux -L vfp send-keys -t vfp y Enter")
                answered = True
            if "EXIT=" in out:
                break
            time.sleep(0.05)
        jout = join.communicate(timeout=60)[0]
        sh("tmux -L vfp kill-server", check=False)
        assert "paired." in jout, jout
        log("paired")

    # ---- a chat on one side ----

    def chat(self, box, workdir):
        box.run(f"tmux -L {box.sess} kill-server", check=False)
        box.run(f"tmux -L {box.sess} new-session -d -s {box.sess} -x 200 -y 50 \"bash -lc '. {box.root}/env.sh; cd {workdir}; exec codeaf chat'\"")
        time.sleep(10)

    def say(self, box, prompt, secs=900):
        """One prompt through the real screen on box's machine, until the footer is idle for 5 s. The driver runs on the box itself."""
        t = box.drive("send", prompt, str(secs), timeout=secs + 120)
        if not t.get("idle_ms"):
            self.save(f"stuck-{box.name}-{int(time.time())}.txt", t.get("pane", ""))
            raise RuntimeError("turn did not finish: " + prompt[:60])
        return t

    def cell_of(self, box):
        d = box.run(f"ls -td {box.root}/home/v3/projects/*/*/.cell 2>/dev/null | head -1").stdout.strip()
        return d, os.path.basename(os.path.dirname(d))

    def head_wait(self, cell, last_turn, secs=3600):
        """The relay's head for the chat is A's view (the same identity): poll it from A's home until it is last_turn."""
        t0 = time.time()
        while time.time() - t0 < secs:
            cells = json.loads(A.sh("s1probe cells").stdout)["cells"] or []
            head = next((c["head"] for c in cells if c["id"] == cell), None)
            if head == last_turn:
                return round(time.time() - t0, 1)
            time.sleep(1)
        raise RuntimeError("head never reached the last sealed turn")

    def last_turn(self, box):
        """The newest sealed turn of the box's chat; the first seal of a chat lands a moment after its turn ends."""
        for _ in range(120):
            cdir, _ = self.cell_of(box)
            out = box.run(f"tail -n1 {cdir}/turns.jsonl 2>/dev/null", check=False).stdout.strip() if cdir else ""
            if out:
                return json.loads(out)["id"]
            time.sleep(1)
        raise RuntimeError("the chat never sealed a turn")

    # ---- manifests and comparison ----

    def manifest(self, box, tree, name):
        if box is A:
            sh(f"python3 {KIT}/fidelity-manifest.py {tree} --out {self.dir}/{name}.json")
        else:
            bsh(f"python3 {B_ROOT}/bin/fidelity-manifest.py {tree} --out /tmp/{name}.json", timeout=3600)
            subprocess.run(["scp", "-q", "-o", f"ControlPath={CTL}", f"{B_HOST}:/tmp/{name}.json", f"{self.dir}/{name}.json"], check=True)

    def compare(self, a, b, leg, *flags):
        r = sh(f"python3 {KIT}/fidelity-compare.py {self.dir}/{a}.json {self.dir}/{b}.json {' '.join(flags)}", check=False)
        self.save(f"{leg}-table.md", r.stdout)
        self.res[leg] = {"exit": r.returncode, "rows": [l for l in r.stdout.splitlines() if l.startswith("|")]}
        print(f"\n## {leg}\n{r.stdout}")

    def set_apart(self, box, cell):
        """The paths the chat's record on box names as not brought along for a reason (unreadable, or a name this file
        system folds): what the product itself says it set apart, which the comparison must not count as lost."""
        raw = box.run(f"cat {box.root}/home/v3/projects/*/{cell}/.cell/env/inventory.json", check=False).stdout
        try:
            held = json.loads(raw).get("withheld") or []
        except ValueError:
            held = []
        return sorted({w["path"] for w in held if w.get("reason")})

    def b_tree(self, cell):
        return bsh(f"ls -d {B_ROOT}/home/v3/projects/*/{cell}/work").stdout.strip()

    def take(self, box, cell):
        t0 = time.time()
        out = box.sh(f"codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()
        res = json.loads(out[-1]) if out else {}
        res["wall_s"] = round(time.time() - t0, 1)
        return res

    # ---- the legs ----

    def run(self):
        subprocess.run(["ssh", "-MNf", "-o", f"ControlPath={CTL}", "-o", "ControlPersist=3h", "-o", "BatchMode=yes", B_HOST], check=False)
        stop_a(); stop_b()
        self.reset()
        if not REUSE:
            self.pair()
        self.chat(A, WS_A)
        self.say(A, "Run exactly this one bash command and nothing else: `git status --short | head -3` (ref-fd1)")
        cdir, cell = self.cell_of(A)
        self.res["cell"] = cell
        self.res["a_durable_s"] = self.head_wait(cell, self.last_turn(A))
        log("first upload durable", self.res["a_durable_s"], "s")
        stop_a()
        self.manifest(A, WS_A, "a0")
        # leg 1: first move A -> B
        t1 = self.take(B, cell)
        self.save("leg1-take.json", t1)
        self.res["leg1_take_s"], self.res["leg1_error"] = t1.get("take_ms"), t1.get("error")
        assert not t1.get("error"), t1
        bt = self.b_tree(cell)
        self.manifest(B, bt, "b1")
        apart = self.set_apart(B, cell)
        self.res["set_apart_leg1"] = apart
        self.compare("a0", "b1", "leg1-first-to-second", "--platform-b", platform(bsh), "--reported", ",".join(apart))
        # leg 2: deletes and renames on A after the first move, then a warm move
        back = self.take(A, cell)           # A takes its chat back from B (no new seals there)
        self.save("leg2-takeback.json", back)
        gone = self.mutate_a()
        self.chat(A, WS_A)
        A.drive("open-local")
        self.say(A, "Run exactly this one bash command and nothing else: `git status --short | head -3` (ref-fd2)")
        self.res["a2_durable_s"] = self.head_wait(cell, self.last_turn(A))
        stop_a()
        self.manifest(A, WS_A, "a2")
        t2 = self.take(B, cell)
        self.save("leg2-take.json", t2)
        self.res["leg2_take_s"], self.res["leg2_error"] = t2.get("take_ms"), t2.get("error")
        self.manifest(B, bt, "b2")
        apart = sorted(set(apart) | set(self.set_apart(B, cell)))
        self.res["set_apart_leg2"] = apart
        self.compare("a2", "b2", "leg2-after-deletes-and-renames", "--platform-b", "mac", "--expect-gone", ",".join(gone), "--reported", ",".join(apart))
        # leg 3: a change made on B by its own agent, then back to A
        self.chat(B, f"{B_ROOT}/work/b")
        B.drive("open-local")
        self.say(B, "Run exactly this one bash command and nothing else: `cd " + bt + " && mv docs/b.md docs/b-from-mac.md && rm -f docs/feature.md && mkdir -p mac-new && printf mac > mac-new/f.txt && printf x > \"$(printf 'mac\\314\\201.txt')\" && printf y >> src/a.txt` (ref-fd3)")
        self.head_wait(cell, self.last_turn(B))
        stop_b()
        self.manifest(B, bt, "b3")
        t3 = self.take(A, cell)
        self.save("leg3-take.json", t3)
        self.res["leg3_take_s"], self.res["leg3_error"] = t3.get("take_ms"), t3.get("error")
        self.manifest(A, WS_A, "a3")
        self.compare("b3", "a3", "leg3-second-to-first", "--platform-b", platform(sh), "--expect-gone", "docs/b.md,docs/feature.md", "--reported", ",".join(apart), "--keeps", f"{self.dir}/a2.json")
        self.save("results.json", self.res)

    PROBES = {
        "read-only-directory": "mkdir ro && echo x > ro/inside.txt && chmod 0555 ro",
        "case-colliding-names": "echo lower > Readme.md && echo UPPER > README.md",
        "nfc-nfd-same-name": "printf a > \"$(printf 'caf\\303\\251').txt\" && printf b > \"$(printf 'cafe\\314\\201').txt\"",
        "unreadable-file-mode-0000": "echo x > zero.txt && chmod 0000 zero.txt",
    }

    # What each probe must leave on B (the Mac): a shell test run there that exits 0 when the outcome is right, plus the paths
    # the record must name as not brought along. Every probe also needs a take with no error and a base file that arrived.
    EXPECT = {
        "read-only-directory": {
            "test": 'test "$(cat ro/inside.txt)" = x && test "$(stat -f %Lp ro)" = 555', "named": []},
        "case-colliding-names": {
            "test": 'test "$(cat README.md)" = UPPER && ! ls | grep -qx Readme.md', "named": ["Readme.md"]},
        "nfc-nfd-same-name": {
            "test": 'test "$(ls | grep -c "^caf")" = 1', "named": ["caf\u00e9.txt"]},
        "unreadable-file-mode-0000": {
            "test": 'test ! -e zero.txt && test "$(cat base.txt)" = base', "named": ["zero.txt"]},
    }

    def probes(self):
        """One tiny folder per awkward thing, each moved alone, so a failure names its cause and one cannot hide another.
        Each must seal, move, arrive as EXPECT says, and be named in B's record; the screen of A must say what it left out."""
        subprocess.run(["ssh", "-MNf", "-o", f"ControlPath={CTL}", "-o", "ControlPersist=3h", "-o", "BatchMode=yes", B_HOST], check=False)
        stop_a(); stop_b()
        self.reset(fixture=False)
        if not REUSE:
            self.pair()
        out = {}
        for name, setup in self.PROBES.items():
            stop_a(); stop_b()
            ws = f"{A_ROOT}/work/probe-{name}-{int(time.time())}"
            sh(f"rm -rf {ws}; mkdir -p {ws} && cd {ws} && git init -q && echo base > base.txt && git add . && git -c user.name=v -c user.email=v@x commit -qm base && {setup}")
            self.chat(A, ws)
            turn = self.say(A, "Run exactly this one bash command and nothing else: `git status --short | head -3` (ref-fp1)")
            self.save(f"probe-{name}-screen.txt", turn.get("pane", ""))
            cdir, cell = self.cell_of(A)
            try:
                head = self.head_wait(cell, self.last_turn(A), secs=120)
            except RuntimeError as e:
                out[name] = {"sealed": False, "note": str(e)}
                sh(f"chmod -R u+rwx {ws}", check=False)
                continue
            take = self.take(B, cell)
            out[name] = {"sealed": True, "durable_s": head, "take_error": take.get("error"), "take_ms": take.get("take_ms"),
                         "screen_names_the_file": "cannot be read here" in turn.get("pane", "")}
            if not take.get("error"):
                out[name].update(self.arrived(cell, name))
            sh(f"chmod -R u+rwx {ws}", check=False)
        self.save("probes.json", out)
        print(json.dumps(out, indent=1))

    def arrived(self, cell, name):
        """Run the probe's test on B's tree and read B's record for the paths the probe says must be named."""
        want = self.EXPECT[name]
        ok = self.bsh_ok(f"cd {self.b_tree(cell)} && {want['test']}")
        named = self.set_apart(B, cell)
        return {"arrived_as_expected": ok, "named_in_record": named, "all_named": all(n in named for n in want["named"])}

    @staticmethod
    def bsh_ok(cmd):
        return bsh(cmd, check=False).returncode == 0

    def mutate_a(self):
        """Deletes and renames of every kind on A's tree; returns the old paths that must not exist on B afterwards."""
        steps = ["rm -f docs/c.md", "mv docs/b.md docs/b-renamed.md", "mv big/300mb.bin big/300mb-renamed.bin", "rm -rf empty-chain",
                 "mv names names-renamed", "rm -f link-rel", "mv deep deep-renamed", "rm -f hl-b"]
        sh(f"cd {WS_A} && " + " && ".join(steps))
        return ["docs/c.md", "docs/b.md", "big/300mb.bin", "empty-chain", "names", "link-rel", "deep", "hl-b"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["run", "probes"])
    ap.add_argument("--out", default=os.path.join(os.path.dirname(HERE), ".lane", "fidelity"))
    args = ap.parse_args()
    configure()
    f = Fid(args.out)
    try:
        f.run() if args.cmd == "run" else f.probes()
    finally:
        f.save("results-partial.json", f.res)


if __name__ == "__main__":
    main()
