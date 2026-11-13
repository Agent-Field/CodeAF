# Call census

Summarize a model-call log as Markdown:

```sh
make census LOG=/path/to/calls.jsonl OUT=/tmp/census.md
make census TOP=40 DAYS=7 MIN=10 CHAINS=20
```

Without `LOG`, the command reads the configured call log. Without `OUT`, it
prints to standard output. It never modifies the log it reads.
