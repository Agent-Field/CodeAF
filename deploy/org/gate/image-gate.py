#!/usr/bin/env python3
"""The leak gate for the org stack's images. It runs on a built image BEFORE any push and exits non-zero on a finding.

    image-gate.py IMAGE [--expect-commit SHA] [--summary FILE]      IMAGE is a tag or id the local docker can `save`
    image-gate.py --tar FILE [...]                                  or a tar written by `docker save`

It never prints a value it finds: a finding names the check, the layer, the path and the rule, plus the first four
characters and the length of what matched (CI logs of a public repository are public). Exit 0: clean. Exit 1: a finding.
Exit 2: the gate itself could not run (a scanner that did not run is a failure, never a pass).

WHAT IT CHECKS (README.md "Published images" repeats this list; change both together):

  A1  trivy secret scan (the pinned image below, built-in rules plus gate/trivy-secret.yaml) over the image's files.
  A2  key and token SHAPES (OpenRouter, Anthropic, OpenAI, GitHub, AWS, Google, Slack, Stripe keys, JWTs, PEM private-key
      blocks, a docker/npm "auth" blob) in the bytes of every file of EVERY layer, binaries included, and in every
      layer a later layer deleted (the layer, not the merged filesystem, is what gets pushed). Skipped: package
      documentation, man pages, locale and zoneinfo data, the package databases, CA bundles, the Python and Perl
      standard libraries (NOISE_PREFIXES below).
  B1  the image configuration: every ENV and label value, the entrypoint, command and health check. A variable whose name
      says secret (key, token, secret, password, credential, auth) with a value that is not empty, a path, a number or a
      boolean fails; so does any value shaped like a key (A2's shapes).
  B2  the build history (`docker history --no-trunc`): any ARG whose NAME says secret fails (declared or not, valued or
      not: a secret is never a build argument), any NAME=value in a build line that B1 would refuse fails, and any key
      shape in the text fails. A build argument's value is recorded in the history, which is why it is refused.
  C1  private hostnames and addresses: railway.app, up.railway and railway.internal anywhere, binaries included;
      credentials in a localhost URL (scheme://user:password@localhost) anywhere, binaries included; any other `.internal`
      host name (except the well-known public ones in WELL_KNOWN_INTERNAL) and 10./192.168./172.16-31. addresses in TEXT
      files outside the distribution's own trees (SYSTEM_PREFIXES: /usr/lib, /usr/share, /bin, /usr/bin, ...): the scripts,
      configuration and copied files our build added. Compiled programs are built from public source, so an example address
      in one's error message is not a leak; a range written with a /prefix length is a network, not a host. The strong
      patterns also run over labels and the configuration.
  C2  with --expect-commit: the image's org.opencontainers.image.revision label is that commit.

  NOT SCANNED, AND SAID SO: a file that a package manager installed and that is still byte for byte what it installed. dpkg
  records the md5 of every file of a package (`/var/lib/dpkg/info/*.md5sums`, and the `Conffiles` of `/var/lib/dpkg/status`
  for configuration files) and pip records a sha256 (`*.dist-info/RECORD`). Such a file is the package's published content,
  public by construction, and it is where the false positives are (libgnutls carries test private keys, a PAM sample file
  carries example addresses). The file is hashed; only a match exempts it, so a file edited after the install, or planted
  under a package's name, is scanned like any other. The count of exempted files is printed, and trivy findings on them
  are dropped the same way. Our own files (the codeaf binary, scripts, configuration) are never in these lists.
"""
import argparse
import base64
import csv
import gzip
import hashlib
import io
import json
import os
import posixpath
import re
import shlex
import subprocess
import sys
import tarfile
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))

# The scanner is pinned by digest (tag kept for the reader); change both together. A container, not a marketplace
# action: the same command runs on a laptop and in CI, and an image by digest cannot be re-pointed.
TRIVY_IMAGE = "ghcr.io/aquasecurity/trivy:0.76.2@sha256:8f6bb947dbc528902e11f2f5be52f7b519f6a39d5d148f41166f2040499768fb"

MAX_FILE_BYTES = 600 * 1024 * 1024

# Excluded from every file scan: distribution data that is large, is not ours, and holds example credentials.
NOISE_PREFIXES = (
    "usr/share/doc/", "usr/share/doc-base/", "usr/share/man/", "usr/share/info/", "usr/share/locale/",
    "usr/share/zoneinfo/", "usr/share/perl", "usr/share/lintian/", "usr/share/bash-completion/",
    "usr/share/git-core/", "usr/share/gitweb/", "usr/share/ca-certificates/", "usr/lib/python3", "usr/lib/git-core/",
    "usr/lib/ssl/", "var/lib/dpkg/", "var/lib/apt/", "var/cache/", "var/log/", "lib/apk/", "etc/ssl/certs/",
    "etc/ca-certificates",
)
# Excluded from the weak host patterns only (private addresses, .internal names): the distribution's own trees, whose
# binaries and data hold such strings legitimately. Everything else, and `usr/local`, is ours.
SYSTEM_PREFIXES = (
    "usr/lib/", "usr/share/", "lib/", "lib64/", "bin/", "sbin/", "usr/bin/", "usr/sbin/", "usr/libexec/",
    "usr/include/", "etc/ssl/", "etc/alternatives/", "etc/apt/", "etc/dpkg/", "etc/perl/", "etc/python3",
)
WELL_KNOWN_INTERNAL = {
    "metadata.google.internal", "host.docker.internal", "gateway.docker.internal", "kubernetes.docker.internal",
    "docker.internal",
}

# name, regex. Specific shapes only: a generic "password = ..." rule would fail on every script that reads one.
KEY_SHAPES = [
    ("openrouter-key", r"sk-or-v1-[0-9a-f]{64}"),
    ("anthropic-key", r"sk-ant-[A-Za-z0-9_\-]{32,}"),
    ("openai-key", r"sk-proj-[A-Za-z0-9_\-]{40,}|sk-[A-Za-z0-9_\-]{20,}T3BlbkFJ[A-Za-z0-9_\-]{20,}"),
    ("github-token", r"\bgh[pousr]_[A-Za-z0-9]{36,}"),
    ("github-fine-grained-token", r"github_pat_[A-Za-z0-9_]{60,}"),
    ("aws-access-key-id", r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b"),
    ("google-api-key", r"\bAIza[0-9A-Za-z_\-]{35}"),
    ("slack-token", r"\bxox[abposr]-[0-9A-Za-z\-]{20,}"),
    ("stripe-live-key", r"\b[sr]k_live_[0-9a-zA-Z]{24,}"),
    ("jwt", r"\beyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}"),
    ("private-key-block", r"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----[\r\n\s\\n]*[A-Za-z0-9+/=]{40,}"),
    ("registry-auth-blob", r'"auth"\s*:\s*"[A-Za-z0-9+/=]{20,}"'),
]
# Railway, and credentials in a localhost URL: wrong in any file, so no distribution exemption.
HOST_STRONG = [
    ("railway-domain", r"(?i)\brailway\.app\b"),
    ("railway-edge-host", r"(?i)\bup\.railway\b"),
    ("railway-private-name", r"(?i)\brailway\.internal\b"),
    ("localhost-credentials", r"\b[a-z][a-z0-9+.\-]*://[^/\s:@]+:[^/\s@]+@(?:localhost|127\.\d+\.\d+\.\d+|\[::1\])(?![\w.\-])"),
]
INTERNAL_HOST = r"\b(?:[a-z0-9][a-z0-9\-]*\.)+internal\b(?![\w/\-])"
PRIVATE_IP = (r"(?<![\w.])(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}"
              r"|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})(?!\d|\.\d|/\d)")

# A variable NAME says secret when one of its words is one of these.
SECRET_WORDS = {"KEY", "KEYS", "APIKEY", "TOKEN", "TOKENS", "SECRET", "SECRETS", "PASSWORD", "PASSWD", "PASSPHRASE",
                "CREDENTIAL", "CREDENTIALS", "AUTH", "AUTHORIZATION"}
BENIGN_VALUES = {"true", "false", "yes", "no", "on", "off", "none", "null", "0", "1"}


def secret_name(name):
    words = re.split(r"[_\-.\s]+|(?<=[a-z])(?=[A-Z])", name)
    return any(w.upper() in SECRET_WORDS for w in words if w)


def benign_value(value):
    v = value.strip().strip("\"'")
    return (len(v) < 8 or v.lower() in BENIGN_VALUES or v.startswith("/") or v.startswith("./")
            or re.fullmatch(r"\d+(\.\d+)*", v) is not None)


def preview(text):
    if isinstance(text, bytes):
        text = text.decode("latin-1")
    text = text.strip()
    return f"{text[:4]}... ({len(text)} chars)"


class Findings:
    def __init__(self):
        self.by_check = {}

    def add(self, check, where, rule, matched):
        key = (where, rule, preview(matched))
        self.by_check.setdefault(check, {})
        self.by_check[check][key] = self.by_check[check].get(key, 0) + 1

    def count(self, check):
        return len(self.by_check.get(check, {}))

    def lines(self, check, limit=40):
        out = []
        for (where, rule, prev), n in list(self.by_check.get(check, {}).items())[:limit]:
            out.append(f"{where}: {rule}  [{prev}]" + (f" x{n}" if n > 1 else ""))
        extra = self.count(check) - limit
        if extra > 0:
            out.append(f"... and {extra} more")
        return out


def docker_cmd():
    return shlex.split(os.environ.get("DOCKER", "docker"))


def save_image(image, dest):
    with open(dest, "wb") as fh:
        proc = subprocess.run(docker_cmd() + ["save", image], stdout=fh, stderr=subprocess.PIPE)
    if proc.returncode != 0:
        raise RuntimeError("docker save %s failed: %s" % (image, proc.stderr.decode(errors="replace").strip()[-300:]))


def read_member(tf, name):
    f = tf.extractfile(tf.getmember(name))
    return f.read() if f else b""


def layer_tar(tf, name):
    """A tarfile positioned at one layer of a `docker save` archive; layers may be plain or gzip."""
    raw = tf.extractfile(tf.getmember(name))
    magic = raw.read(4)
    raw.seek(0)
    if magic[:2] == b"\x1f\x8b":
        return tarfile.open(fileobj=gzip.GzipFile(fileobj=raw), mode="r|")
    if magic == b"\x28\xb5\x2f\xfd":
        raise RuntimeError("zstd-compressed layers are not supported by this gate: " + name)
    return tarfile.open(fileobj=raw, mode="r|")


def compile_all(table, flags=0):
    return [(n, re.compile(p.encode(), flags)) for n, p in table]


KEY_RES = compile_all(KEY_SHAPES)
HOST_STRONG_RES = compile_all(HOST_STRONG)
INTERNAL_RE = re.compile(INTERNAL_HOST.encode(), re.I)
PRIVATE_IP_RE = re.compile(PRIVATE_IP.encode())


def canon(path):
    """A path as the package databases spell it, comparable with a layer's: no leading slash, and the usr-merge links
    (/bin, /sbin, /lib, /lib64 -> /usr/...) folded in, because dpkg lists some files under one name and some under the other."""
    path = posixpath.normpath(path.lstrip("/"))
    top = path.split("/", 1)[0]
    if top in ("bin", "sbin", "lib", "lib32", "lib64", "libx32"):
        return "usr/" + path
    return path


RECORD_RE = re.compile(r"^(?P<root>.*?)/?[^/]+\.dist-info/RECORD$")


def add_owned(owned, path, kind, digest):
    owned.setdefault(canon(path), set()).add(f"{kind}:{digest}")


def parse_dpkg_status(owned, text):
    """Conffiles: ` /etc/x <md5>` lines in the continuation of a `Conffiles:` field."""
    in_conffiles = False
    for line in text.splitlines():
        if line.startswith("Conffiles:"):
            in_conffiles = True
        elif in_conffiles and line.startswith((" ", "\t")):
            parts = line.split()
            if len(parts) >= 2 and parts[0].startswith("/") and re.fullmatch(r"[0-9a-f]{32}", parts[1]):
                add_owned(owned, parts[0], "md5", parts[1])
        else:
            in_conffiles = False


def collect_owned(tf, layers):
    """Pass 1: the (path, hash) pairs the package managers recorded, from every layer (the lists are small)."""
    owned = {}
    for name in layers:
        with layer_tar(tf, name) as lt:
            for member in lt:
                if not member.isreg() or member.size > 64 * 1024 * 1024:
                    continue
                path = re.sub(r"^(?:\./)+", "", member.name).lstrip("/")
                is_md5 = path.startswith("var/lib/dpkg/info/") and path.endswith(".md5sums")
                is_status = path == "var/lib/dpkg/status" or path.startswith("var/lib/dpkg/status.d/")
                record = RECORD_RE.match(path) if path.endswith("/RECORD") else None
                if not (is_md5 or is_status or record):
                    continue
                text = lt.extractfile(member).read().decode("utf-8", "replace")
                if is_md5:
                    for line in text.splitlines():
                        m = re.fullmatch(r"([0-9a-f]{32})\s+\*?(.+)", line.strip())
                        if m:
                            add_owned(owned, m.group(2), "md5", m.group(1))
                elif is_status:
                    parse_dpkg_status(owned, text)
                else:
                    root = record.group("root")
                    for row in csv.reader(io.StringIO(text)):
                        if len(row) >= 2 and row[1].startswith("sha256="):
                            add_owned(owned, posixpath.join(root, row[0]), "sha256", row[1][len("sha256="):].rstrip("="))
    return owned


def is_installed_unmodified(owned, path, data):
    wanted = owned.get(canon(path))
    if not wanted:
        return False
    if any(w.startswith("md5:") for w in wanted) and "md5:" + hashlib.md5(data).hexdigest() in wanted:
        return True
    if any(w.startswith("sha256:") for w in wanted):
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip("=")
        return "sha256:" + digest in wanted
    return False


def scan_bytes(findings, where, data, under_system):
    for rule, rx in KEY_RES:
        for m in rx.finditer(data):
            findings.add("A2", where, rule, m.group(0))
    for rule, rx in HOST_STRONG_RES:
        for m in rx.finditer(data):
            findings.add("C1", where, rule, m.group(0))
    # The weak patterns look at TEXT files (no NUL byte near the start) outside the distribution's trees. A compiled program
    # is built from public source (this repository, SWE-AF and PR-AF at pinned commits, the published control plane): an
    # example address in one of its error messages (codeaf's own say `such as http://10.0.0.5:8080`) is in that public source
    # already. What the gate guards is what the build context and the build steps added: scripts, configuration, copied files.
    # A key, a Railway host or localhost credentials are refused in binaries too (the strong patterns above).
    if under_system or b"\x00" in data[:8192]:
        return
    if b".internal" in data:
        for m in INTERNAL_RE.finditer(data):
            host = m.group(0).decode("latin-1").lower()
            if host not in WELL_KNOWN_INTERNAL and "railway.internal" not in host:
                findings.add("C1", where, "internal-hostname", m.group(0))
    for m in PRIVATE_IP_RE.finditer(data):
        findings.add("C1", where, "private-address", m.group(0))


def scan_layers(tf, layers, findings, owned):
    """Pass 2. Returns (files scanned, the paths exempted as installed unmodified by a package manager)."""
    files = 0
    exempt = set()
    for index, name in enumerate(layers, start=1):
        with layer_tar(tf, name) as lt:
            for member in lt:
                if not member.isreg():
                    continue
                path = re.sub(r"^(?:\./)+", "", member.name).lstrip("/")
                base = os.path.basename(path)
                if base.startswith(".wh."):
                    continue
                if path.startswith(NOISE_PREFIXES):
                    continue
                if member.size > MAX_FILE_BYTES:
                    findings.add("A2", f"layer {index} /{path}", "file-too-large-to-scan", b"x" * 4)
                    continue
                data = lt.extractfile(member).read()
                if is_installed_unmodified(owned, path, data):
                    exempt.add(canon(path))
                    continue
                files += 1
                scan_bytes(findings, f"layer {index} /{path}", data, path.startswith(SYSTEM_PREFIXES))
    return files, exempt


def scan_text_for_shapes(findings, check, where, text):
    data = text.encode("utf-8", "replace")
    for rule, rx in KEY_RES:
        for m in rx.finditer(data):
            findings.add(check, where, rule, m.group(0))
    for rule, rx in HOST_STRONG_RES:
        for m in rx.finditer(data):
            findings.add("C1", where, rule, m.group(0))


def check_config(findings, config):
    cfg = config.get("config") or {}
    for item in cfg.get("Env") or []:
        name, _, value = item.partition("=")
        if secret_name(name) and not benign_value(value):
            findings.add("B1", f"ENV {name}", "secret-named-variable-with-a-value", value)
        scan_text_for_shapes(findings, "B1", f"ENV {name}", value)
    for name, value in (cfg.get("Labels") or {}).items():
        if secret_name(name) and not benign_value(value):
            findings.add("B1", f"label {name}", "secret-named-label-with-a-value", value)
        scan_text_for_shapes(findings, "B1", f"label {name}", value)
    for field in ("Entrypoint", "Cmd", "Healthcheck", "User", "WorkingDir", "Volumes", "ExposedPorts"):
        scan_text_for_shapes(findings, "B1", f"config {field}", json.dumps(cfg.get(field)))


ASSIGN = re.compile(r"(?<![\w$])([A-Za-z_][A-Za-z0-9_]*)=(\"[^\"]*\"|'[^']*'|\S+)")
ARG_DECL = re.compile(r"(?:^|\s)ARG\s+([A-Za-z_][A-Za-z0-9_]*)")


def check_history(findings, config):
    for number, entry in enumerate(config.get("history") or [], start=1):
        line = entry.get("created_by") or ""
        where = f"history step {number}"
        for m in ARG_DECL.finditer(line):
            if secret_name(m.group(1)):
                findings.add("B2", where, "secret-named-build-argument", m.group(1))
        for m in ASSIGN.finditer(line):
            name, value = m.group(1), m.group(2)
            if secret_name(name) and not benign_value(value):
                findings.add("B2", where, "secret-named-assignment-in-build-line", value)
        scan_text_for_shapes(findings, "B2", where, line)


def run_trivy(tar_path, findings, layer_count, exempt=frozenset()):
    """A1. Returns (layers scanned, error). The scanner's JSON is read for rule, file and line only: its Match text is dropped.
    A scan that did not read every layer is an error, not a pass: a scanner that saw nothing says the same as one that saw nothing wrong."""
    cfg = os.path.join(HERE, "trivy-secret.yaml")
    common = ["image", "--scanners", "secret", "--format", "json", "--quiet", "--skip-version-check"]
    if os.environ.get("TRIVY_BIN"):
        cmd = [os.environ["TRIVY_BIN"]] + common + ["--secret-config", cfg, "--input", tar_path]
    else:
        cmd = docker_cmd() + ["run", "--rm", "-v", f"{tar_path}:/gate/image.tar:ro", "-v", f"{cfg}:/gate/trivy-secret.yaml:ro",
                              TRIVY_IMAGE] + common + ["--secret-config", "/gate/trivy-secret.yaml", "--input", "/gate/image.tar"]
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode != 0:
        return None, "trivy exited %d: %s" % (proc.returncode, proc.stderr.decode(errors="replace").strip()[-400:])
    try:
        report = json.loads(proc.stdout or b"{}")
    except ValueError as exc:
        return None, "trivy output was not JSON: %s" % exc
    scanned = len((report.get("Metadata") or {}).get("DiffIDs") or [])
    if scanned != layer_count:
        return None, "trivy read %d of the image's %d layers" % (scanned, layer_count)
    for result in report.get("Results") or []:
        if canon(result.get("Target", "?")) in exempt:
            continue
        for secret in result.get("Secrets") or []:
            where = "%s:%s" % (result.get("Target", "?"), secret.get("StartLine", "?"))
            findings.add("A1", where, "trivy:%s (%s)" % (secret.get("RuleID", "?"), secret.get("Severity", "?")), b"redacted")
    return scanned, None


CHECKS = [
    ("A1", "trivy secret scan (built-in rules + trivy-secret.yaml)"),
    ("A2", "key and token shapes in every layer's files"),
    ("B1", "image configuration: ENV, labels, entrypoint, command"),
    ("B2", "build history: build arguments and key shapes"),
    ("C1", "private hostnames and addresses"),
    ("C2", "source commit label"),
]


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("image", nargs="?", help="image tag or id to `docker save`")
    ap.add_argument("--tar", help="a `docker save` archive instead of an image")
    ap.add_argument("--expect-commit", help="the commit the revision label must name (C2)")
    ap.add_argument("--summary", help="append a Markdown summary to this file (CI passes $GITHUB_STEP_SUMMARY)")
    args = ap.parse_args()
    if not args.image and not args.tar:
        ap.error("an image or --tar is required")

    findings = Findings()
    errors = []
    label = args.image or os.path.basename(args.tar)
    tmp = None
    tar_path = args.tar
    try:
        if not tar_path:
            tmp = tempfile.TemporaryDirectory(prefix="image-gate-")
            tar_path = os.path.join(tmp.name, "image.tar")
            save_image(args.image, tar_path)
        tar_path = os.path.abspath(tar_path)

        with tarfile.open(tar_path) as tf:
            manifest = json.loads(read_member(tf, "manifest.json"))
            entry = manifest[0]
            config = json.loads(read_member(tf, entry["Config"]))
            layers = entry["Layers"]
            check_config(findings, config)
            check_history(findings, config)
            commit = (config.get("config") or {}).get("Labels", {}).get("org.opencontainers.image.revision", "")
            if args.expect_commit and commit != args.expect_commit:
                findings.add("C2", "label org.opencontainers.image.revision", "revision-label-is-not-the-built-commit", commit.encode() or b"empty")
            owned = collect_owned(tf, layers)
            nfiles, exempt = scan_layers(tf, layers, findings, owned)

        layers_scanned, err = run_trivy(tar_path, findings, len(layers), exempt)
        if err:
            errors.append("A1: " + err)
    except Exception as exc:  # the gate could not run: not a pass
        errors.append("%s: %s" % (type(exc).__name__, exc))
        nfiles, exempt = 0, set()
    finally:
        if tmp:
            tmp.cleanup()

    print("image gate: %s" % label)
    failed = False
    rows = []
    for check, title in CHECKS:
        if check == "C2" and not args.expect_commit:
            continue
        n = findings.count(check)
        status = "FAIL" if n else "PASS"
        if any(e.startswith(check + ":") for e in errors):
            status = "ERROR"
        failed = failed or status != "PASS"
        print("  [%s] %s  %s: %d finding(s)" % (status, check, title, n))
        for line in findings.lines(check):
            print("         " + line)
        rows.append((check, title, status, n))
    note = "%d files scanned; %d more were installed by dpkg or pip and are unmodified, so they were not (see the header of image-gate.py)" % (nfiles, len(exempt))
    print("  " + note)
    for e in errors:
        print("  gate error: " + e)
    verdict = "FAIL" if findings.by_check else ("ERROR" if errors else "PASS")
    print("GATE: %s" % verdict)

    if args.summary:
        try:
            with open(args.summary, "a") as fh:
                fh.write("### Leak gate: `%s` — %s\n\n| check | what | result | findings |\n|---|---|---|---|\n" % (label, verdict))
                for check, title, status, n in rows:
                    fh.write("| %s | %s | %s | %d |\n" % (check, title, status, n))
                fh.write("\n")
                for check, _ in CHECKS:
                    for line in findings.lines(check):
                        fh.write("- %s %s\n" % (check, line))
                fh.write("- %s\n" % note)
                for e in errors:
                    fh.write("- gate error: %s\n" % e)
                fh.write("\n")
        except OSError as exc:
            print("could not write the summary: %s" % exc, file=sys.stderr)

    if findings.by_check:
        sys.exit(1)
    if errors:
        sys.exit(2)


if __name__ == "__main__":
    main()
