# The delegate protocol

*Version 1, 2026-09-22. What a program must do to be a codeaf delegate. The
design behind it is `docs/design/delegate/DESIGN.md`. Conforming today:
`swe-pro` (`zeropoint95/improvements @ 5793499`).*

A delegate is an outside program codeaf hands one task to. codeaf starts it,
reads its stdout, stops it when a limit is hit, and takes its result. This page
is the whole interface. If a program does what is written here, a manifest
and a manual page beside it are all codeaf needs.

## 1. Launch

codeaf runs the program once per task, as a child process, with:

| handed over | how |
| --- | --- |
| the brief, as text | an argv slot, `{{brief}}` |
| a directory to work in | an argv slot, `{{workspace}}` (absolute) |
| a dollar ceiling | an argv slot, `{{cost_usd}}` |
| a wall-clock ceiling in hours | an argv slot, `{{hours}}` |
| API keys | environment, resolved by codeaf from the person's profile |

The program must:

1. Take all of these on the command line or in the environment. There is no
   stdin. Nothing is written to it and nothing is read from it.
2. Treat the ceilings as its own limits and stop itself when it reaches one.
   codeaf also enforces them from outside, but a program that cuts itself
   first ends cleanly and keeps its result.
3. Start with no interactive step. It cannot ask anything.

## 2. Stream

stdout carries **one JSON object per line and nothing else**. stderr is the
program's own; codeaf keeps it for a person to read and never parses it.

Four record types are read. **Any other line is ignored**, so a program may
put whatever else it likes on stdout as long as every line is a JSON object.

| record | fields | meaning |
| --- | --- | --- |
| `{"type":"stage","stage":S,"status":T}` | `stage`, `status`: short strings | the live step shown on the rail, `S · T`. Emit on every phase change |
| `{"type":"spend","cost_usd":C}` | `cost_usd`: number, **cumulative for the whole run, never decreasing** | what the run has cost so far. Emit after every model call. Emit even at zero |
| `{"type":"step","command":X,"observation":Y}` | `command`: one line, ≤ 200 bytes; `observation`: optional, ≤ 2048 bytes, valid UTF-8 | one row on the task page. Emit once per finished tool call or action. Optional: a program with none is drawn by its stages |
| `{"type":"terminal","status":U,"message":M,"data":{…}}` | see §3 | the result. **Exactly one, and the last record** |

Every record may carry `"ts"`: unix milliseconds. Extra fields are ignored.

## 3. Terminal

The terminal record is the result. codeaf reads it and nothing else for the
verdict, and it does not read the exit code for the verdict.

```json
{"type":"terminal","status":"pass","message":"submitted and verified",
 "data":{"cost_usd":0.42,"reason":"…","claim":"…","observed":"…","deliverable":"…"}}
```

| field | required | values |
| --- | --- | --- |
| `status` | yes | `pass`, `fail`, `budget-exhausted`, `crashed` |
| `message` | yes | one sentence saying why |
| `data.cost_usd` | yes | the final total. Must be ≥ the last `spend` |
| `data.reason` | no | a longer reason |
| `data.claim` | no | what the program's model said it did |
| `data.observed` | no | what the program itself verified. Kept separate from `claim`, never merged |
| `data.deliverable` | for `lands: text` | the answer text |

What codeaf makes of `status`:

| `status` | rail word | meaning |
| --- | --- | --- |
| `pass` | done | the work stands |
| `fail` | incomplete | it ran and the work does not stand |
| `budget-exhausted` | stopped, naming the limit | a ceiling was reached before it passed |
| `crashed` | incomplete | the program itself failed |

A process that exits with no terminal record is read as `incomplete`, with
the last `stage` seen as the reason. **Emit the terminal on every path**,
including error and signal.

## 4. Stop

codeaf sends **SIGTERM** to the process group when a limit is hit or a person
presses stop, then waits a grace period, then SIGKILL.

The program must, on SIGTERM:

1. Stop starting new work.
2. Write its terminal record, with the true `status` and `cost_usd`.
3. Exit.

A terminal record inside the grace is kept. After SIGKILL nothing is read.

## 5. Result on disk

The manifest says which of two things the program produces.

| `lands` | the program must | codeaf then |
| --- | --- | --- |
| `tree` | leave its changes in `{{workspace}}`, as commits on the current branch or as a dirty tree, and **nothing that is not its work** (its own state files git-excluded or outside the tree) | squashes everything past the cut point into one commit and merges it home |
| `text` | change nothing in `{{workspace}}`; put the answer in `data.deliverable` | folds the text into the conversation |

## 6. What the program may not do

- Ask a question and wait for an answer. There is nobody there.
- Read stdin.
- Write anything to stdout that is not a JSON object on its own line.
- Exit before writing the terminal record, except when killed.
- For `lands: tree`, touch files outside `{{workspace}}`.

## 7. The manifest and the manual page

Two files in `~/.codeaf/delegates/`:

```jsonc
// swe-pro.json
{
  "name": "swe-pro",                 // also the command: /swe-pro <brief>
  "description": "an autonomous coding agent for one large, well-specified change",
  "bin": "swe-pro",                  // on PATH, or a path
  "argv": ["run", "--dir", "{{workspace}}",
           "--max-cost", "{{cost_usd}}", "--max-hours", "{{hours}}",
           "--", "{{brief}}"],
  "env": { "OPENROUTER_API_KEY": "{{key:openrouter}}" },
  "lands": "tree",
  "limits": { "cost": true, "elapsed": true, "steps": false, "questions": false }
}
```

- `name` is one lowercase word and becomes the command. It may not collide
  with a built-in command or alias.
- `{{key:<provider>}}` is filled from the person's profile.
- A `bin` not found means the delegate is not offered. Nothing fails.

`swe-pro.md` beside it is the delegate's manual page: what it does, how to
ask it, what it cannot do, what a run costs, where the work lands. It follows
the rules of `internal/manual/chat/` pages and **must mention `/<name>`**. A
page that does not refuses the manifest.

## 8. Conformance: swe-pro

| requirement | swe-pro |
| --- | --- |
| launch from argv | `swe-pro run --dir D --max-cost X --max-hours H -- "goal"` |
| no stdin, no questions | nothing reads stdin; `question` is auto-rejected |
| stdout is JSON lines only | yes, EVENTS-CONTRACT.md |
| `stage` | yes, thirteen stages |
| `spend`, cumulative, top-level | yes, since `5793499` |
| `step` per tool call | yes, since `5793499` |
| exactly one `terminal`, last, on every path | yes, including crash and signal |
| `status` set | `pass`, `fail`, `budget-exhausted`, `crashed`, exactly |
| SIGTERM writes the terminal | yes |
| `lands: tree`, own state excluded | `.swe-pro/` is in `.git/info/exclude`; `refs/swe-pro/*` stay in the copy |
| runs without a control plane | yes, since `f3b9716` |

Not yet mapped on swe-pro's side: `data.claim` and `data.observed` are spelled
`submission_reason` / `submission_evidence` and `status` /
`verification_failing` in its `data`. The reader accepts swe-pro's spellings
for these two optional fields.
