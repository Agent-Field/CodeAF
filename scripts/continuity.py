#!/usr/bin/env python3
"""Does a move carry ALL of a chat's work, so the agent on the other machine continues and does not redo it?

  scripts/continuity.py cold [--out DIR]     a fresh B takes a finished session
  scripts/continuity.py warm [--out DIR]     B already holds an older copy; A continues; B takes again

Two homes on this box are two devices of one identity (the relay under test, CODEAF_RELAY). Home A does a
realistic session in a real repository with a live model (reads, two edits, tests, an install, a dev
server, a task, a memory, a setting, a closing summary). The chat then moves to home B through the home
screen (cold) or the take the home screen runs (warm). Everything the chat owns is snapshotted on both
homes and compared (measure/continuity/check.py), then B's agent is asked to continue and two questions
only the prior work can answer, and the calls it repeats from A are counted.

It REUSES the vdemo harness: Box and the tmux/tuidrive plumbing of scripts/hosted-validate.py, and
scripts/measure/hosted/{tuidrive,treehash}.py. Live model: deepseek/deepseek-v4.1-flash for every role (rig.sh
pins them), the key comes from the login environment and is never read or printed here.
"""
import argparse
import importlib.util
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
spec = importlib.util.spec_from_file_location("hv", os.path.join(HERE, "hosted-validate.py"))
hv = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hv)
import check  # noqa: E402
import tuidrive as td  # noqa: E402

sys.path.insert(0, os.path.join(HERE, "measure"))
import rigenv  # noqa: E402

# The folders are read by configure() once the arguments are parsed, so that `--help` works on a box
# where none of them is set.
RIG = BIN = WS_A = A = B = None
# CONT_SUFFIX names the tmux servers, so a second rig (another binary) can run beside the first.
SUF = os.environ.get("CONT_SUFFIX", "")
sh, log = hv.sh, hv.log


def configure():
    """Read the rig folder and the built programs, stopping with the name of the first one that is missing."""
    global RIG, BIN, WS_A, A, B
    RIG, BIN = rigenv.get("CODEAF_FIRST_ROOT"), rigenv.get("CODEAF_RIG_BIN")
    rigenv.get("CODEAF_RELAY")  # the rig scripts read it themselves; a missing one is better named now than half way in
    WS_A = f"{RIG}/a/work/inv"
    A, B = hv.Box("A", f"{RIG}/a", hv.sh, "vca" + SUF), hv.Box("B", f"{RIG}/b", hv.sh, "vcb" + SUF)

PORT = os.environ.get("CONT_PORT", "8793")   # one dev-server port per rig, so two rigs can run side by side
# Each model turn names itself with a dashed word (ref-vcN): the rig tells one turn from the last by it.
FIRST = [
    "Read README.md, src/money.js and src/tax.js, then tell me in two sentences what this repo does and what the release codename is. (ref-vc1)",
    "Run npm test and tell me exactly which tests fail and how many pass. (ref-vc2)",
    "Fix the rounding bug in src/money.js so toCents rounds half up, then run npm test again. (ref-vc3)",
    "Now add a 'reduced' tax rate of 0.07 to src/tax.js (edit the file), rerun npm test and report the final numbers. (ref-vc4)",
    "Install the vendored dependency: run exactly `npm install ./vendor/pad-lite-1.0.0.tgz --offline --no-audit --no-fund --cache ./.npm-cache` once, then show me the dependencies line of package.json. (ref-vc5)",
]
SECOND = [
    f"Start the dev server in the background and leave it running: one bash call, `PORT={PORT} nohup node server.js > /tmp/vcont-srv.log 2>&1 &`, then check with `ss -ltn` that port {PORT} listens. (ref-vc6)",
    ("slash", "/task solo Write docs/CSV.md: a half-page design for exporting invoices as CSV (columns, quoting rules, one example row). Do not touch src/.", r"branch kept|landed"),
    "Remember this for next time, with your remember tool: the team prefers tabs in docs, and the on-call maintainer is Ines Okafor. (ref-vc8)",
    ("slash", "/effort high", r"high"),
    "Run git status --short, then give me a five-line status: files changed and why, test result, server port, open task. (ref-vc10)",
]
QUESTIONS = [
    "Continue where you left off. (ref-vq0)",
    "Which two files did you change in src/, and why? (ref-vq1)",
    "What did the tests say the last time you ran them, and which port is the dev server on? (ref-vq2)",
]
EXPECT = {"ref-vq1": ["money.js", "tax.js"], "ref-vq2": [PORT]}


def rig_pids(root):
    out = sh("ps -u $(id -u) -o pid=,args=").stdout.splitlines()
    pids = []
    for line in out:
        pid, _, args = line.strip().partition(" ")
        cwd = sh(f"readlink /proc/{pid}/cwd", check=False).stdout.strip()
        if args.startswith(root + "/") or cwd.startswith(root + "/work"):
            pids.append(int(pid))
    return pids


def stop(box):
    """End the chat, its engine daemon and furrow, and any server left running: only processes of this rig's root."""
    box.tmux("kill-server")
    for pid in rig_pids(box.root):
        sh(f"kill {pid}", check=False)
    time.sleep(1.5)


def chat_text(pane):
    """The conversation part of the screen: everything above the footer line (which ticks clocks and rotates tips), left of the side panel."""
    lines = pane.splitlines()
    foot = next((i for i in range(len(lines) - 1, -1, -1) if lines[i].startswith("─ ")), len(lines))
    return "\n".join(l[:150] for l in lines[:foot])   # the side panel at the right ticks the age of running jobs


class Run:
    def __init__(self, kind, out):
        self.kind, self.dir = kind, os.path.join(out, kind)
        os.makedirs(self.dir, exist_ok=True)
        self.res = {"kind": kind, "model": hv.MODEL, "started": time.strftime("%FT%TZ", time.gmtime())}

    def save(self, name, text):
        with open(os.path.join(self.dir, name), "w") as f:
            f.write(text if isinstance(text, str) else json.dumps(text, indent=1))

    def reset(self):
        """Fresh homes. The relay limits pairings per network, so a pair made once is kept (`paired/` beside each home)
        and put back for the next run: the identity and device keys are the same, and every chat is new."""
        for box in (A, B):
            stop(box)
            sh(f"bash {KIT}/rig.sh {box.root}")
            if os.path.isdir(f"{box.root}/paired"):
                sh(f"rm -rf {box.root}/home && cp -a {box.root}/paired {box.root}/home")
            sh(f"cp {BIN}/codeaf {BIN}/codeaf-vd {HELP}/tuidrive.py {HELP}/treehash.py {box.root}/bin/ && cp {BIN}/s1probe {box.root}/bin/")
        sh(f"bash {KIT}/fixture.sh {WS_A}")
        sh(f"mkdir -p {B.root}/work/b && cd {B.root}/work/b && git init -q && echo b > README.md && git add . && git -c user.name=v -c user.email=v@x commit -qm init")

    def ensure_pair(self):
        if os.path.isdir(f"{A.root}/paired") and os.path.isdir(f"{B.root}/paired"):
            return log("pairing kept from an earlier run")
        raise RuntimeError("no kept pairing: run `continuity.py pair` first (the relay limits pairings per network)")

    def pair_fresh(self):
        """Pairing against the staging relay is racy (its mailbox lives 4 s), so a failed handshake is tried again, twice."""
        for attempt in range(1):
            try:
                return self.pair_once()
            except (AssertionError, RuntimeError, subprocess.TimeoutExpired) as e:
                log("pairing attempt", attempt + 1, "failed:", str(e)[:120])
        raise RuntimeError("pairing failed (the relay limits pairings per network; wait and run again)")

    def pair_once(self):
        """The six-digit-style code way: A shows a code, B types it, A answers y. The staging mailbox lives 4 s, so it is scripted."""
        sh(f"tmux -L vcp{SUF} kill-server", check=False)
        sh(f"tmux -L vcp{SUF} new-session -d -s vcp -x 150 -y 30 \"bash -lc '. {A.root}/env.sh; codeaf pair --code; echo EXIT=\\$?; sleep 60'\"")
        code, t0 = None, time.time()
        while time.time() - t0 < 90 and not code:
            m = re.search(r"codeaf pair (\d\d-\d\d\d-\d\d\d)", sh(f"tmux -L vcp{SUF} capture-pane -p -t vcp").stdout)
            code = m and m.group(1)
            time.sleep(0.05)
        assert code, "no pairing code shown"
        join = subprocess.Popen(["bash", "-lc", f". {B.root}/env.sh; codeaf pair {code}"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        answered = False
        while time.time() - t0 < 40:
            out = sh(f"tmux -L vcp{SUF} capture-pane -p -t vcp").stdout
            if "wants your chats" in out and not answered:
                sh(f"tmux -L vcp{SUF} send-keys -t vcp y Enter")
                answered = True
            if "EXIT=" in out:
                break
            time.sleep(0.05)
        jout = join.communicate(timeout=30)[0]
        sh(f"tmux -L vcp{SUF} kill-server", check=False)
        assert "paired." in jout, jout
        log("paired")

    # ---- driving a chat ----

    def chat(self, box, workdir):
        box.run(f"tmux -L {box.sess} kill-server", check=False)
        box.run(f"tmux -L {box.sess} new-session -d -s {box.sess} -x 200 -y 50 \"bash -lc '. {box.root}/env.sh; cd {workdir}; exec codeaf chat'\"")
        time.sleep(8)

    def say(self, box, prompt, secs=420):
        """Type a prompt and wait until the footer has said idle for 5 s running. The vdemo send() waits for a
        `worked` line, which a turn with several tool groups does not always draw, so the footer is the judge."""
        td.SESS = box.sess
        td.key("-l", prompt)
        time.sleep(0.4)
        td.key("Enter")
        t0, calm, busy, last_pane = time.time(), None, False, None
        while time.time() - t0 < secs:
            time.sleep(1)
            pane = td.cap()
            if "needs your ok to run" in pane:   # a command the guard calls critical: refuse it, as a person away would
                self.res.setdefault("approvals_refused", []).append(pane.split("needs your ok to run")[-1][:160])
                td.key("3")
                time.sleep(2)
                continue
            # A background job the turn left running (a dev server) keeps the footer on `working` for good, so a screen
            # that has not changed for the whole calm interval counts as settled as well as an `idle` footer.
            body = chat_text(pane)
            idle = td.status_idle(pane) or body == last_pane
            last_pane = body
            busy = busy or not td.status_idle(pane)
            calm = (calm or time.time()) if (busy or time.time() - t0 > 20) and idle else None
            if calm and time.time() - calm >= 5:
                return {"pane": pane}
        self.save(f"stuck-{int(time.time())}.txt", f"{prompt}\n---\n{td.cap()}")
        raise RuntimeError("turn did not settle: " + prompt[:60])

    def quiet(self, box, secs=600):
        """Wait until the chat has been still for 8 s, so what it did on its own is over before a question is typed."""
        td.SESS = box.sess
        last, since, t0 = None, time.time(), time.time()
        while time.time() - t0 < secs:
            time.sleep(1)
            body = chat_text(td.cap())
            if body != last:
                last, since = body, time.time()
            elif time.time() - since >= 8:
                return
        self.save(f"never-quiet-{int(time.time())}.txt", td.cap())

    def turn(self, box, step):
        if isinstance(step, str):
            return self.say(box, step)
        _, text, until = step
        box.tmux("send-keys", "-t", box.sess, "-l", text)
        time.sleep(0.4)
        box.tmux("send-keys", "-t", box.sess, "Enter")
        t0 = time.time()
        while time.time() - t0 < 300:
            time.sleep(2)
            pane = box.tmux("capture-pane", "-p", "-t", box.sess)
            if re.search(until, pane.split(text[:30])[-1] if text[:30] in pane else pane, re.I):
                return {"pane": pane}
        raise RuntimeError("slash turn never showed " + until + "\n" + pane[-1500:])

    def cell(self, box):
        d = sh(f"ls -td {box.root}/home/v3/projects/*/*/.cell 2>/dev/null | head -1").stdout.strip()
        return d, os.path.basename(os.path.dirname(d))

    def settle(self, box):
        """Quiet the chat before a snapshot: the transcript stops growing and the relay's head is the last sealed turn."""
        cdir, cell = self.cell(box)
        last, t0 = -1, time.time()
        while time.time() - t0 < 120:
            size = os.path.getsize(f"{cdir}/transcript.jsonl")
            if size == last:
                break
            last = size
            time.sleep(6)
        return cell, hv.Pass.wait_durable(self, box, cell, cdir)

    def aliases(self, ws=None):
        al = [(f"{A.root}/home", "<HOME>"), (f"{B.root}/home", "<HOME>"), (WS_A, "<WS>")]
        return al + ([(ws, "<WS>")] if ws else [])

    def snap(self, box, name, ws, cell=None):
        cell = cell or self.cell(box)[1]
        check.snap(f"{box.root}/home", cell, ws, f"{self.dir}/{name}.json", self.aliases(ws if box is B else None))
        return json.load(open(f"{self.dir}/{name}.json"))

    def b_ws(self, cell):
        return sh(f"ls -d {B.root}/home/v3/projects/*/{cell}/work").stdout.strip()

    # ---- the scenarios ----

    def pair(self):
        """Pair the two homes once and keep the pair (see reset); the runs after this one reuse it."""
        sh(f"rm -rf {A.root}/paired {B.root}/paired")
        self.reset()
        self.pair_fresh()
        for box in (A, B):
            stop(box)
            sh(f"cp -a {box.root}/home {box.root}/paired")

    def cold(self):
        self.reset(); self.ensure_pair()
        self.chat(B, f"{B.root}/work/b"); B.drive("home")
        self.chat(A, WS_A)
        for step in FIRST + SECOND:
            self.turn(A, step)
        self.res["a_settled_durable_s"] = self.settle(A)[1]
        a = self.snap(A, "a", WS_A)
        cell = a["cell"]
        # The take is the one the home screen runs (Continuer.Take), asked for by chat id: the relay keeps the chats of
        # earlier runs of this identity, and the home screen's row order cannot be told to pick the new one.
        t0 = time.time()
        take = json.loads(B.sh(f"codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])
        self.save("b-take.json", take)
        self.res["take_s"], self.res["take_error"] = round(time.time() - t0, 3), take.get("error")
        self.res["take"] = {k: take.get(k) for k in ("take_ms", "kept", "error")}
        stop(A)
        time.sleep(3)
        return self.finish(a, cell)

    def warm(self):
        self.reset(); self.ensure_pair()
        self.chat(B, f"{B.root}/work/b"); B.drive("home")
        self.chat(A, WS_A)
        for step in FIRST:
            self.turn(A, step)
        cell, _ = self.settle(A)
        take = json.loads(B.sh(f"codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])   # the older copy B will hold
        self.save("b-first-take.json", take)
        self.res["first_take_error"] = take.get("error")
        stop(A); stop(B)
        tb = json.loads(sh(f". {A.root}/env.sh; codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])   # take back
        self.save("a-take-back.json", tb)
        self.res["take_back_error"] = tb.get("error")
        self.chat(A, WS_A); A.drive("open-local")
        for step in SECOND:
            self.turn(A, step)
        self.res["a_settled_durable_s"] = self.settle(A)[1]
        a = self.snap(A, "a", WS_A)
        stop(A)
        t0 = time.time()
        tw = json.loads(B.sh(f"codeaf-vd vdemo-take {cell}").stdout.strip().splitlines()[-1])
        self.save("b-warm-take.json", tw)
        self.res["take_s"], self.res["take_error"] = round(time.time() - t0, 3), tw.get("error")
        return self.finish(a, cell)

    def finish(self, a, cell):
        b = self.snap(B, "b", self.b_ws(cell), cell)
        self.b_lines_at_take = len([l for l in b["transcript"] if l.strip()])
        cmp = check.compare(a, b)
        self.save("compare-after-take.json", cmp)
        self.res["checks_after_take"] = {k: bool(v.get("pass")) for k, v in cmp.items()}
        self.agent(a, cell)
        self.save("results.json", self.res)
        for k, ok in self.res["checks"].items():
            print("PASS" if ok else "FAIL", k)
        print("repeated calls on B:", self.res.get("repeated_calls"), "answers:", self.res.get("answers_ok"))

    def agent(self, a, cell):
        """B's model, asked to continue, then two questions only the earlier work can answer."""
        self.chat(B, f"{B.root}/work/b")
        op = B.drive("open-local")
        self.save("b-open-drive.json", op)
        time.sleep(10)   # the chat recovers its task graph and writes its moved-here note as it opens
        b1 = self.snap(B, "b-open", self.b_ws(cell), cell)
        cmp = check.compare(a, b1)
        self.save("compare.json", cmp)
        self.res["checks"] = {k: bool(v.get("pass")) for k, v in cmp.items()}
        before = self.b_lines_at_take   # everything B's agent does from the take on counts, prompted or not
        self.quiet(B)   # a chat whose last turn lost its ending may start that turn again by itself
        self.save("b-screen-before-questions.txt", td.cap())
        answers = {}
        for n, q in enumerate(QUESTIONS):
            self.save(f"b-screen-q{n}.txt", self.turn(B, q).get("pane", ""))
        time.sleep(8)
        b2 = self.snap(B, "b-after", self.b_ws(cell), cell)
        rep = check.repeats(a, b2, before)
        self.save("repeats.json", rep)
        self.res["repeated_calls"], self.res["b_calls"] = rep["count"], rep["b_calls"]
        recs = [json.loads(l) for l in b2["transcript"] if l.strip()][before:]
        users = [i for i, r in enumerate(recs) if r.get("role") == "user"]
        for n, i in enumerate(users):
            nxt = users[n + 1] if n + 1 < len(users) else len(recs)
            text = [r.get("content", "") for r in recs[i:nxt] if r.get("role") == "assistant" and r.get("content")]
            tag = re.search(r"ref-vq\d", recs[i].get("content", ""))
            answers[tag.group(0) if tag else str(n)] = text[-1] if text else ""
        self.save("b-answers.json", answers)
        self.res["answers_ok"] = {t: all(k in answers.get(t, "") for k in want) for t, want in EXPECT.items()}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("kind", choices=["cold", "warm", "pair"])
    ap.add_argument("--out", default=os.path.join(os.path.dirname(HERE), ".lane", "continuity"))
    args = ap.parse_args()
    configure()
    r = Run(args.kind, args.out)
    try:
        getattr(r, args.kind)()
    finally:
        r.save("results-partial.json", r.res)


if __name__ == "__main__":
    main()
