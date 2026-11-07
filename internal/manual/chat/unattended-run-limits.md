# What an unattended run may touch

## Are factory steps sandboxed — what a step, `codeaf chat --once` or `codeaf exec` may change and read

Yes. A run with nobody at the keyboard has its commands kept inside its own job. That is a factory step (a stage conversation), `codeaf chat --once`, the conversation a host opens for `--once`, and `codeaf exec`. An AgentField node reaches codeaf through the last three. The allow posture (`--yolo`, `CODEAF_FACTORY_POSTURE`) still decides what is asked; these limits hold under every posture.

A command in such a run may:

- read and write inside the folder it works in: the item's worktree, or the folder `--once` or `exec` was started in;
- write the repository's own git folder, so a worktree can commit, fetch and make branches;
- write its own temp folder and its own cache folders (see "its own temp and cache folders");
- read the rest of the computer, so toolchains under `/usr`, `/opt/homebrew`, Xcode and the Go root work;
- use the network.

The run's own `read`, `write` and `edit` tools keep to the same limits, even though they run inside codeaf.

Interactive conversations are not limited this way: a person at the keyboard sees each command, and the approval settings decide.

## What is blocked — writes outside the checkout, brew install, reading ~/.ssh, keys, docker

Refused in an unattended run:

- writes anywhere else: the home folder, a Homebrew prefix (`brew install` fails), `~/Library`, other repositories;
- reads of keys and sign-ins: `~/.ssh`, `~/.aws`, `~/.config/gh`, `~/.gnupg`, `~/.netrc`, `~/.git-credentials`, `~/.npmrc`, `~/.pypirc`, `~/.kube`, `~/.config/gcloud`, `~/.azure`, `~/.docker/config.json`, the keychain, and codeaf's own `config.json`, `credentials.json`, `identity.json`, `device.json` and `vault.enc`;
- the docker and podman sockets, so `docker pull` and `docker run` fail;
- writes to the repository's git hooks, its git `config`, and its `.codeaf` folder, so a step cannot leave something that runs later outside these limits.

## The sentence a blocked command reads — "codeaf blocked this"

When a command fails because of these limits, the person and the model read this line under the command's own error:

`codeaf blocked this: a run with nobody at the keyboard may change files only in its checkout and its own temp and cache folders, and may not read keys and sign-ins or use docker; to allow more, add "jail.allow" to the repository's .codeaf/config.json.`

A `read`, `write` or `edit` outside the limits is answered with that sentence and nothing else. A command that names a key file outright may instead be refused earlier, by the credential floor in "permissions", in its own words.

## How to allow more — jail.allow, allow docker, extra folders, turn the limits off

Add `"jail.allow"` in any of three places. Every word in any of them counts:

- the repository: `.codeaf/config.json` in your checkout of it. For a factory worktree it is read from the checkout the worktree was cut from;
- your profile: `"jail.allow"` in your own `config.json`, for every unattended run on this computer;
- the environment: `CODEAF_JAIL_ALLOW`, for one launch, such as a script that starts `codeaf exec`.

Each value is a list, or one comma-separated text, of:

- `docker`: allow the docker and podman sockets;
- a folder, absolute or starting `~/`: read and write it, even one the list above hides (`"~/.cache/ms-playwright"`);
- `off`: no limits at all, as before.

```json
{ "jail.allow": ["docker", "~/.cache/ms-playwright"] }
```

A relative path is ignored. The setting is read when the run starts. A run cannot widen its own limits: its `.codeaf` folder is read-only to it, and your checkout and profile are outside what it may write.

## Its own temp and cache folders — GOCACHE, GOPATH, npm cache, pip cache, TMPDIR

Each checkout gets a folder of its own under codeaf's folder, `jail/<id>/`, holding `tmp/` and `cache/`. A command's `TMPDIR`, `TMP` and `TEMP` point at `tmp/`. These point into `cache/`: `GOCACHE`, `GOPATH`, `GOMODCACHE`, `XDG_CACHE_HOME`, `npm_config_cache`, the pnpm store, `YARN_CACHE_FOLDER`, `PIP_CACHE_DIR`, `UV_CACHE_DIR`, `GRADLE_USER_HOME`, `BUN_INSTALL_CACHE_DIR` and `DENO_DIR`. Cargo keeps your own `CARGO_HOME`, so your Rust install is found; only its `registry` and `git` download folders may be written, and `cargo install` is refused. So `go build`, `go test`, `npm install` and `npm test` work inside the checkout without writing your own caches.

Go modules already on this computer are read from your own module cache first, so a job does not download them again. A job folder unused for fourteen days is removed. A command can tell it is limited by `CODEAF_FENCED=1`.

## When this computer cannot apply the limits — Linux without user namespaces, no Landlock, old kernels

On macOS the limits come from the system sandbox. On Linux they come from Landlock, together with user namespaces where the system allows them. Stock Ubuntu does not allow user namespaces for ordinary programs. Then the conversation says once:

`This system limits this run's commands only in part: writes and private files are protected, but docker and the checkout's own git hooks are not fully closed off.`

In that case docker is only pointed at a socket nobody may open (`DOCKER_HOST`), and a command that sets `DOCKER_HOST` itself reaches the daemon. The checkout's own `.git` hooks and `.codeaf` folder are writable, while a worktree's repository keeps its hooks read-only. `/tmp` is the shared one.

With neither Landlock nor user namespaces, or with no system sandbox, the run says once and then runs as before:

`This system cannot confine commands, so this run's commands have your full permissions.`

`codeaf exec` prints the same line on stderr after `note: `. A run is never failed because the limits cannot apply.
