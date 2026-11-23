#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
deploy_script="$repo_root/scripts/relay-deploy.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/relay-deploy-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

fake_bin="$tmp/bin"
mkdir -p "$fake_bin"

# Deterministic 64-hex fingerprint stub (first 16 hex = 0123456789abcdef).
cat > "$tmp/fake-fp.sh" <<'EOF'
#!/usr/bin/env bash
echo "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
EOF
chmod +x "$tmp/fake-fp.sh"

# Fake curl: records calls, reads stdin config for Bearer token without network I/O.
cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
stdin_cfg="$(cat)"
printf 'CURL %s\n' "$*" >> "$MOCK_LOG"
case "$stdin_cfg" in
	*"Authorization: Bearer "*) ;;
	*) echo "missing Authorization header in curl --config stdin" >&2; exit 9 ;;
esac
out=""
endpoint=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) out="$2"; shift 2 ;;
		-w|--config) shift 2 ;;
		-*) shift ;;
		*) endpoint="$1"; shift ;;
	esac
done
case "$endpoint" in
	*/deployments)
		cp "$MOCK_DEPLOYMENTS_BODY" "$out"
		printf '%s' "${MOCK_DEPLOYMENTS_CODE:-200}"
		;;
	*/versions/*)
		cp "$MOCK_VERSION_BODY" "$out"
		printf '%s' "${MOCK_VERSION_CODE:-200}"
		;;
	*)
		echo "unexpected curl endpoint: $endpoint" >&2
		exit 9
		;;
esac
EOF
chmod +x "$fake_bin/curl"

# Fake npx: intercepts wrangler commands and logs every invocation.
cat > "$fake_bin/npx" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'NPX %s\n' "$*" >> "$MOCK_LOG"
if [ "${MOCK_ECHO_SECRET:-0}" = "1" ]; then
	echo "leaked token=${CLOUDFLARE_API_TOKEN} acct=${CLOUDFLARE_ACCOUNT_ID}"
fi
case "$*" in
	"wrangler r2 bucket create "*)
		if [ "${MOCK_R2_EXISTS:-0}" = "1" ]; then
			echo "✘ [ERROR] A request to the Cloudflare API failed. [code: 10004] The bucket you tried to create already exists, and you own it." >&2
			exit 1
		fi
		echo "Created bucket."
		;;
	"wrangler deploy "*)
		echo "Deployed."
		;;
	"wrangler rollback "*)
		echo "Rolled back."
		;;
	*)
		echo "unexpected npx command: $*" >&2
		exit 9
		;;
esac
EOF
chmod +x "$fake_bin/npx"

# Fake go: handles relay-url, hosted_test.go conformance, and codeaf-release service --url.
cat > "$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'GO %s (HOSTED_URL=%s TELEMETRY=%s DNT=%s)\n' \
	"$*" "${CODEAF_RELAY_HOSTED_URL:-}" "${CODEAF_TELEMETRY:-}" "${DO_NOT_TRACK:-}" >> "$MOCK_LOG"
case "$*" in
	"run ./cmd/codeaf-release relay-url --channel stable"|"run ./cmd/codeaf-release relay-url --channel rc")
		echo "https://codeaf.agentfield.ai/fabric"
		;;
	"run ./cmd/codeaf-release relay-url --channel staging")
		echo "https://caf-fabric-staging.instrument-santosh.workers.dev"
		;;
	"run ./cmd/codeaf-release relay-url --channel dev")
		echo "https://caf-fabric-dev.instrument-santosh.workers.dev"
		;;
	"test -count=1 -run ^TestAHostedRelay ./internal/relay")
		if [ "${MOCK_FAIL_HOSTED_TEST:-0}" = "1" ]; then
			echo "FAIL hosted_test" >&2
			exit 1
		fi
		echo "ok internal/relay"
		;;
	"run ./cmd/codeaf-release service --url "*)
		if [ "${MOCK_FAIL_SERVICE_GATE:-0}" = "1" ]; then
			echo "missing capability" >&2
			exit 1
		fi
		echo "service ok"
		;;
	*)
		echo "unexpected go command: $*" >&2
		exit 9
		;;
esac
EOF
chmod +x "$fake_bin/go"

run_deploy() {
	PATH="$fake_bin:$PATH" \
	MOCK_LOG="$tmp/mock.log" \
	MOCK_DEPLOYMENTS_BODY="$tmp/deployments.json" \
	MOCK_VERSION_BODY="$tmp/version.json" \
	RELAY_FINGERPRINT_SCRIPT="$tmp/fake-fp.sh" \
	RELAY_WRANGLER_CONFIG_OUT="$tmp/wrangler.ci.toml" \
	bash "$deploy_script" "$@"
}

# 1. Production is OFF unless RELAY_DEPLOY_PRODUCTION is literally "true".
for ch in stable rc; do
	for val in "" "false" "TRUE" "1" "yes"; do
		: > "$tmp/mock.log"
		out="$(CHANNEL="$ch" RELAY_DEPLOY_PRODUCTION="$val" run_deploy)"
		[ "$(printf '%s\n' "$out" | wc -l | tr -d ' ')" = "1" ] || fail "expected 1 log line when production off ($ch/$val), got: $out"
		[[ "$out" == *"RELAY_DEPLOY_PRODUCTION=true"* ]] || fail "missing enablement hint: $out"
		[ ! -s "$tmp/mock.log" ] || fail "production-off run contacted tools: $(cat "$tmp/mock.log")"
	done
done

# 2. Matching live tag skips with one log line and deploys nothing.
: > "$tmp/mock.log"
cat > "$tmp/deployments.json" <<'EOF'
{"success":true,"result":{"deployments":[{"id":"dep-1","versions":[{"version_id":"ver-live-1","percentage":100}]}]}}
EOF
cat > "$tmp/version.json" <<'EOF'
{"success":true,"result":{"id":"ver-live-1","annotations":{"workers/tag":"0123456789abcdef"}}}
EOF
out="$(
	CHANNEL="dev" \
	RELEASE_SHA="deadbeefcafe" \
	CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
	CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
	run_deploy
)"
[ "$(printf '%s\n' "$out" | wc -l | tr -d ' ')" = "1" ] || fail "expected 1 skip line, got: $out"
[[ "$out" == *"already matches fingerprint tag 0123456789abcdef; skipping deploy."* ]] || fail "unexpected skip line: $out"
if grep -q '^NPX ' "$tmp/mock.log"; then
	fail "skip path invoked wrangler: $(cat "$tmp/mock.log")"
fi

# 3. Missing tag (today's live Worker) triggers deploy + both proofs; existing R2 bucket (10004) is idempotent; secrets are redacted.
: > "$tmp/mock.log"
cat > "$tmp/version.json" <<'EOF'
{"success":true,"result":{"id":"ver-untagged","annotations":{}}}
EOF
out="$(
	CHANNEL="staging" \
	RELEASE_SHA="abc123456789" \
	CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
	CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
	MOCK_R2_EXISTS="1" \
	MOCK_ECHO_SECRET="1" \
	run_deploy 2>&1
)"
[[ "$out" != *"acct_secret_xyz"* ]] || fail "CLOUDFLARE_ACCOUNT_ID leaked in output: $out"
[[ "$out" != *"tok_secret_xyz"* ]] || fail "CLOUDFLARE_API_TOKEN leaked in output: $out"
grep -Fq "NPX wrangler r2 bucket create caf-fabric-staging-frames" "$tmp/mock.log" || fail "missing R2 create for staging"
grep -Fq "NPX wrangler deploy --config $tmp/wrangler.ci.toml --tag 0123456789abcdef --message abc123456789" "$tmp/mock.log" || fail "missing wrangler deploy with tag and message"
grep -Fq "GO test -count=1 -run ^TestAHostedRelay ./internal/relay (HOSTED_URL=https://caf-fabric-staging.instrument-santosh.workers.dev TELEMETRY=off DNT=1)" "$tmp/mock.log" || fail "missing hosted_test proof"
grep -Fq "GO run ./cmd/codeaf-release service --url https://caf-fabric-staging.instrument-santosh.workers.dev" "$tmp/mock.log" || fail "missing codeaf-release service proof"

# 4. Production deploys caf-fabric when RELAY_DEPLOY_PRODUCTION=true.
: > "$tmp/mock.log"
CHANNEL="stable" \
RELAY_DEPLOY_PRODUCTION="true" \
RELEASE_SHA="mainsha999" \
CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
run_deploy >/dev/null
grep -Fq "NPX wrangler r2 bucket create caf-fabric-frames" "$tmp/mock.log" || fail "missing production R2 bucket create"
grep -Fq "GO run ./cmd/codeaf-release service --url https://codeaf.agentfield.ai/fabric" "$tmp/mock.log" || fail "missing production service proof"

# 5. Failed hosted_test rolls back to recorded live version id, warns about one-way storage upgrades, and fails the job.
: > "$tmp/mock.log"
cat > "$tmp/deployments.json" <<'EOF'
{"success":true,"result":{"deployments":[{"id":"dep-prev","versions":[{"version_id":"ver-rollback-target","percentage":100}]}]}}
EOF
cat > "$tmp/version.json" <<'EOF'
{"success":true,"result":{"id":"ver-rollback-target","annotations":{"workers/tag":"ffffffffffffffff"}}}
EOF
set +e
fail_out="$(
	CHANNEL="dev" \
	RELEASE_SHA="badcommit1" \
	CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
	CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
	MOCK_FAIL_HOSTED_TEST="1" \
	run_deploy 2>&1
)"
fail_rc=$?
set -e
[ "$fail_rc" -ne 0 ] || fail "expected failed hosted_test to fail relay-deploy.sh"
[[ "$fail_out" == *"cannot undo a one-way storage upgrade"* ]] || fail "missing one-way storage upgrade warning: $fail_out"
grep -Fq "NPX wrangler rollback ver-rollback-target --config $tmp/wrangler.ci.toml" "$tmp/mock.log" || fail "missing wrangler rollback to recorded version"

# 6. Failed codeaf-release service gate also rolls back and fails the job.
: > "$tmp/mock.log"
set +e
fail_out2="$(
	CHANNEL="dev" \
	RELEASE_SHA="badcommit2" \
	CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
	CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
	MOCK_FAIL_SERVICE_GATE="1" \
	run_deploy 2>&1
)"
fail_rc2=$?
set -e
[ "$fail_rc2" -ne 0 ] || fail "expected failed service gate to fail relay-deploy.sh"
grep -Fq "NPX wrangler rollback ver-rollback-target" "$tmp/mock.log" || fail "missing rollback after service gate failure"

# 7. First deploy of a brand-new Worker (404 / 10007): failure skips rollback cleanly and still fails the job.
: > "$tmp/mock.log"
cat > "$tmp/deployments.json" <<'EOF'
{"success":false,"errors":[{"code":10007,"message":"workers.api.error.script_not_found"}]}
EOF
set +e
first_fail_out="$(
	CHANNEL="dev" \
	RELEASE_SHA="firstbad" \
	CLOUDFLARE_ACCOUNT_ID="acct_secret_xyz" \
	CLOUDFLARE_API_TOKEN="tok_secret_xyz" \
	MOCK_DEPLOYMENTS_CODE="404" \
	MOCK_FAIL_SERVICE_GATE="1" \
	run_deploy 2>&1
)"
first_fail_rc=$?
set -e
[ "$first_fail_rc" -ne 0 ] || fail "expected failed first deploy to fail relay-deploy.sh"
if grep -q "wrangler rollback" "$tmp/mock.log"; then
	fail "unexpected rollback when no prior version existed: $(cat "$tmp/mock.log")"
fi
[[ "$first_fail_out" == *"No prior live version existed"* ]] || fail "missing no-prior-version log: $first_fail_out"

echo "PASS: relay-deploy_test"
