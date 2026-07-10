#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tag=${AGIT_DEMO_IMAGE:-agit-two-machine:local}
network=agit-two-machine-net
minio=agit-two-machine-minio
laptop_a=agit-laptop-a
laptop_b=agit-laptop-b
volume_a=agit-laptop-a-data
volume_b=agit-laptop-b-data
follow_a=
follow_b=

now_ms() {
  perl -MTime::HiRes=time -e 'printf "%.0f\n", time * 1000'
}

cleanup() {
  test -z "$follow_a" || kill "$follow_a" >/dev/null 2>&1 || true
  test -z "$follow_b" || kill "$follow_b" >/dev/null 2>&1 || true
  docker rm -f "$laptop_a" "$laptop_b" "$minio" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  docker volume rm "$volume_a" "$volume_b" >/dev/null 2>&1 || true
}

remote_usage() {
  docker run --rm --network "$network" --entrypoint /bin/sh minio/mc:latest -c '
    mc alias set local http://agit-two-machine-minio:9000 agit-test agit-test-secret >/dev/null
    mc du --json --recursive local/sync | tail -1
  '
}

json_number() {
  printf '%s\n' "$1" | sed -n "s/.*\"$2\":\([0-9][0-9]*\).*/\1/p"
}

tree_digest() {
  docker exec "$1" sh -c '
    cd /machine/project
    find . -type f ! -path "./.agit/*" -print0 |
      sort -z | xargs -0 sha256sum | sha256sum | cut -d " " -f 1
  '
}

wait_for_file() {
  machine=$1
  file=$2
  expected=$3
  for _ in $(seq 1 300); do
    if docker exec "$machine" sh -c "test -f '$file' && grep -qx '$expected' '$file'"; then
      return 0
    fi
    sleep 0.1
  done
  printf 'timed out waiting for %s on %s\n' "$file" "$machine" >&2
  return 1
}

trap cleanup EXIT
cleanup

docker build -q -f "$root/demo/Dockerfile.two-machine" -t "$tag" "$root" >/dev/null
docker network create "$network" >/dev/null
docker volume create "$volume_a" >/dev/null
docker volume create "$volume_b" >/dev/null
docker run -d --name "$minio" --network "$network" \
  -e MINIO_ROOT_USER=agit-test -e MINIO_ROOT_PASSWORD=agit-test-secret \
  minio/minio:latest server /data >/dev/null
docker run --rm --network "$network" --entrypoint /bin/sh minio/mc:latest -c '
  until mc alias set local http://agit-two-machine-minio:9000 agit-test agit-test-secret >/dev/null 2>&1; do sleep 1; done
  mc mb local/sync >/dev/null
'

for machine in "$laptop_a:$volume_a" "$laptop_b:$volume_b"; do
  name=${machine%%:*}
  volume=${machine#*:}
  docker run -d --name "$name" --network "$network" -v "$volume:/machine" \
    -e AGIT_DATA_DIR=/machine/agit-data \
    -e AWS_ACCESS_KEY_ID=agit-test \
    -e AWS_SECRET_ACCESS_KEY=agit-test-secret \
    -e AGIT_S3_ENDPOINT=http://agit-two-machine-minio:9000 \
    -e AGIT_S3_ALLOW_HTTP=1 -e AGIT_S3_PATH_STYLE=1 \
    "$tag" >/dev/null
done

# Laptop A begins with a realistic mixed working tree: committed source, a large
# fixture, ignored credentials, and uncommitted agent notes.
docker exec "$laptop_a" sh -c '
  mkdir -p /machine/project/src /machine/project/fixtures /machine/project/reports
  cd /machine/project
  git init -q
  git config user.email agent-a@example.test
  git config user.name "Agent A"
  for number in $(seq -w 1 80); do
    printf "export function module%s(value: number) { return value + %s; }\n" "$number" "${number#0}" > "src/module${number}.ts"
  done
  dd if=/dev/urandom of=fixtures/research-corpus.bin bs=1M count=4 status=none
  printf "node_modules/\n.env\n" > .gitignore
  printf "MODEL_TOKEN=encrypted-working-state\n" > .env
  printf "Investigate corpus outliers and write the handoff report.\n" > agent-notes.txt
  git add src .gitignore fixtures/research-corpus.bin
  git commit -qm "seed analysis project"
  printf "Uncommitted hypothesis: cohorts 7 and 12 need review.\n" >> agent-notes.txt
  agit watch --no-daemon >/dev/null
'

source_bytes=$(docker exec "$laptop_a" sh -c 'du -sb /machine/project | cut -f 1')
source_files=$(docker exec "$laptop_a" sh -c 'find /machine/project -type f ! -path "*/.agit/*" | wc -l | tr -d " "')
add_output=$(docker exec "$laptop_a" sh -c 'cd /machine/project && agit remote add s3://sync/transport --name project')
key=$(printf '%s\n' "$add_output" | sed -n 's/^Recovery key //p')
test ${#key} -eq 64

push_started=$(now_ms)
initial_push=$(docker exec "$laptop_a" sh -c 'cd /machine/project && agit sync --push')
push_latency_ms=$(( $(now_ms) - push_started ))
initial_usage=$(remote_usage)
initial_remote_bytes=$(json_number "$initial_usage" size)
initial_remote_objects=$(json_number "$initial_usage" objects)
test -n "$initial_remote_bytes"

clone_started=$(now_ms)
clone_output=$(docker exec -e AGIT_RECOVERY_KEY="$key" "$laptop_b" sh -c '
  cd /machine
  agit clone s3://sync/transport/project --no-watch
')
clone_latency_ms=$(( $(now_ms) - clone_started ))

test "$(tree_digest "$laptop_a")" = "$(tree_digest "$laptop_b")"
docker exec "$laptop_b" grep -qx 'MODEL_TOKEN=encrypted-working-state' /machine/project/.env
docker exec "$laptop_b" grep -qx 'Uncommitted hypothesis: cohorts 7 and 12 need review.' /machine/project/agent-notes.txt
docker exec "$laptop_b" sh -c 'cd /machine/project && test "$(git log -1 --format=%s)" = "seed analysis project"'

docker exec "$laptop_a" sh -c 'cd /machine/project && agit sync --follow --poll-seconds 1' >/tmp/agit-follow-a.log 2>&1 &
follow_a=$!
docker exec "$laptop_b" sh -c 'cd /machine/project && agit sync --follow --poll-seconds 1' >/tmp/agit-follow-b.log 2>&1 &
follow_b=$!

# Agent A produces several related artifacts in one turn. The completion marker
# is written last so its arrival means B can validate the complete turn.
a_started=$(now_ms)
docker exec "$laptop_a" sh -c '
  cd /machine/project
  mkdir -p reports artifacts
  {
    printf "# Cohort analysis\n\n"
    printf "The corpus scan found two review candidates.\n\n"
    printf "| cohort | samples | confidence |\n| --- | ---: | ---: |\n"
    for cohort in $(seq 1 160); do
      printf "| cohort-%03d | %d | 0.%03d |\n" "$cohort" "$((800 + cohort))" "$((700 + cohort % 250))"
    done
  } > reports/analysis.md
  printf "{\"candidate_cohorts\":[7,12],\"rows\":160}\n" > artifacts/summary.json
  printf "Agent A complete\n" > artifacts/agent-a.done
  agit snap -m "agent A: cohort analysis" >/dev/null
'
wait_for_file "$laptop_b" /machine/project/artifacts/agent-a.done 'Agent A complete'
a_latency_ms=$(( $(now_ms) - a_started ))
docker exec "$laptop_b" grep -q '^| cohort-160 |' /machine/project/reports/analysis.md
docker exec "$laptop_b" grep -Fqx '{"candidate_cohorts":[7,12],"rows":160}' /machine/project/artifacts/summary.json
test "$(tree_digest "$laptop_a")" = "$(tree_digest "$laptop_b")"
a_usage=$(remote_usage)
a_remote_bytes=$(json_number "$a_usage" size)
a_delta_bytes=$(( a_remote_bytes - initial_remote_bytes ))
a_payload_bytes=$(docker exec "$laptop_a" sh -c 'cd /machine/project; wc -c < reports/analysis.md; wc -c < artifacts/summary.json' | awk '{ total += $1 } END { print total }')

# Agent B consumes A's report, adds a review and final report, then hands the
# complete state back. This also exercises the explicit writer handoff.
b_started=$(now_ms)
docker exec "$laptop_b" sh -c '
  cd /machine/project
  test "$(grep -c "^| cohort-" reports/analysis.md)" -eq 160
  mkdir -p reviews
  cat > reviews/methodology.md <<"EOF"
# Methodology review

The analysis is internally consistent. Cohorts 7 and 12 should remain flagged,
and the generated table contains all 160 expected cohorts.
EOF
  {
    printf "# Final research report\n\n"
    printf "Reviewed by Agent B after reading the complete Agent A artifact set.\n\n"
    printf "Decision: investigate cohorts 7 and 12.\n"
  } > reports/final.md
  printf "Agent B complete\n" > artifacts/agent-b.done
  agit snap -m "agent B: methodology review" >/dev/null
'
wait_for_file "$laptop_a" /machine/project/artifacts/agent-b.done 'Agent B complete'
b_latency_ms=$(( $(now_ms) - b_started ))
docker exec "$laptop_a" grep -qx 'Decision: investigate cohorts 7 and 12.' /machine/project/reports/final.md
docker exec "$laptop_a" grep -q 'all 160 expected cohorts' /machine/project/reviews/methodology.md
final_digest_a=$(tree_digest "$laptop_a")
final_digest_b=$(tree_digest "$laptop_b")
test "$final_digest_a" = "$final_digest_b"
b_usage=$(remote_usage)
b_remote_bytes=$(json_number "$b_usage" size)
b_delta_bytes=$(( b_remote_bytes - a_remote_bytes ))
b_payload_bytes=$(docker exec "$laptop_b" sh -c 'cd /machine/project; wc -c < reviews/methodology.md; wc -c < reports/final.md' | awk '{ total += $1 } END { print total }')

container_stats=$(docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}} network={{.NetIO}}' "$laptop_a" "$laptop_b")

kill "$follow_a" "$follow_b" >/dev/null 2>&1 || true
wait "$follow_a" "$follow_b" >/dev/null 2>&1 || true
follow_a=
follow_b=

printf 'PASS: two isolated machines exchanged and consumed one complete working tree\n'
printf '  source: %s files, %s bytes (committed, dirty, ignored, and Git state)\n' "$source_files" "$source_bytes"
printf '  initial publish: %s ms, %s encrypted bytes, %s remote objects\n' "$push_latency_ms" "$initial_remote_bytes" "${initial_remote_objects:-unknown}"
printf '  fresh-machine clone: %s ms; %s\n' "$clone_latency_ms" "$(printf '%s' "$clone_output" | tail -1)"
printf '  Agent A -> B: %s ms, %s payload bytes, %s new encrypted remote bytes\n' "$a_latency_ms" "$a_payload_bytes" "$a_delta_bytes"
printf '  Agent B -> A: %s ms, %s payload bytes, %s new encrypted remote bytes\n' "$b_latency_ms" "$b_payload_bytes" "$b_delta_bytes"
printf '  final working-tree digest: %s\n' "$final_digest_a"
printf '  initial push: %s\n' "$(printf '%s' "$initial_push" | tr '\n' ';')"
printf '  %s\n' "$container_stats"
