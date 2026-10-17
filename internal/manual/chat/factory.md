# The factory

## what is the factory — jobs, runners and a pull request at the end

The factory takes work that is already written down, such as a GitHub issue or a brief handed over from a chat, and runs it through the steps of its repository's pipeline on a runner. A person is asked at fixed points, and a job that changes code ends as a pull request. Nothing starts by surprise: a job a rule does not allow asks first, and a repository without `.codeaf/factory.yaml` runs on the defaults, under which nothing starts on its own.

Three things make one factory:

- the **service**, started with `codeaf factory serve`, which holds the jobs and the people;
- one or more **runners**, machines that pull a job and run it with their owner's keys;
- the **people**, each signed in with their own GitHub.

A machine with no factory set up has none of this: the chat's factory tools are absent from its belt rather than present and failing.

## codeaf factory serve — host a team factory, listen address and HTTPS

`codeaf factory` (the same as `codeaf factory serve`) runs the service and prints the address to give people, plus one operator access link. It listens on `127.0.0.1:7420` unless `--listen host:port` says otherwise, or the `listen` key of `~/.codeaf/factory/config.yaml` does (`--config path` picks another file). It runs no job itself: Run queues a job, and a runner that has joined pulls it. With no runner attached a job stays queued.

The config keys are `name`, `listen`, `access_token`, `repos` (a list of `owner/name`), `github_client_id` (an OAuth app with device flow on), `team` (GitHub handles), `mode` (`solo` or `team`) and the HTTPS keys `tls`, `domain`, `cert_file`, `key_file`, `cache_dir` and `trusted_proxies`. Unknown keys are refused. Serving plain HTTP on a non-loopback address prints a warning, because access tokens and sign-in sessions would cross the network unencrypted. `--model slug` and `--one-model` pin the model its triage reads use. `--new-operator-link` prints a fresh one-time operator recovery link. Only one service runs per codeaf home (`serve.lock`); a second one says so.

## give someone access — codeaf factory token, revoke, revoke-sessions and name

The service is joined with an access link, which the operator creates for one person and which can expire. Commands on the machine that hosts it:

- `codeaf factory token add [--expires 7d]` mints an access token and prints it alone; `codeaf factory token list` lists the live ones by id; `codeaf factory revoke <id>` ends one.
- `codeaf factory revoke-sessions` ends every signed-in session at once; people sign in again.
- `codeaf factory name` shows or changes the name the factory goes by.
- `codeaf factory steps` prints the built-in step prompts; `codeaf factory convert` turns a Markdown pipeline into `.codeaf/factory.yaml`.

An access link looks like `codeaf://factory/join?url=https://factory.example#token=…`. The secret is in the part after `#`, so it is never sent as part of an address. A link checks, in this order: the factory answers, it is not revoked, not expired, not used up, and the person's GitHub can see at least one of its repositories. Each failure is one plain sentence.

## codeaf attach and codeaf detach — use another machine's factory

`codeaf attach <url> [--name n] [--token t]` writes `attach.json` in the factory folder (`~/.codeaf/v3/factory`, readable by you only) so this machine's windows and runners use that factory instead of serving their own. It does not dial the factory: a team box that is down now is still the one you mean. `--name` is the name this machine goes by there (its hostname by default) and `--token` is a session you already hold. `codeaf detach` removes the file, and this machine's own factory is used again. Only `http://` and `https://` addresses are accepted, and a link with a user name in it is refused.

## codeaf runner join — run a factory's jobs on this machine, sign in with GitHub

`codeaf runner join <link>` joins this computer to a factory and runs its jobs here until you press ctrl-c. `<link>` is an access link (`codeaf://factory/join?url=…#token=…`); you sign in with GitHub on the page it names, using the code it prints, and the factory issues a session for you alone, so the runner works as you and never as the operator. Other spellings: `<secret> --url <factory>` (the link's secret, with the address spelled separately), or `<url> --token <session>` when you already hold a session. `--name` names the machine. `--attach-only` writes the attachment and returns, which is what `codeaf attach` does too.

A runner runs only its owner's jobs, with its owner's keys; keys are shown by name and never by value. One process per codeaf home holds the runner role; a second `runner join` shares the first one's session and waits to take over when it stops, so joining twice registers one machine. An access link that was used or expired is refused with a plain sentence and no session is kept. The service has no separate expiring runner token: a link made to expire in an hour is the one-time command.

## from chat to the factory — factory_add, factory_recipe, factory_item and the manager's tools

On a machine with a factory set up, a conversation can hand work over. Every one of these shows a card and writes nothing until you say yes; none of them starts a job.

- `factory_add` offers one piece of work (a title, a brief, a repository) to the factory. Your `add it` writes the item, as new.
- `factory_recipe` offers one line for a repository's recipe: a stage, a policy or a habit, saved to `.codeaf/factory.yaml` on your `bank it`.
- `factory_item` offers a change to the one item a conversation is about: its stages, gate, budget or thinking, within the recipe's bounds.

The conversation that manages an item also has `factory_run` (shape its run), `factory_start` (start, resume or continue it when you ask) and `factory_answer` (reply to a question a step asked; send it to you when it turns on taste, scope, risk, money or access). The foreman conversation has `factory_floor` (read the whole floor and select items for you to run). These are absent from every other conversation, from `--once` runs and from windows over `--host` or `--at`.

## which model runs factory jobs — Task models and one model

Factory reads, manager turns and step conversations follow Task models. With one model selected, every text role uses that exact model on its selected connection. A missing, unreachable or mismatched choice refuses the turn; it does not choose a fallback. The daily crew cap also refuses new factory admission when reached. A job's own spending ceiling remains separate.

An explicit `--one-model` launch keeps its pin. Interactive chats keep their own model. Under the saved one-model policy, a factory manager turn uses its governed factory conversation. If an open chat holds that manager conversation, close it before the background turn can proceed. A running conversation keeps the model it started with.
