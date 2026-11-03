# A dev box on your own Railway account

## Can I run codeaf on my own server or an always-on computer in the cloud?

Yes. `codeaf devbox up <name>` creates an always-on computer on **your own Railway
account** and pairs it into your identity. You pay Railway for its compute and
persistent volume. Your laptop can sleep while the box keeps running.

The box holds your whole identity, including access to chats, memory and saved provider
keys, just like any paired computer. The pairing code joins only that identity:
**one code never gives both** chats and remote access. Terraform separately writes
the box's remote key and admits the laptop that creates it in the box's
configuration. `codeaf chat --at <name>` on that laptop opens a chat there without
another prompt. `/devices` and Settings → Devices list the chosen name. The box
dials out through the same relay as the laptop; it opens no public port or domain.
Restarts preserve its remote key and any later admissions or revocations.

For a server you already own, install codeaf there, pair it with `codeaf pair`,
and run `CODEAF_SERVE_SYNC=1 codeaf serve` so it receives your saved keys, or use
`codeaf chat --host <ssh-host>` when ssh works.

## Set up a dev box with codeaf devbox up

Install Terraform from https://developer.hashicorp.com/terraform/install. Create
a Railway token at https://railway.com/account/tokens and set `RAILWAY_TOKEN`.
`RAILWAY_API_TOKEN` is also accepted when `RAILWAY_TOKEN` is empty. No token or no
Terraform means nothing is created.

Run `codeaf devbox up build-box`. Names use lowercase letters, digits and hyphens,
start and end with a letter or digit, and are at most 32 characters. An existing
device name is refused. An existing project is left alone; if its box is reachable
and setup values remain, up finishes removing those values without pairing again.

The command shows a Terraform plan and asks once:
`Create these on your Railway account? [y/N]`. After approval, codeaf creates the
box without a pairing code, waits up to three minutes for Railway to finish
starting it, then sends a one-time pairing code. It checks deployments every
three seconds; if they have not settled by the limit, it says so and proceeds.
Only the service variables may change when sending the code; a plan changing
other resources is refused without applying it or asking you again.

Allow a few minutes to install and join. If Railway swaps the deployment while it joins, or
the code expires, codeaf sends a fresh code, up to three codes in total, keeping the same remote key and admission list. Up
probes the pinned key every five seconds while waiting, including when a join
acknowledgement was lost. After acknowledgement it waits at most three minutes
for reachability. It says `<name> is up` only after a successful connection and
removal of the one-time setup values. Then open `codeaf chat --at build-box`.

## Dev box up flags for folder, region, workspace and channel

- `--dir <folder>`: keep the Terraform in this folder.
- `--yes`: skip confirmation; required when stdin is not a terminal.
- `--region <railway region>`: choose a region; empty lets Railway choose.
- `--workspace <name-or-id>`: choose a Railway workspace. With several workspaces,
  name one; a workspace token that cannot list names can use an id or omit it.
- `--channel stable|staging|dev`: follow that channel. The default follows the
  running binary; a source build uses `dev`, and a release candidate uses stable.
  Each channel syncs through its own relay by default, so the box is pinned to
  the relay of the laptop that created it, whichever channel it installs.

## How saved provider keys, settings and shared memories reach the box

The box starts `codeaf serve` with `CODEAF_SERVE_SYNC=1`, so it pulls the identity's saved provider keys,
settings and shared memories at startup and every five seconds by default,
the same interval an open terminal uses; without that variable `codeaf serve`
syncs nothing, as before. `CODEAF_SYNC_INTERVAL_MS` changes that
interval in milliseconds. `codeaf devbox up` sends the
creating computer's saved keys and settings once the box answers, so the first
chat there has them; if that fails it says `Could not send this computer's saved
keys yet` and opening codeaf on that computer sends them. Keys saved on another
computer reach the identity once that computer has opened codeaf.
Keys held only in environment variables are not saved or shared.

Serving begins while the first round is still running. A `codeaf chat --at <name>`
started before that round finishes can report a missing provider key. Wait a
moment and open the chat again.

## Where the Terraform and the always-on computer's identity live

By default the Terraform is in `<codeaf home>/devbox/<name>/`. `CODEAF_HOME` moves
that home. `--dir <folder>` selects another folder for up, status or down. You own
the folder: read or edit the module, or run Terraform there yourself with
`RAILWAY_TOKEN` set. Keep using that same folder for later status and down.

There is one private project `codeaf-<name>`, one service `box`, and one volume
`home` mounted at `/data`. The service runs continuously with one replica. The
volume keeps the identity, installed binary and work folder across restarts and
redeploys. A successful first join writes a marker, so it never reuses that code.
Before its first join, a boot without a code waits without downloading. A failed
join waits for a new code from up. The first install uses the same release as the
computer that ran up when both use the same stable, staging or dev channel, so
it does not depend on GitHub's API limit. If that install fails, it tries the
channel's newest build once in the same boot. Source builds, release candidates
and a different `--channel` install the channel's newest build directly. Later
boots refresh to the channel's newest build and keep the earlier volume copy if
that fails. With no earlier binary, a failed install waits a minute before
exiting so Railway can retry.

## One-time setup values, Terraform state and private files

Non-secret choices are in `codeaf.auto.tfvars.json`. The Railway token is passed
only through the Terraform process environment. The pairing code and base64
remote files are passed only as `TF_VAR_pair_code`, `TF_VAR_remote_key` and `TF_VAR_remote_devices`, never
written to that variables file. The saved plan is deleted on completion or
failure. Once the pinned remote key answers, codeaf says
`Removing the one-time setup values from Railway.` and applies a plan that can
change only the service variables. It asks no second confirmation. This removes
the spent pairing code, the box's remote key and its admission list from Railway
and the folder's current state, then deletes `terraform.tfstate.backup` containing
the previous state. The box restarts and keeps using its seeded volume files.

Until cleanup succeeds, **state and backups hold those secrets**. Keep the folder
private (0700); state and backups are tightened to 0600 after Terraform writes.
A hand-run apply after joining needs no setup values.
When created with `--region`, change `region` in the variables file to move it.
Without `--region`, the module leaves placement to Railway; to pick one later,
edit the service's `regions` setting in `main.tf`.

## Check whether my dev box is up and when it was last seen

`codeaf devbox status <name>` reads the project's Terraform outputs and your device
list; it needs Terraform but no Railway token. `codeaf devbox status` checks every
folder under the default devbox directory. Both accept `--dir <folder>` and
`--yes` (status has no confirmation).

The status shows the name, region, project and last seen when available. On the
creating laptop it first opens and immediately closes a remote connection with
the box's pinned key, with a three-second timeout. A successful probe means `up`
and last seen now, including with released boxes that hold no directory watch.
It logs a short connection on the box and may start its engine process, without
opening a chat or making a provider call.

If that connection fails or this laptop has no alias, status falls back to
directory online presence, then `up (seen recently)` for a record seen within
five minutes when presence is unavailable. Missing readings are omitted. Empty
state says `<name>: not up`. A `codeaf serve` started with `CODEAF_SERVE_SYNC=1`,
as the box's is, holds one shared directory watch and refreshes its name and
capabilities every two minutes when home sync is eligible. Quiet homes, sync off, unavailable cells and pending rotation
publish nothing. A changed identity, device or relay ends that presence worker.

## Opening the dev box from another computer

Only the laptop that ran `codeaf devbox up` is admitted for remote access. Pairing
another computer into your chats does not admit it or copy the creating laptop's
local name alias. With the box's key-derived name, a new computer is asked for a
remote pairing code; even a correct code is refused by headless `serve` because
there is no terminal to approve it. It does not wait for a yes/no answer there.
An unadmitted key with a saved pin is also refused. There is no unattended command
to admit another computer yet; use the creating laptop.

## Send a task to my dev box

A dev box also takes tasks. It starts `codeaf serve --take-tasks` whenever its
installed build lists that option, so a chat on your laptop can run one task on the
box while the conversation and transcript stay on the laptop:
`/task --on <name> <brief>` names it for one task, and Settings → Run tasks on
sends every task that can go. The box's synced provider keys pay, and the spend
appears on the laptop too. A box whose build predates the option starts plain
`codeaf serve` and takes no tasks; sending one says
`<name> is not taking tasks right now — run codeaf serve --take-tasks on it`, and
the box picks the option up when its channel's build has it and it restarts. See
*Tasks on another computer* for what can go and what happens when a computer
stops answering.

## How do I remove my dev box or stop paying for it?

Run `codeaf devbox down <name>` with the Railway token set. It asks
`Destroy <name> and everything in project codeaf-<name>? [y/N]`. `--yes` skips the
question; `--dir <folder>` selects the Terraform folder used to create it.

Down destroys the project, service and volume, including the box's work and
identity, then removes its device from your identity. If destruction fails, it
keeps the device. The Terraform folder stays on your laptop; a later up can use
it again. Removing a device in Settings alone does not destroy the paid cloud
resources.

Down selects the saved joined device id from `codeaf.devbox.json`, so renaming
it does not prevent removal. Without that id it selects the active device by
name, ignoring removed records. If none matches, the receipt says no device was
removed. Reusing the name after down works.

## Dev box pairing, reachability and setup cleanup failures

`<name> did not join: ...` means the offer ended without a successful connection.
The box remains on your Railway account. Run the printed `codeaf devbox down
<name>` command, then up again. A lost acknowledgement can leave it running;
this receipt does not claim it is idle. Nothing is destroyed automatically.

`<name> took its pairing code but is not reachable yet: ...` means the offer
succeeded but its pinned key did not answer within the wait. It remains on your
Railway account. Run the printed `codeaf devbox status <name>` command to check,
or `codeaf devbox down <name>` to remove it.

`<name> is up, but its one-time setup values are still on Railway and in
<folder>: ...` means reachability was proved but cleanup did not finish. Secrets
may remain in Railway, state or its backup. Run the printed `codeaf devbox up
<name>` command again to clear them without pairing, or down to remove the box.
When cleanup is already complete, up keeps saying it is already up.

Recovery commands include `--dir <folder>` for a non-default folder, quoted when
needed. Keep that flag when copying a command.

## How much does a dev box cost? Does codeaf offer AWS?

You pay Railway directly for the computer's compute and persistent storage,
according to your Railway plan and usage. codeaf does not set a fixed price or
include that computer in a subscription. Provider calls also spend your own
provider keys as usual. `codeaf devbox down <name>` removes the paid resources;
closing your laptop or ending a remote chat keeps the computer running.

AWS: a Terraform module exists in codeaf's source but has never been applied, and
`codeaf devbox` does not offer it yet.
