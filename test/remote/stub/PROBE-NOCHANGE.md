# Replaying the no-change fold

From the repository root, build the real binary and the local model stub, then
start the stub with its log kept beside it:

```sh
export GOFLAGS=-buildvcs=false
make build
go build -o /tmp/modelstub ./test/remote/stub
/tmp/modelstub -addr 127.0.0.1:8080 2>/tmp/modelstub-nochange.log &
stub_pid=$!
```

Open codeaf in tmux with a fresh, short state path and the one model served by
the stub:

```sh
probe_home=/tmp/caf-nc-$$
tmux new-session -s codeaf-nochange \
  "cd '$PWD' && env CODEAF_BASE_URL=http://127.0.0.1:8080/api/v1 OPENROUTER_API_KEY=stub CODEAF_HOME=$probe_home bin/codeaf chat --model stub/scripted --one-model"
```

Type `PROBE-NOCHANGE ./go.mod` and press enter. When the turn settles, the stub
log has exactly one request whose last user message starts with `[carry on]`,
one `PROBE-NOCHANGE no change` branch, and one reader branch:

```sh
grep -c 'ask="\[carry on\]' /tmp/modelstub-nochange.log
grep -c 'PROBE-NOCHANGE no change' /tmp/modelstub-nochange.log
grep -c 'PROBE-NOCHANGE reader ' /tmp/modelstub-nochange.log
```

Each command prints `1`. On screen, the folded turn still ends with the
`seat 30` row of the table. `[no change]` appears nowhere on screen. Stop the
stub after leaving codeaf with `kill "$stub_pid"`.
