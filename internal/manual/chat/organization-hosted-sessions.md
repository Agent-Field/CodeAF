# Chats that run on the organization's server

## where does my chat run when I use --org — on the organization's server, not on your computer

`codeaf chat --org https://your-organization` opens a chat that runs on a machine the organization keeps, and shows it in your terminal. The folder it works in, its files, its shell, git and every command it runs are there, not on your computer. Your computer only shows the screen and sends what you type.

This is a different thing from `--host` and `--at`, which reach one computer that you or someone else runs. `--org` reaches the organization's server, and the server finds your chat for you.

It is off unless the organization's operator has turned it on. When it is off, the server answers `--org` the way it answers any address it does not know. Nothing about your own chats, on your own computer, changes either way.

## open a chat on the organization's server — codeaf chat --org

```
codeaf chat --org https://your-organization
codeaf chat --org https://your-organization --once "run the tests and tell me what fails"
codeaf chat --org https://your-organization --session hs_0123456789abcdef01234567
```

With no `--session`, it opens the chat you used most recently, or makes your first one if you have none. `--session` takes the name of a chat, which looks like `hs_` followed by 24 letters and digits. `--once` sends one message and prints the reply, as it does elsewhere. Another window on the same command opens another view of the same chat.

You sign in first. Set `CODEAF_ORG_TOKEN`, or run `codeaf attach <url> --token <token>` once for that server. Without either you read: `Sign in to <host> first: set CODEAF_ORG_TOKEN, or run codeaf attach <url> --token <token>.` The address must be `https://`; a plain `http://` address is accepted only for the computer you are on, because a sign-in must not cross a network in the clear.

The chat's folder is chosen on the server, so `--org` cannot take `--no-compact`, `--yolo`, `--max-hours`, `--max-cost`, `--one-model` or `--no-host`, nor be combined with `--host` or `--at`. It says which one cannot travel and why.

## is my code on the organization's server when I use --org — what stays on your computer and what does not

Everything the chat does happens on the server, in a folder that belongs to that one chat: the files it makes, the repositories it clones, what it writes to its shell history. None of it is copied to your computer unless you ask for it. Nothing on your computer is sent to the server except what you type and attach in the chat.

The chat does not see your computer's files, your keys or your other chats. It starts in an empty folder. To work on code, it clones or is given the code there.

The organization's operator keeps the machine and its disk. See *what can the operator of the server see* for what that means for you.

## can other people on the server see my hosted chats — only you, and anyone else gets `There is no such chat.`

Each chat belongs to the person who made it. Another person cannot open it, list it, stop it or delete it, and cannot tell it exists: asking for someone else's chat gets the same answer as asking for one that was never made, `There is no such chat.`

A chat also cannot see another chat. Every chat runs as a separate user of the machine, in a folder only that user can read or write, so a command in one chat cannot read another chat's files, send it a signal or read its environment. Files a command leaves in `/tmp` or `/dev/shm`, which every chat on the machine shares, are closed to other chats unless you open them yourself, and a command cannot connect to another chat's local sockets. For the same reason two separate commands of one chat do not reach each other's abstract sockets (a socket whose name starts with `@`); sockets that are files, such as the ones tools keep in a folder, are not affected. That holds between two chats of the same person too.

What the operator of the machine could do is a separate matter: see *what can the operator of the server see*.

## what can the operator of the server see — honest limits of a hosted chat

Whoever runs the machine, and whoever is its administrator on the hosting platform, can read everything on its disk. A hosted chat is not private from them. The disk is not encrypted by the server software itself.

The chat can reach the internet. A command in it can connect only to the TCP ports the operator allowed (443, 80, 22 and 9418 unless the operator changed the list) and to the organization's own server; any other port is refused by the machine and the command fails with `permission denied` and one line that says so. That keeps a chat off the other services the organization runs on the same private network. It filters by port and not by address, so a hostile service listening on one of the allowed ports is still reachable, and a service of your own on some other port (a dev server on 3000, say) cannot be reached by a command either, even from the same chat; the operator can add ports. Each of the organization's services should still ask for its own sign-in.

A chat has no key of the organization's, no key of yours and no GitHub access of yours. It reaches a model only through the organization's server, with access made for that chat alone, and reaches git only through the same server, which adds your GitHub access to each request. That chat access ends when you stop or delete the chat, and after seven days. If the organization's server restarts, a chat that was running keeps working, and the access is still good.

If you do not want your code on a machine the organization runs, do not open it with `--org`.

## what happens to a hosted chat when I disconnect — the turn goes on and I come back to it

A turn that is running on the server when you disconnect, lose the connection or kill `codeaf chat --org` goes on running there. When you open the same chat again with `codeaf chat --org`, the result is there.

An idle chat stops its own program after a while and starts it again, with its conversation, the next time you open it. Files stay in place the whole time. Processes you started that were still running stay running until you stop the chat.

If the machine that runs chats restarts, its running processes end and its files stay. Your first attach afterwards works: the organization's server notices the machine lost your chat's model access and hands over new access before anything is sent. If the organization's server restarts instead, the chats keep working and so does the access they hold.

## how many chats can I have on the server at once — limits and waiting

The operator sets how many chats may run at once, and how many one person may have running. When you are over your own limit you read: `You already have 2 chats running; stop one first.` (the number is the operator's). When the whole server is full you read: `Waiting for a free slot (3 of 3 in use).` Try again in a moment, or stop a chat you are not using.

A chat counts as running while any process of it is running, including a background one you started. Stopping a chat ends all of them.

## stop or delete a chat on the server — and what is left behind

Stopping a chat ends every process it started, including ones that detached themselves, and takes away its model access. Its files stay, and opening it again starts it again.

Deleting a chat does the same and then removes its folder. Afterwards it is gone: `There is no such chat.`

Both are done through the organization's server by its owner, and only its owner. Neither affects another chat.

## the server says it cannot keep my chat apart — hosted chat refusals

The machine that runs chats measures what it can do when it starts. A chat only runs where two things hold: each chat can run as a separate user of the machine, and the machine can confine a chat's commands to its own folder, including keeping its local sockets from other chats, which needs Linux 6.12 or newer. On an older kernel the default stays closed: the operator reads which setting accepts the gap, `CODEAF_SESSIONHOST_REQUIRE=uid`, and chats then run on the user switch alone, with no limit on what their commands reach (including the port limit). If it cannot, it starts no chats and says: `This server cannot keep your chat apart from other people's, so it did not start one.` That is the operator's to fix.

If the machine cannot confine commands, a command in a chat is not run at all, and you read: `This computer cannot keep commands inside this chat's folder, so no command was run.` It never runs the command with fewer limits instead.

Other sentences you may read when using `--org`: `<host> is unreachable.`, `<host> could not answer (status N).`, `This chat has no model access yet.`, `The place chats run on is not answering.`, `This chat's model access has ended.` and `The model is not answering.`

An operator who has chosen to run on the user switch alone, because the machine has no way to confine commands, sees a warning when each chat opens. That is a choice the operator made, and it is not the default.

## turn on chats on the server — for the operator

Hosted chats are off by default. The operator turns them on by running the chats' machine beside the organization's server, in the organization's deployment (`deploy/org/README.md`, "Hosted chats"), and giving both the same long secret, which `./setup.sh` makes.

On the organization's server set `CODEAF_HOSTED_URL` to the chats' machine, `CODEAF_HOSTED_SECRET`, `CODEAF_HOSTED_MODEL_UPSTREAM` (the model endpoint the organization pays for; OpenRouter's when empty) and `CODEAF_HOSTED_MODEL_KEY` (or the key the server already holds for model calls). Set `CODEAF_SERVER_KEY` too (64 hexadecimal characters; `./setup.sh` makes it): people's own keys are sealed with it, and a server that holds keys sealed with another one, or with none, refuses to start instead of carrying on. Optional: `CODEAF_HOSTED_SESSION_CAP_USD` (what one chat may spend, default 5) and `CODEAF_HOSTED_GIT_HOSTS` (the git hosts chats may reach, default `https://github.com`). On the chats' machine set `CODEAF_SESSIONHOST_SECRET` to the same secret, and optionally `CODEAF_SESSIONHOST_MAX_RUNNING`, `CODEAF_SESSIONHOST_MAX_PER_PERSON`, `CODEAF_SESSIONHOST_IDLE`, `CODEAF_SESSIONHOST_MODEL` and `CODEAF_SESSIONHOST_CONNECT_PORTS` (the TCP ports commands may connect to, default `443,80,22,9418`).

`codeaf sessionhost probe` prints what the machine can do and exits non-zero when it cannot keep chats apart. The machine runs as root inside its container and keeps only the capabilities it needs to switch users; it holds no organization secret except the shared one and no model key. The key stays on the organization's server, which adds it to each model request.

## what does a hosted chat cost and who pays — the organization's key, the spending limit and the monthly limit

A hosted chat calls its model through the organization's server, with the organization's key unless you stored your own. Every call is counted to the cent in your My space on that server, and the organization's operator sees only totals. A chat stops at its own spending limit, five dollars unless the operator set another, and you read: `This chat has reached its spending limit of $5.00, so it is paused. Start a new chat to carry on.` Your My space has a monthly limit too, twenty dollars unless the operator set another, and at it you read: `You have reached your monthly limit of $20.00, so this chat is paused. The organization's operator can raise it in Settings.`

A refused message makes no model call and costs nothing. Calls made with your own key are counted too, against both limits. A chat that is making as many requests as it may at once, four, reads: `This chat is already making as many requests as it may at once. Try again in a moment.`

## keep my own model key or GitHub access on the organization's server — stored sealed, only yours

You can give the organization's server your own OpenRouter key and your own GitHub access (a token) for your chats. They go to `PUT /v1/hosted/keys/openrouter` and `PUT /v1/hosted/keys/github` with your sign-in and the body `{"key":"..."}`; `GET /v1/hosted/keys` lists what you stored as the provider, the last four characters and when, never the key, and `DELETE /v1/hosted/keys/<provider>` removes one. codeaf's own chat has no screen for it yet.

Each person's keys are sealed on the server's disk under a key of that person's own, which is itself sealed by the server key the operator set; another person cannot read them, and neither can a chat. With your own model key stored, your chats' calls go out with it and never with the organization's, and if it cannot be opened the chat pauses and says so rather than using the organization's instead. If the operator has set no server key, the server answers that it cannot keep personal keys safe yet and stores nothing; chats then use the organization's key. Signing in with GitHub gives the server your GitHub access for git without storing anything yourself, unless you stored your own.

## use git in a hosted chat — clone and push through the organization's server

A hosted chat is set up so that `git clone https://github.com/<owner>/<repo>.git` and `git push` work with your GitHub access without the chat ever holding it. The commands go to the organization's server, which adds your access to each request and sends it on; only the git hosts the operator allowed (GitHub unless changed) are reached this way, and one line per git operation is kept, which you can read with `GET /v1/hosted/audit` (your own lines only).

If you have no GitHub access stored you read: `You have no GitHub access stored on this server yet. Add it, then try again.` A host the operator did not allow reads: `This server does not reach that git host for chats.` The chat itself holds only access for that one chat, which ends when the chat is stopped. It is not limited to one repository: your GitHub access decides what you can reach.
