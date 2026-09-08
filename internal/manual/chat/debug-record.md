# The debug record

## How do I see what happened — turning the record on with /debug, --debug or AFORGE_DEBUG

When a turn goes wrong and the answer is not in what you can see, you can ask aforge to
keep the **debug record** of a run: everything that run did, in a folder of its own. There
is one switch and three ways to say it, and they all mean the same thing.

```
/debug                            in a conversation, from here to the end of it
aforge chat --debug               and the same flag on `aforge do` and `aforge exec`
AFORGE_DEBUG=1 aforge             for one shell, one run
```

`AFORGE_CALL_LOG_BODIES=1` — the older word, if it is the one in your shell history —
means the same thing now.

**It does not change what the run does.** Nothing is asked differently, nothing is slower,
no model is told anything new. With it off, a run costs one check and writes nothing at
all; that is why it is safe to leave the flag out and reach for it only on the day you
need it.

**`/debug` cannot be turned off again.** You type it because something has already gone
wrong, and a switch you could turn back off would only ever leave you half a record — the
half after the thing you were trying to catch. It lasts for the rest of the conversation,
and the next one starts with it off.

`/debug` answers with the folder either way: the first time it says
`recording this conversation · it goes to …`, and a second time
`the record is already on · it goes to …`.

## Does /debug turn the record on for my other conversations — what each door covers

The three doors mean the same record and not the same reach. **With the pin or the flag,
every conversation this process holds is recorded, each into its own folder; `/debug`
records only the conversation you typed it in.** The pin and the flag were handed to that
aforge on purpose, before anything opened; `/debug` was typed inside one conversation, and
one aforge can be holding several.

So if you have two conversations open and type `/debug` in one, the other one keeps
writing nothing — its prompts, its files and its replies do not land in a folder its
person never asked for. Each conversation that is being recorded has a folder of its own,
named by its own run.

Where the pin or the flag already turned everything on, `/debug` says so rather than
pretending it did something: `the record is already on for every conversation this aforge
holds · this one goes to …`, naming your own conversation's folder. And in a conversation
that belongs to no run at all, it says `this conversation has no run to record.` instead
of switching on a record that would go nowhere.

## Where is the debug record — the folder for a run, and what is in it

Every run gets a folder of its own, named after that run:

```
~/.aforge/logs/trace/<run>/
```

`<run>` is sixteen characters minted when the run starts — when you open a conversation,
and when `aforge do` or `aforge exec` begins. It is the id every record in that folder
carries, so records from two runs can never be read as one.

Under `AFORGE_HOME` the folder moves with everything else aforge keeps.

**The folder holds the record of that run, and the first thing in it is `run.json`** —
written the moment the run starts, and saying which door opened it (`chat`, `resume`,
`do`, `exec`), which model was asked for, which build of aforge this was, which folder the
run was pointed at, and when it began. It is the file that tells you which run a folder
you found afterwards actually was.

Beside it the run's own records accumulate: `events.jsonl`, one line per thing that
happened, and a `calls/` folder with one file per model call named by that call's id.
Every record carries the run it belongs to, and — where the work it came from has a name —
the piece of work, so a long run reads as the plan it was rather than a pile of calls in
time order.

**The pieces that write those are being added one at a time**, and this build ships the
switch, the run, its header, the folder and its size law; the call bodies, the tool calls
and the choices a run made arrive with the changes that record them.

Each of the three doors also prints one line to the error output when it finishes, and
only when there is something to go and look at:

```
debug record: ~/.aforge/logs/trace/52dfbdde3f1a7c04
```

**With the switch off, nothing is created at all** — no folder, no line, nothing to clean
up afterwards.

## Why did that call fail — what answers it today, and what the record will

The record is being built to answer exactly that from the files alone; today it holds the
run's header and the pieces that fill in the rest are landing one at a time. Until they
have, the answer is in the **model-call log**, which is always on and needs nothing
switched: `aforge logs` prints the last calls with the status each came back with,
the endpoint's own first sentence on a failure, how long it took and what it cost. The
models-and-cost page has how to read one of those lines.

What the log holds is the **shape** of a call — how many messages, how many tools, which
ceiling, which lane — and not what you wrote. The one exception is the old
`AFORGE_CALL_LOG_BODIES` pin, which still adds the whole request and reply to each line of
`calls.jsonl` as well as turning the debug record on. With that pin on, the live file is
allowed 256 MB (32 MB without it) so a long session keeps the bodies; the bodies are still
moving out of the log and into the record, so that the file you grep stays small.

## Is my key in the debug record — what it never holds

**No key, ever.** Nothing that looks like a credential is written into the record: an
authorization or `api-key` field, anything after `Bearer`, and anything shaped like an
`sk-…` token are each replaced by the word `[redacted]` before anything reaches the disk.

**And not only the shapes.** Every credential this machine is actually configured with —
the model key, the search keys, an application secret you pasted for your own Google
registration — is handed to the record by name as the run opens, and is replaced wherever
it appears, whatever it looks like. That matters because a key with no recognisable shape
at all (a Google `AIza…`, a Groq `gsk_…`, the plain token a self-hosted endpoint was
given) can come back inside a provider's own error message. You can grep the folder for
`authorization` or for your own key and find nothing.

**Everything else in it is yours.** The record is built to hold what you wrote, what your
files say and what the model answered — that is the point of it — so the folder is
readable by you and nobody else on the machine (the folder is `0700`, its files `0600`),
it never leaves the state root, and nothing is sent anywhere. Treat a folder you attach to
a bug report the way you would treat the conversation itself.

## How much does the debug record keep — the size law, and what happens when it fills

Two ceilings, and both are about never losing the run you are looking at.

- **256 MB per run.** When a run's folder reaches it, the record writes one last line
  saying it was capped and stops. It keeps everything it already had rather than making
  room by dropping the start of the run — which is where the choice that went wrong
  usually is.
- **20 runs kept.** When a new run opens its folder and there are more than twenty, the
  oldest whole folders are removed until twenty remain. A run is kept complete or not at
  all; nothing is ever deleted from inside a run that is still going. **A run that is
  still going is never removed either**, however low you set the count — retention is a
  rule about runs that are over, so a second conversation opening cannot delete the record
  of the one you are debugging.

Both move for one shell when you need them to:

```
AFORGE_TRACE_MAX_MB=1024 aforge chat --debug     a bigger ceiling for one run
AFORGE_TRACE_KEEP=3 aforge chat --debug          keep fewer folders around
```

If the record cannot be written at all — a full disk, a folder that is not writable —
aforge says so once on the error output, naming the path, and the run carries on exactly
as it would have with the record off. **A record that cannot be written is never a failed
run.**
