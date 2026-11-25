#!/usr/bin/env bash
# Proves that the leak gate (image-gate.py) works: throwaway images with FAKE secrets planted in them must fail it,
# each with the check that is meant to catch that kind of leak, and a clean image (with strings that look alarming but
# are fine) must pass. It builds from a busybox base, tags nothing for any registry, pushes nothing and removes what
# it built, by exact name. CI runs it before every image build (.github/workflows/org-images.yml); run it by hand with
#
#     DOCKER=docker TRIVY_BIN=/path/to/trivy ./selftest.sh      # DOCKER and TRIVY_BIN are optional, see image-gate.py
#
# The fake values are made at run time from random characters, and written here in pieces, so this file holds nothing
# a secret scanner would take for a credential.
set -u
cd "$(dirname "$0")" || exit 2
DOCKER=${DOCKER:-docker}
read -r -a DOCKER_ARGV <<<"$DOCKER"
BASE='busybox:1.37.0-musl@sha256:f6a95d611e0a8780fabdb2aec43e31275b2720dfefa3e41b16835bfc42454478'
RUN_ID="$(date +%s)-$$"
WORK="$(mktemp -d)"
BUILT=()
cleanup() {
  for image in "${BUILT[@]}"; do "${DOCKER_ARGV[@]}" rmi -f "$image" >/dev/null 2>&1; done
  rm -rf "$WORK"
}
trap cleanup EXIT

rand_alnum() { python3 -c 'import secrets,string,sys; print("".join(secrets.choice(string.ascii_letters+string.digits) for _ in range(int(sys.argv[1]))))' "$1"; }
rand_hex()   { python3 -c 'import secrets,sys; print(secrets.token_hex(int(sys.argv[1])))' "$1"; }
md5_of()     { python3 -c 'import hashlib,sys; print(hashlib.md5(open(sys.argv[1],"rb").read()).hexdigest())' "$1"; }
sha256_b64_of() { python3 -c 'import base64,hashlib,sys; print(base64.urlsafe_b64encode(hashlib.sha256(open(sys.argv[1],"rb").read()).digest()).rstrip(b"=").decode())' "$1"; }
rand_b64()   { python3 -c 'import base64,secrets,sys; print(base64.b64encode(secrets.token_bytes(int(sys.argv[1]))).decode())' "$1"; }

OPENROUTER_FAKE="sk-or-""v1-$(rand_hex 32)"
GITHUB_FAKE="gh""p_$(rand_alnum 36)"
PEM_BEGIN="-----BEGIN ""PRIVATE KEY-----"
PEM_END="-----END ""PRIVATE KEY-----"
PEM_FAKE="${PEM_BEGIN}"$'\n'"$(rand_b64 150 | fold -w 64)"$'\n'"${PEM_END}"
AUTH_FAKE="{\"auths\":{\"registry.example.org\":{\"auth\":\"$(rand_b64 30 | tr -d '\n')\"}}}"
RAILWAY_FAKE="db-$(rand_alnum 6).up.rail""way.app"
RAILWAY_PRIVATE_FAKE="postgres://db.rail""way.internal:5432/app"

pass=0
fail=0
results=()

# build_and_gate NAME EXPECT_EXIT EXPECT_CHECK [extra gate args] -- reads the Dockerfile body from stdin; every file written into
# $WORK/NAME/ctx beforehand is the build context (COPY it). EXPECT_CHECK is the check that must report, or "-" for none.
build_and_gate() {
  local name="$1" want_exit="$2" want_check="$3"; shift 3
  local dir="$WORK/$name" tag="org-gate-selftest-${name}:${RUN_ID}"
  { echo "FROM $BASE"; cat; } >"$dir/Dockerfile"
  BUILT+=("$tag")
  if ! tar -C "$dir" -cf - . | "${DOCKER_ARGV[@]}" build -q -t "$tag" ${BUILD_ARGS[@]+"${BUILD_ARGS[@]}"} - >"$dir/build.log" 2>&1; then
    results+=("ERROR  $name: the throwaway image did not build: $(tail -n 3 "$dir/build.log" | tr '\n' ' ')")
    fail=$((fail + 1)); return
  fi
  python3 ./image-gate.py "$tag" "$@" >"$dir/gate.log" 2>&1
  local got_exit=$?
  local ok=1
  [ "$got_exit" = "$want_exit" ] || ok=0
  # EXPECT_CHECK may name several checks ("A1 A2"): every one of them must report.
  if [ "$want_check" != "-" ]; then
    for check in $want_check; do grep -q "\[FAIL\] $check" "$dir/gate.log" || ok=0; done
  fi
  # A gate finding must never print the planted value.
  for secret in "$OPENROUTER_FAKE" "$GITHUB_FAKE"; do grep -qF "$secret" "$dir/gate.log" && ok=0; done
  if [ "$ok" = 1 ]; then
    results+=("OK     $name: exit $got_exit, $( [ "$want_check" = "-" ] && echo 'clean' || echo "check $want_check reported")")
    pass=$((pass + 1))
  else
    results+=("WRONG  $name: wanted exit $want_exit and check $want_check, got exit $got_exit:"); results+=("$(sed 's/^/         /' "$dir/gate.log")")
    fail=$((fail + 1))
  fi
}

new_case() { mkdir -p "$WORK/$1/ctx"; BUILD_ARGS=(); }

# 0. Clean, with strings that must NOT trip it: a network range, the metadata host, a URL with credentials to a real
# (non-local) host in documentation, an empty secret-named variable, a path-valued one, a checksum.
new_case clean
printf 'ranges 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16\nhost metadata.google.internal\nsmtp://user:pass@mail.example.org:587\n' >"$WORK/clean/ctx/notes.txt"
build_and_gate clean 0 - <<EOF
COPY ctx/notes.txt /opt/org/notes.txt
ENV OPENROUTER_API_KEY= GH_TOKEN_FILE=/run/secrets/gh CODEAF_SHA256=$(rand_hex 32)
LABEL org.opencontainers.image.revision=1111111111111111111111111111111111111111
EOF

# A file from the build context carrying a key, each kind. Trivy (A1) must see the three kinds it has rules for (our OpenRouter
# rule, its built-in GitHub-token and private-key rules), so a scanner that silently scans nothing cannot pass this test.
new_case file-openrouter-key; printf '{"openrouter":"%s"}\n' "$OPENROUTER_FAKE" >"$WORK/file-openrouter-key/ctx/config.json"
build_and_gate file-openrouter-key 1 "A1 A2" <<'EOF'
COPY ctx/config.json /opt/app/config.json
EOF
new_case file-github-token; printf 'token: %s\n' "$GITHUB_FAKE" >"$WORK/file-github-token/ctx/hosts.yml"
build_and_gate file-github-token 1 "A1 A2" <<'EOF'
COPY ctx/hosts.yml /home/app/.config/gh/hosts.yml
EOF
new_case file-private-key; printf '%s\n' "$PEM_FAKE" >"$WORK/file-private-key/ctx/deploy.pem"
build_and_gate file-private-key 1 "A1 A2" <<'EOF'
COPY ctx/deploy.pem /opt/app/deploy.pem
EOF
new_case file-registry-auth; printf '%s\n' "$AUTH_FAKE" >"$WORK/file-registry-auth/ctx/config.json"
build_and_gate file-registry-auth 1 A2 <<'EOF'
COPY ctx/config.json /root/.docker/config.json
EOF
# The same key, added and then deleted by a later layer: the merged filesystem is clean, the pushed layer is not.
new_case deleted-layer-key; printf '%s\n' "$OPENROUTER_FAKE" >"$WORK/deleted-layer-key/ctx/key.txt"
build_and_gate deleted-layer-key 1 A2 <<'EOF'
COPY ctx/key.txt /opt/key.txt
RUN rm /opt/key.txt
EOF

# The configuration and the history (B1, B2).
new_case env-secret-name
build_and_gate env-secret-name 1 B1 <<EOF
ENV OPENROUTER_API_KEY=$(rand_alnum 40)
EOF
new_case env-key-shape
build_and_gate env-key-shape 1 B1 <<EOF
ENV SOMETHING_HARMLESS=$GITHUB_FAKE
EOF
new_case label-key-shape
build_and_gate label-key-shape 1 B1 <<EOF
LABEL note=$OPENROUTER_FAKE
EOF
new_case build-arg-secret
BUILD_ARGS=(--build-arg "API_TOKEN=$(rand_alnum 40)")
build_and_gate build-arg-secret 1 B2 <<'EOF'
ARG API_TOKEN
RUN echo built
EOF

# Files a package manager installed and that are still what it installed (the false positives of a real base image: a library
# that embeds test private keys, a PAM sample file with example addresses) are not scanned; a file that differs from what the
# package recorded is. The exemption is by recorded hash, not by place: these live under /opt and /etc, not under a system tree.
new_case owned-dpkg-unmodified
{ printf '\177ELF\000\000'; printf '%s\n' "$PEM_FAKE"; } >"$WORK/owned-dpkg-unmodified/ctx/lib.so"
printf '%s  opt/vendor/lib.so\n' "$(md5_of "$WORK/owned-dpkg-unmodified/ctx/lib.so")" >"$WORK/owned-dpkg-unmodified/ctx/vendor.md5sums"
build_and_gate owned-dpkg-unmodified 0 - <<'EOF'
COPY ctx/lib.so /opt/vendor/lib.so
COPY ctx/vendor.md5sums /var/lib/dpkg/info/vendor.md5sums
EOF
new_case owned-dpkg-conffile; printf '%s\n' '- : ALL : 192.168.1.1' >"$WORK/owned-dpkg-conffile/ctx/access.conf"
printf 'Package: vendor\nStatus: install ok installed\nConffiles:\n /etc/vendor/access.conf %s\nDescription: x\n' "$(md5_of "$WORK/owned-dpkg-conffile/ctx/access.conf")" >"$WORK/owned-dpkg-conffile/ctx/status"
build_and_gate owned-dpkg-conffile 0 - <<'EOF'
COPY ctx/access.conf /etc/vendor/access.conf
COPY ctx/status /var/lib/dpkg/status
EOF
new_case owned-dpkg-modified; printf 'original content\n' >"$WORK/owned-dpkg-modified/ctx/original"
printf '%s  opt/vendor/config.json\n' "$(md5_of "$WORK/owned-dpkg-modified/ctx/original")" >"$WORK/owned-dpkg-modified/ctx/vendor.md5sums"
printf '{"openrouter":"%s"}\n' "$OPENROUTER_FAKE" >"$WORK/owned-dpkg-modified/ctx/config.json"
build_and_gate owned-dpkg-modified 1 A2 <<'EOF'
COPY ctx/config.json /opt/vendor/config.json
COPY ctx/vendor.md5sums /var/lib/dpkg/info/vendor.md5sums
EOF
new_case owned-pip-unmodified; printf '%s\n' "$PEM_FAKE" >"$WORK/owned-pip-unmodified/ctx/data.pem"
printf 'pkg/data.pem,sha256=%s,%s\n' "$(sha256_b64_of "$WORK/owned-pip-unmodified/ctx/data.pem")" "$(wc -c <"$WORK/owned-pip-unmodified/ctx/data.pem")" >"$WORK/owned-pip-unmodified/ctx/RECORD"
build_and_gate owned-pip-unmodified 0 - <<'EOF'
COPY ctx/data.pem /usr/local/lib/python3.12/site-packages/pkg/data.pem
COPY ctx/RECORD /usr/local/lib/python3.12/site-packages/pkg-1.0.dist-info/RECORD
EOF

# Private hostnames and addresses in files we put there (C1).
new_case host-railway; printf 'upstream=%s\nprivate=%s\n' "$RAILWAY_FAKE" "$RAILWAY_PRIVATE_FAKE" >"$WORK/host-railway/ctx/endpoints"
build_and_gate host-railway 1 C1 <<'EOF'
COPY ctx/endpoints /opt/app/endpoints
EOF
new_case host-private-address; printf 'http://10.8.3.12:8080/health\n' >"$WORK/host-private-address/ctx/endpoint"
build_and_gate host-private-address 1 C1 <<'EOF'
COPY ctx/endpoint /opt/app/endpoint
EOF
new_case host-internal-name; printf 'db.corp.internal\n' >"$WORK/host-internal-name/ctx/endpoint"
build_and_gate host-internal-name 1 C1 <<'EOF'
COPY ctx/endpoint /opt/app/endpoint
EOF
new_case host-localhost-credentials; printf 'http://admin:%s@localhost:8080/\n' "$(rand_alnum 12)" >"$WORK/host-localhost-credentials/ctx/endpoint"
build_and_gate host-localhost-credentials 1 C1 <<'EOF'
COPY ctx/endpoint /opt/app/endpoint
EOF

# The commit label (C2).
new_case wrong-commit-label
build_and_gate wrong-commit-label 1 C2 --expect-commit 2222222222222222222222222222222222222222 <<'EOF'
LABEL org.opencontainers.image.revision=1111111111111111111111111111111111111111
EOF

echo "image gate self-test: $pass passed, $fail failed"
printf '%s\n' "${results[@]}"
[ "$fail" = 0 ]
