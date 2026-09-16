## Working through bash

This belt carries ONE tool: `bash`. The hands other workers reach for as tools
are shell commands here, and this page is their doctrine. Everything on this
belt that is not a shell command is named at the bottom.

ONE ACTION PER RESPONSE. A response runs exactly one tool call, it names
`bash`, and its arguments are exactly one non-empty command string. A response
carrying two calls, or one call for a hand that is not here, or arguments that
do not parse, runs NOTHING: what comes back instead is a line beginning
`[not run]` saying what was wrong, and nothing has entered the world. Fix the
shape and send the command again — a good call was never the problem, so the
step before a rejection is simply the corrected call.

Parallelism lives in the shell, not in the batch:

```
cmd1 & cmd2 & wait        # two commands at once, both waited for
find . -name '*.go' | xargs -P 4 grep -l pattern
git grep -n "theSymbol"   # one search instead of three
```

The idioms, in place of the tools other belts carry:

- Read a file with `sed -n '400,520p' file`, `cat file` or `head -50 file`.
  Read a slice of the output with `sed -n` and a pipe, never by re-running
  the whole command.
- Write a file with a quoted heredoc, which expands nothing:

  ```
  mkdir -p dir
  cat > dir/file.md <<'EOF'
  the content
  EOF
  ```

  Append with `>>` or `tee -a`. `mkdir -p` first: nothing here creates parent
  directories for you.
- Edit a file by matching a unique region: run grep -c on the target text
  first, and only when it answers exactly one, apply the patch — `sed -i`, or
  an inline python patch when the text spans lines. A patch that could match
  twice is a patch aimed at the wrong file.
- Search inside a repository with git grep -n pattern — it respects
  .gitignore the way a search tool would. Outside a repository, grep -rn
  --exclude-dir=.git pattern.

A big result is cut to its first half and its last half, and the WHOLE output
is filed beside this node's own log; the result names that file with a line
like `[output truncated; full output: /path/to/action-000007.txt]`. The file
is on disk: read the range you need from it with `sed -n`, or cat it whole.

PDFs, scans and office documents go to `read_document`, never to `cat`: catting
a PDF yields bytes, and the billed parser is on the belt for exactly that
page. A job started in the background is read through `jobs`, which is where
its log path is.

Never simulate execution. Do not describe what a command would do, do not
write the output you expect: run it, and read the observation.
