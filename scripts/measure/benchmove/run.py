#!/usr/bin/env python3
"""run.py: the move benchmark's orchestrator (called by scripts/bench-move.sh).

For each environment and repo size it drives the `benchmove` probe on two
devices, A (this box, always) and B (this box's second home, or the second machine), through
the one relay, with scripted edits and no model:

  first    A seals the whole project and uploads it (chat created)
  cold     B forgets the chat, then takes it            x reps
  prime    B seals one tiny turn (a device that has driven the chat), A takes back
  per kind (one file, fifty files, +big MB), x reps:
    a_seal     A edits and seals                          publish A -> relay
    warm       B takes it (B holds the previous head)
    b_seal     B edits the same way and seals             publish B -> relay
    takeback   A takes it back (A holds the previous head, in place)
  latency  n back-to-back one-file seals on A and on B, sync interval 200 ms and the 5 s default

Every result row carries the product sha. Rows go to <out>/rows.jsonl as they finish.
"""
import argparse, json, os, shlex, subprocess, sys, time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import rigenv  # noqa: E402

K = None  # the corpus folder, read from CODEAF_CORPUS by main() once the arguments are parsed
CLASSES = {"S": "r18-pareto-c365", "M": "r02-mj-base", "L": "r06-agentfield", "XL": "r03-hax-sdk"}


class Box:
    """One device: how to run a shell command on it, and where it keeps its bench folder."""

    def __init__(self, name, ssh, scratch, bm, furrow, url, path_extra=""):
        self.name, self.ssh, self.scratch, self.bm, self.furrow, self.url = name, ssh, scratch, bm, furrow, url
        self.path_extra = path_extra

    def sh(self, cmd, timeout=None, check=True):
        pre = f"export PATH={self.path_extra}$PATH; " if self.path_extra else ""
        argv = ["ssh", "-o", "BatchMode=yes", self.ssh, "bash", "-c", shlex.quote(pre + cmd)] if self.ssh else ["bash", "-c", pre + cmd]
        r = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        if check and r.returncode:
            raise RuntimeError(f"{self.name}: {cmd[:200]} -> rc {r.returncode}: {r.stderr[-800:]}")
        return r

    def probe(self, verb, root, interval_ms=None, timeout=7200, **flags):
        env = f"CODEAF_SYNC_URL={self.url} CODEAF_CELLS=1 BENCH_FURROW={self.furrow}"
        if interval_ms:
            env += f" CODEAF_SYNC_INTERVAL_MS={interval_ms}"
        args = " ".join(f"-{k} {shlex.quote(str(v))}" for k, v in flags.items())
        r = self.sh(f"env {env} {self.bm} {verb} -root {root} -device {self.name} {args}", timeout=timeout, check=False)
        lines = [l for l in r.stdout.splitlines() if l.startswith("{")]
        out = json.loads(lines[-1]) if lines else {}
        if r.returncode or "error" in out:
            raise RuntimeError(f"{self.name} {verb} failed rc={r.returncode}: {out.get('error', '')} {r.stderr[-600:]}")
        return out


def need(flag, value, variable):
    """A setting the second machine cannot do without, named by its flag and its variable when absent."""
    if not value:
        sys.exit(f"{flag} is not set: pass it or export {variable} (docs/testing-anywhere.md lists every setting).")
    return value


def log(*a):
    print(time.strftime("%T"), *a, file=sys.stderr, flush=True)


def summarize_take(t):
    return {"total_ms": t["total_ms"], "phases_ms": t["phases_ms"], "warm": t["warm"], "wire": t["wire"],
            "engine": t["engine"], "kept": t["kept"]}


class Run:
    def __init__(self, a, args, sha):
        self.a, self.args, self.sha = a, args, sha
        self.rows = open(Path(args.out) / "rows.jsonl", "a")

    def row(self, **kw):
        kw.update(sha=self.sha, ts=time.strftime("%FT%TZ", time.gmtime()), load_a=float(open("/proc/loadavg").read().split()[0]))
        self.rows.write(json.dumps(kw) + "\n")
        self.rows.flush()

    def sealed(self, box, ctx, res):
        for t in res["turns"]:
            self.row(**ctx, machine=box.name, drive_open_ms=res["drive_open_ms"],
                     close_ms=res["close_ms"], turn=t)

    def matrix(self, env, b, size):
        args, a = self.args, self.a
        repo = CLASSES[size]
        ra = f"{a.scratch}/{env.replace(' ', '_')}/{repo}/a"
        rb = f"{b.scratch}/{env.replace(' ', '_')}/{repo}/b"
        a.sh(f"rm -rf {ra}; mkdir -p {ra}")
        b.sh(f"rm -rf {rb}; mkdir -p {rb}")
        a.sh(f"cp -a --reflink=auto {K}/{repo} {ra}/work && rm -rf {ra}/work/.furrow")
        size_b = int(a.sh(f"du -sxb {ra}/work | cut -f1").stdout.split()[0])
        idf = f"{ra}/id.blob"
        a.sh(f"{a.bm} id-export -home {ra}/home -out {idf}")
        if b.ssh:
            subprocess.run(["scp", "-q", "-o", "BatchMode=yes", idf, f"{b.ssh}:{rb}/id.blob"], check=True)
        else:
            b.sh(f"cp {idf} {rb}/id.blob")
        b.sh(f"{b.bm} id-import -home {rb}/home -in {rb}/id.blob && rm -f {rb}/id.blob")
        a.sh(f"rm -f {idf}")
        cid = a.probe("create", ra, work=f"{ra}/work")["cell"]
        ctx0 = dict(env=env, size=size, repo=repo, repo_bytes=size_b)
        log(env, size, repo, size_b, "bytes; chat", cid)
        wb = f"{rb}/chats/{cid}/work"
        seed = [0]

        def nxt():
            seed[0] += 1
            return seed[0]

        # first upload
        res = a.probe("seal", ra, cell=cid, work=f"{ra}/work", kind="tiny", n=1, seed=nxt(), interval_ms=200)
        self.sealed(a, dict(ctx0, scenario="first_upload", kind="whole_repo", rep=0), res)
        log(" first upload", round(res["turns"][0]["durable_ms"]), "ms", res["turns"][0]["bytes_up"], "bytes")
        # cold takes
        for rep in range(1, args.reps + 1):
            b.probe("wipe", rb, cell=cid)
            t = b.probe("take", rb, cell=cid)
            assert not t["warm"], "cold take found a store"
            self.row(**ctx0, scenario="cold_take", kind="none", rep=rep, machine=b.name, take=summarize_take(t))
            log(" cold", rep, round(t["total_ms"]), "ms", t["wire"].get("store_get", {}).get("n"), "gets")
        # prime: B has now driven the chat once (its engine watches the tree), A takes back
        res = b.probe("seal", rb, cell=cid, work=wb, kind="tiny", n=1, seed=nxt(), interval_ms=200)
        self.sealed(b, dict(ctx0, scenario="prime_b_seal", kind="tiny", rep=0), res)
        t = a.probe("take", ra, cell=cid)
        self.row(**ctx0, scenario="prime_takeback", kind="tiny", rep=0, machine=a.name, take=summarize_take(t))
        # warm cycles per change kind
        for kind in args.kinds:
            for rep in range(1, args.reps + 1):
                try:
                    ctx = dict(ctx0, kind=kind, rep=rep)
                    extra = {"mb": args.big_mb} if kind == "big" else {}
                    res = a.probe("seal", ra, cell=cid, work=f"{ra}/work", kind=kind, n=1, seed=nxt(), interval_ms=200, **extra)
                    self.sealed(a, dict(ctx, scenario="publish_a"), res)
                    t = b.probe("take", rb, cell=cid)
                    assert t["warm"], "warm take found no store"
                    self.row(**ctx, scenario="warm_take", machine=b.name, take=summarize_take(t))
                    res = b.probe("seal", rb, cell=cid, work=wb, kind=kind, n=1, seed=nxt(), interval_ms=200, **extra)
                    self.sealed(b, dict(ctx, scenario="publish_b"), res)
                    t2 = a.probe("take", ra, cell=cid)
                    self.row(**ctx, scenario="takeback", machine=a.name, take=summarize_take(t2))
                    log(f" {kind} rep{rep}: warm {round(t['total_ms'])} ms ({t['wire'].get('store_get', {}).get('n', 0)} gets), takeback {round(t2['total_ms'])} ms")
                except (RuntimeError, AssertionError) as e:
                    # a failed step is a result (for example a request that timed out), not the end of the matrix
                    self.row(**dict(ctx0, kind=kind, rep=rep), scenario="error", error=str(e)[:500])
                    log(f" {kind} rep{rep} FAILED: {str(e)[:160]}")
                    try:
                        a.probe("take", ra, cell=cid)  # resync A with the head B may have published
                    except RuntimeError:
                        pass
        # per tool call latency, on A then on B (B takes first)
        for who, box, root, work in (("a", a, ra, f"{ra}/work"), ("b", b, rb, wb)):
            if who == "b":
                b.probe("take", rb, cell=cid)
            for label, interval, n in (("interval_200ms", 200, args.latency_n), ("interval_default_5s", None, max(4, args.latency_n // 2))):
                res = box.probe("seal", root, cell=cid, work=work, kind="one", n=n, seed=nxt(), interval_ms=interval)
                self.sealed(box, dict(ctx0, scenario="latency_" + label, kind="one", rep=0), res)
                d = sorted(t["durable_ms"] for t in res["turns"])
                log(f" latency {who} {label}: median durable {d[len(d)//2]:.0f} ms, seal {sorted(t['seal_ms'] for t in res['turns'])[len(d)//2]:.0f} ms")
            if who == "b":
                a.probe("take", ra, cell=cid)
        if not args.keep:
            a.sh(f"rm -rf {ra}")
            b.sh(f"rm -rf {rb}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--sha", required=True)
    ap.add_argument("--envs", default="same")
    ap.add_argument("--sizes", default="S,M,L")
    ap.add_argument("--reps", type=int, default=3)
    ap.add_argument("--kinds", default="one,fifty,big")
    ap.add_argument("--big-mb", type=int, default=100)
    ap.add_argument("--latency-n", type=int, default=20)
    ap.add_argument("--url", default="http://127.0.0.1:18787")
    ap.add_argument("--scratch", default=os.environ.get("BENCH_SCRATCH", str(Path.home() / "bench-move-scratch")))
    ap.add_argument("--bin", required=True, help="dir with benchmove-linux, benchmove-darwin, furrow-linux")
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--second-host", default=os.environ.get("CODEAF_SECOND_HOST"), help="the second machine (default: $CODEAF_SECOND_HOST)")
    ap.add_argument("--second-root", default=os.environ.get("CODEAF_SECOND_ROOT"), help="its bench folder holding bin/ and run/ (default: $CODEAF_SECOND_ROOT)")
    args = ap.parse_args()
    global K
    K = Path(rigenv.get("CODEAF_CORPUS"))
    args.kinds = args.kinds.split(",")
    Path(args.out).mkdir(parents=True, exist_ok=True)
    bin_ = Path(args.bin)
    urls = {"hosted": lambda: rigenv.get("CODEAF_RELAY"), "cf-l": lambda: "http://127.0.0.1:18789"}
    label = {"same": "same-box", "second": "two-machines", "hosted": "hosted two-machines", "cf-l": "local-worker two-machines"}
    run = None
    for env in args.envs.split(","):
        url = urls.get(env, lambda: args.url)()
        a = Box("A", None, args.scratch, str(bin_ / "benchmove-linux"), str(bin_ / "furrow-linux"), url)
        if env == "same":
            b = Box("A2", None, args.scratch, a.bm, a.furrow, url)
        else:  # B reaches the docker relay through ssh -R, and a hosted relay directly
            host, root = need("--second-host", args.second_host, "CODEAF_SECOND_HOST"), need("--second-root", args.second_root, "CODEAF_SECOND_ROOT")
            b = Box("B", host, root + "/run", root + "/bin/benchmove", root + "/bin/furrow", url, "/opt/homebrew/bin:")
        run = Run(a, args, args.sha)
        for size in args.sizes.split(","):
            log("=== env", env, "size", size, "url", url)
            run.matrix(label[env], b, size)
    log("done")


if __name__ == "__main__":
    main()
