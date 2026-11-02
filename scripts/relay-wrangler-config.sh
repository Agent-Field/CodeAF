#!/usr/bin/env bash
# Generate a per-channel Wrangler configuration from relay/hosted/wrangler.toml.
#
# relay/hosted/wrangler.toml is the single source of Durable Object bindings,
# migrations, build command, observability and baseline rate-limit ids. This
# script substitutes only the channel's Worker name, account id, R2 bucket,
# base path, routing mode, and per-account rate-limit namespace offset.
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <production|stable|rc|staging|dev>" >&2
	exit 2
fi

channel="$1"
case "$channel" in
	production|stable|rc)
		mode="production"
		worker_name="caf-fabric"
		bucket_name="caf-fabric-frames"
		base_path="/fabric"
		rate_offset=0
		;;
	staging)
		mode="workers_dev"
		worker_name="caf-fabric-staging"
		bucket_name="caf-fabric-staging-frames"
		base_path=""
		rate_offset=100
		;;
	dev)
		mode="workers_dev"
		worker_name="caf-fabric-dev"
		bucket_name="caf-fabric-dev-frames"
		base_path=""
		rate_offset=200
		;;
	*)
		echo "unknown relay channel: $channel (expected production, stable, rc, staging, or dev)" >&2
		exit 2
		;;
esac

account_id="${CLOUDFLARE_ACCOUNT_ID:-}"
if [ -z "$account_id" ]; then
	echo "CLOUDFLARE_ACCOUNT_ID must be set in the environment" >&2
	exit 2
fi
case "$account_id" in
	*[!A-Za-z0-9_-]*)
		echo "CLOUDFLARE_ACCOUNT_ID contains unexpected characters" >&2
		exit 2
		;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
base_toml="${RELAY_WRANGLER_BASE:-$repo_root/relay/hosted/wrangler.toml}"

if [ ! -f "$base_toml" ]; then
	echo "missing base Wrangler config: $base_toml" >&2
	exit 1
fi

awk \
	-v mode="$mode" \
	-v worker_name="$worker_name" \
	-v bucket_name="$bucket_name" \
	-v base_path="$base_path" \
	-v rate_offset="$rate_offset" \
	-v account_id="$account_id" \
'
BEGIN {
	section = ""
	saw_name = 0
	saw_compat = 0
	saw_bucket = 0
	saw_vars = 0
	emitted_base_path = 0
	ratelimits = 0
}
/^[[:space:]]*\[/ {
	if (section == "[vars]" && !emitted_base_path) {
		printf "BASE_PATH = \"%s\"\n", base_path
		emitted_base_path = 1
	}
	section = $0
	sub(/^[[:space:]]+/, "", section)
	sub(/[[:space:]]+$/, "", section)
	if (section == "[vars]") {
		saw_vars = 1
	}
	print $0
	next
}
section == "" && /^name[[:space:]]*=/ {
	printf "name = \"%s\"\n", worker_name
	saw_name = 1
	next
}
section == "" && /^compatibility_date[[:space:]]*=/ {
	print $0
	printf "account_id = \"%s\"\n", account_id
	# No channel publishes a preview URL per version, as the live production
	# Worker does not; the one address a channel answers on is its own.
	print "preview_urls = false"
	if (mode == "production") {
		print "workers_dev = false"
		print "routes = ["
		print "  { pattern = \"codeaf.agentfield.ai/fabric*\", zone_name = \"agentfield.ai\" },"
		print "  { pattern = \"codeaf.agentfield.ai/p/*\", zone_name = \"agentfield.ai\" },"
		print "]"
	} else {
		print "workers_dev = true"
	}
	saw_compat = 1
	next
}
section == "[[r2_buckets]]" && /^bucket_name[[:space:]]*=/ {
	printf "bucket_name = \"%s\"\n", bucket_name
	saw_bucket = 1
	next
}
section == "[vars]" && /^BASE_PATH[[:space:]]*=/ {
	printf "BASE_PATH = \"%s\"\n", base_path
	emitted_base_path = 1
	next
}
section == "[[ratelimits]]" && /^namespace_id[[:space:]]*=/ {
	val = $0
	sub(/^[^"]*"/, "", val)
	sub(/".*$/, "", val)
	if (val !~ /^[0-9]+$/) {
		print "non-numeric ratelimit namespace_id: " $0 > "/dev/stderr"
		exit 1
	}
	printf "namespace_id = \"%d\"\n", (val + 0) + rate_offset
	ratelimits++
	next
}
{
	print $0
}
END {
	if (section == "[vars]" && !emitted_base_path) {
		printf "BASE_PATH = \"%s\"\n", base_path
		emitted_base_path = 1
	}
	if (!saw_name || !saw_compat || !saw_bucket || !saw_vars || ratelimits < 2) {
		printf "incomplete base wrangler.toml (name=%d compat=%d bucket=%d vars=%d ratelimits=%d)\n", \
			saw_name, saw_compat, saw_bucket, saw_vars, ratelimits > "/dev/stderr"
		exit 1
	}
}
' "$base_toml"
