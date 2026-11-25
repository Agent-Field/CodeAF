# Chats that run on the organization's server

## where does my chat run when I use --org — on the organization's server, not on your computer

`codeaf chat --org https://your-organization` opens a chat that runs on a machine the organization keeps, and shows it in your terminal. The folder it works in, its files, its shell, git and every command it runs are there, not on your computer. Your computer only shows the screen and sends what you type.

This is a different thing from `--host` and `--at`, which reach one computer that you or someone else runs. `--org` reaches the organization's server, and the server finds your chat for you.

It is off unless the organization's operator has turned it on. When it is off, the server answers `--org` the way it answers any address it does not know. Nothing about your own chats, on your own computer, changes either way.

## open, start or reopen a hosted chat in a desktop organization window

Sign in to the organization in the desktop app and open its window. The window offers new chat only when the organization has enabled hosted chats. Its chat list shows the chats you started and any chats the server says were shared with you. In a shared chat you can use the actions the organization's server says you hold there, such as reading the conversation or sending messages. The window asks the server each time it lists the chats, so a change to the organization's permissions shows up on the next refresh, and a chat the server named no permissions for is shown as watch only. The server checks every action as you take it, whatever the window shows. Use a Personal window for chats on your computer.

The organization chooses where each chat works on its server. The desktop app creates no conversation folder on your computer for that chat. Attachments are refused with `Attachments cannot go to a chat on <host> yet.`

If the server is down, the app says `<host> is unreachable.` If your sign-in is missing or has expired, it says `Sign in to <host> first.`

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

## can other people on the server see my hosted chats — private by default, shared only when you share it, and anyone else gets `There is no such chat.`

A hosted chat is private to the person who made it, unless the organization's policy makes chats of this kind start otherwise (the policy's default for `session`). A private chat is left out of every other person's list and out of the organization's live updates: nobody else is told it exists, who owns it or what state it is in, and that includes the operator unless the policy says the operator sees private things (see *can the operator open my hosted chat*). Someone else who asks for it, to open it, list it, stop it or delete it, gets the same answer as for a chat that was never made, `There is no such chat.`

You can share a chat with a person, a team or the whole organization the way you share anything else (`POST /v1/resources/session/<chat>/grants` with who and a role; see *Sharing*). Once you do, it shows up in that person's `shared` list. Taking the access away takes it off their list again, and cuts them off if they are watching it at that moment: see *what can someone I shared my hosted chat with do*.

A chat made by a bot belongs to the person who made the bot, is private like any other, and is shown to nobody else until that person shares it.

What the operator of the machine could do is a separate matter: see *what can the operator of the server see*.

## can one hosted chat read another chat's files or processes — the sandbox around a hosted chat: every chat runs as its own user of the machine

Every chat runs as a separate user of the machine, in a folder only that user can read or write, so a command in one chat cannot read another chat's files, send it a signal or read its environment. Files a command leaves in `/tmp` or `/dev/shm`, which every chat on the machine shares, are closed to other chats unless you open them yourself, and a command cannot connect to another chat's local sockets. For the same reason two separate commands of one chat do not reach each other's abstract sockets (a socket whose name starts with `@`); sockets that are files, such as the ones tools keep in a folder, are not affected. That holds between two chats of the same person too. One thing a command can still see is that other chats exist: the folders above a chat are readable so the chat can save its history, which lists their names (a hashed name for each person and a random identifier for each chat) and nothing inside them.

A chat shared with someone else is still this one chat: it runs in its owner's folder as its owner's user, so what a guest does through it cannot reach the guest's own files, and nothing in it is the guest's.

## what can someone I shared my hosted chat with do — watching, interrupting, answering, sending messages, and what only the owner can do

A chat shared with you opens with `codeaf chat --org <server> --session hs_...`, and your own chats stay what a bare `--org` opens. What you can do depends on the permissions of the role you were given:

- **view** (every role has it): watch the chat live and read what has been said and what is running. Anything that would send, change or run something is refused. Sharing means choosing a role, and the access the policy gives everyone in the organization when it makes chats visible is at view.
- **steer**: also interrupt a running turn and change where it is going.
- **answer**: also answer a question the chat is waiting on.
- **run** (the `edit` and `full` roles have it): also send messages, files and images, and run a command the chat's own way, as the chat's owner would.

Everything else on the chat's wire stays with the owner whatever role a person holds: approving a command, changing a setting, opening the chat's files, stopping its work. A command that needs approval waits for the owner. Stopping or deleting the chat itself is a separate door that needs the `manage` permission, which the `full` role holds: see *stop or delete a chat on the server*.

## what happens when I give someone run access to my hosted chat — they act with my GitHub identity, files and spending limits

**A person you let send messages acts with your GitHub identity, your files and your spending limits.** The chat is yours: its commands run in your folder on the server, as your user, with your model access, and its git goes through your GitHub access, whoever typed the message. What it costs is yours too (see *who pays*), against your chat limit and month. The server notes who asked on each message, on each charge and on each use of your GitHub access, so you can read it afterwards. If you would rather nobody else sends messages, share at `view` or `comment`, which do not hold `run`, or ask the operator to take `run` out of the roles in the organization's policy: nothing else needs to change.

## why was I refused in a shared hosted chat — the sentences, the keyboard and a removed share

You read `This chat is shared with you. Only its owner can do that.` for what only the owner can do. Sending without the permission reads `You can watch this chat, but you were not given permission to send messages in it. Ask its owner.`, steering without it `You can watch this chat, but you were not given permission to steer it. Ask its owner.`, and answering without it `You can watch this chat, but you were not given permission to answer its questions. Ask its owner.`

Only one window types at a time. While the owner has the chat open with the keyboard, a message or a redirect from someone else is refused by the chat with its own sentence about the keyboard; taking the keyboard (a person who may send can) moves it to their window and tells the owner's, interrupting and answering are not affected, and a message goes through once the owner has closed their window.

When the owner takes your access away, or the policy changes so you no longer have it, your window is ended within a few seconds (five unless the operator set another heartbeat) and reads `Your access to this chat was removed.`

Opening a chat that is not running starts it for its owner, which can be refused while its owner or the machine is running as many chats as are allowed. Then you read `This chat is not running, and there is no room to start it right now. Try again later.`

## who pays for a shared chat — when someone else is in my hosted chat, it runs as the owner, with the owner's access and limits

A shared chat always runs in the owner's folder on the machine, with the owner's model access and the owner's spending limits: what a guest's messages cost is counted to the owner, in the owner's My space, against the owner's chat limit and month. The server notes who last put words into the chat next to each charge, so the owner can see it was a guest, but that is approximate: a model call is credited to whoever last sent a message, redirected a turn or answered through the server, and the owner opening their own window puts it back to the owner. The owner's activity list (`GET /v1/hosted/audit`) holds one line for each message a guest sent, with who sent it (never what it said), and each line for the owner's GitHub access carries who was asking at the time.

## can the operator open my hosted chat — only metadata, and only if the policy makes the chat visible

By default the operator holds no right on a private chat: it is refused with `There is no such chat.` and is not told the chat exists. When the policy makes new chats not private, or says the operator sees private things, the operator's list shows the chat's identifier, owner, state and times and nothing else, and the operator still cannot open it, read its history or see its folder. Whoever runs the machine itself is another matter: see *what can the operator of the server see*.

## bots and hosted chats — what a bot may do with a chat, and what it may not

A bot (see *Bots*) can start (when the organization's policy lets agents create things), list, open, stop and delete hosted chats, and it holds exactly what the person who made it holds on each chat right now, narrowed by its scope. If that person loses access to someone else's chat, the bot loses it on its very next request, and an open window of the bot's is ended within a heartbeat. A chat the bot starts is owned by that person and recorded as made by the bot. A bot cannot touch a person's stored keys (`/v1/hosted/keys`): that answers `a bot cannot use this yet`. A bot that holds the `run` verb on its creator's own chat can type into it, as its creator would; a bot whose scope leaves `run` out is treated like a guest. On someone else's chat a bot gets what its creator's role gives, and no more.

## may I still start a chat on my own computer after joining an organization — the hosting policy

An organization is its own account, separate from your personal one: you switch between them, and what you make in the organization's account belongs to the organization. When the organization runs hosted chats, every chat in its account is a hosted chat, and a chat does not start on your computer from inside it. There is no setting on your side to override that; the way to work on your own computer is to switch to your personal account, which does not see the organization's chats or files.

The organization's policy has a `hosting` section for this. `local_sessions` is `allow`, `warn` or `refuse`; left empty it means `refuse` when the organization runs hosted chats and `allow` when it does not, so an organization with no machine for chats behaves as before. `GET /v1/hosted/sessions` reports the value that applies as `hosting.local_sessions`, with the words to show for it: for `refuse` `This organization runs your chats on its own server, so they do not start on this computer. To work here instead, switch to your personal account, which does not see the organization's chats or files.` and for `warn` `This organization keeps your chats on its own server. A chat started on this computer is not part of the organization's account.` Only the operator changes the policy, with `PUT /v1/org/policy`. `max_session_cost_usd` is what one hosted chat may spend before it is paused; zero keeps `CODEAF_HOSTED_SESSION_CAP_USD` (five dollars unless set). This version reports the setting and enforces the cost limit; `codeaf` on a computer does not yet act on `local_sessions`.
## what can the operator of the server see — honest limits of a hosted chat

Whoever runs the machine, and whoever is its administrator on the hosting platform, can read everything on its disk. A hosted chat is not private from them. The disk is not encrypted by the server software itself.

The chat can reach the internet. A command in it can connect only to the TCP ports the operator allowed (443, 80, 22 and 9418 unless the operator changed the list) and to the organization's own server; any other port is refused by the machine and the command fails with `permission denied` and one line that says so. That keeps a chat off the other services the organization runs on the same private network. It filters by port and not by address, so a hostile service listening on one of the allowed ports is still reachable, and a service of your own on some other port (a dev server on 3000, say) cannot be reached by a command either, even from the same chat; the operator can add ports. Each of the organization's services should still ask for its own sign-in.

A chat has no key of the organization's, no key of yours and no GitHub access of yours. It reaches a model only through the organization's server, with access made for that chat alone, and reaches git only through the same server, which adds your GitHub access to each request. That chat access ends when you stop or delete the chat, and after seven days. If the organization's server restarts, a chat that was running keeps working, and the access is still good.

If you do not want your code on a machine the organization runs, do not open it with `--org`.

## what happens to a hosted chat when I disconnect — the turn goes on and I come back to it

A turn that is running on the server when you close its desktop window, disconnect, or quit `codeaf chat --org` goes on running there. Reopen the chat in the organization window or with `codeaf chat --org` to read the result. Stop in the desktop chat interrupts the current turn. The server's stop action below ends the chat's processes.

An idle chat stops its own program after a while and starts it again, with its conversation, the next time you open it. Files stay in place the whole time. Processes you started that were still running stay running until you stop the chat.

If the machine that runs chats restarts, its running processes end and its files stay. Your first attach afterwards works: the organization's server notices the machine lost your chat's model access and hands over new access before anything is sent. If the organization's server restarts instead, the chats keep working and so does the access they hold.

## how many chats can I have on the server at once — limits and waiting

The operator sets how many chats may run at once, and how many one person may have running. When you are over your own limit you read: `You already have 2 chats running; stop one first.` (the number is the operator's). When the whole server is full you read: `Waiting for a free slot (3 of 3 in use).` Try again in a moment, or stop a chat you are not using.

A chat counts as running while any process of it is running, including a background one you started. Stopping a chat ends all of them.

## stop or delete a chat on the server — and what is left behind

Stopping a chat ends every process it started, including ones that detached themselves, and takes away its model access. Its files stay, and opening it again starts it again.

Deleting a chat does the same and then removes its folder. Afterwards it is gone: `There is no such chat.`

Both are done through the organization's server by its owner, or by someone who holds the `manage` permission on the chat (the `full` role). Anyone else who can see the chat reads `Only this chat's owner, or someone it is shared with in full, can stop or delete it.`, and anyone who cannot reads `There is no such chat.` Neither affects another chat.

## the server says it cannot keep my chat apart — hosted chat refusals

The machine that runs chats measures what it can do when it starts. A chat only runs where two things hold: each chat can run as a separate user of the machine, and the machine can confine a chat's commands to its own folder, including keeping its local sockets from other chats, which needs Linux 6.12 or newer. On an older kernel the default stays closed: the operator reads which setting accepts the gap, `CODEAF_SESSIONHOST_REQUIRE=uid`, and chats then run on the user switch alone, with no limit on what their commands reach (including the port limit). If it cannot, it starts no chats and says: `This server cannot keep your chat apart from other people's, so it did not start one.` That is the operator's to fix.

If the machine cannot confine commands, a command in a chat is not run at all, and you read: `This computer cannot keep commands inside this chat's folder, so no command was run.` It never runs the command with fewer limits instead.

Other sentences you may read when using `--org`: `<host> is unreachable.`, `<host> could not answer (status N).`, `This chat has no model access yet.`, `The place chats run on is not answering.`, `This chat's model access has ended.` and `The model is not answering.`

An operator who has chosen to run on the user switch alone, because the machine has no way to confine commands, sees a warning when each chat opens. That is a choice the operator made, and it is not the default.

## can my admin read my chats — what the operator, my company's IT and whoever runs the server can see

**The organization's operator cannot open your private chat.** Asked for it, the server answers `There is no such chat.`, the same as for a chat that was never made. It is not in the operator's list, and the operator never reads its history or opens its folder. A policy can make new chats show to the operator as a row of facts (an identifier, an owner, a state and times), and even then never the conversation. What the operator does see is the organization's people, roles, policy and the totals of what each person spent.

**Whoever runs the machine can read it.** The people who administer the computer the server runs on, or the platform that hosts it, can read everything on its disk, including every chat's folder: a hosted chat is not private from them, and codeaf does not encrypt the disk. If your company's IT runs the server, that includes them.

If you do not want your code on a machine your organization runs, do not use the organization's window for it: switch to your personal account, where the chat runs on your own computer and the organization never sees it.

## switch between Personal and my organization — two accounts, and what each window can see

The desktop app has two kinds of account side by side: **Personal**, which lives on your computer, and each organization you joined, which lives on its server. A switch at the foot of the left strip chooses where a window works, and a window is one account at a time.

Personal never sees the organization's chats, files or people, and an organization's window never shows anything from Personal: nothing of one is drawn, counted or searched in a window of the other. What you make in an organization's window belongs to the organization. When the organization runs hosted chats, every chat you start there runs on its server and never on your computer; to work on your own computer, switch to Personal. See *may I still start a chat on my own computer after joining an organization*.

## can a teammate send messages in my chat while I am using it — one window writes at a time, and the owner's keyboard comes first

Only if you shared the chat with a role that holds `run` (the `edit` and `full` roles do), and even then one window writes at a time. While you have the chat open with the keyboard, a message or a redirect from your teammate is refused by the chat with its own sentence about the keyboard. It goes through once you close your window, or after they take the keyboard (a person who may send can), which tells your window. Interrupting a turn and answering a question are not held up. Whatever they send runs as you: in your folder, with your GitHub identity and your spending limits, and a command that needs approval waits for you.

## the organization's server is down or my sign-in ended — a hosted chat never runs on my computer instead

If the server cannot be reached, the desktop window says `<host> is unreachable.` once, with **Try again**, and the chat stays where it is. If your sign-in is missing or has ended it says `Sign in to <host> first.` In neither case does the app start a chat on your computer in its place: a hosted chat never falls back to running locally. A turn that was already running on the server goes on without you, and the conversation is there when the server answers again.

## turn on chats on the server — for the operator

Hosted chats are off by default. The operator turns them on by running the chats' machine beside the organization's server, in the organization's deployment (`deploy/org/README.md`, "Hosted chats"), and giving both the same long secret, which `./setup.sh` makes.

On the organization's server set `CODEAF_HOSTED_URL` to the chats' machine, `CODEAF_HOSTED_SECRET`, `CODEAF_HOSTED_MODEL_UPSTREAM` (the model endpoint the organization pays for; OpenRouter's when empty) and `CODEAF_HOSTED_MODEL_KEY` (or the key the server already holds for model calls). Set `CODEAF_SERVER_KEY` too (64 hexadecimal characters; `./setup.sh` makes it): people's own keys are sealed with it, and a server that holds keys sealed with another one, or with none, refuses to start instead of carrying on. Optional: `CODEAF_HOSTED_SESSION_CAP_USD` (what one chat may spend, default 5) and `CODEAF_HOSTED_GIT_HOSTS` (the git hosts chats may reach, default `https://github.com`). On the chats' machine set `CODEAF_SESSIONHOST_SECRET` to the same secret, and optionally `CODEAF_SESSIONHOST_MAX_RUNNING`, `CODEAF_SESSIONHOST_MAX_PER_PERSON`, `CODEAF_SESSIONHOST_IDLE`, `CODEAF_SESSIONHOST_MODEL` and `CODEAF_SESSIONHOST_CONNECT_PORTS` (the TCP ports commands may connect to, default `443,80,22,9418`).

`codeaf sessionhost probe` prints what the machine can do and exits non-zero when it cannot keep chats apart. The machine runs as root inside its container and keeps only the capabilities it needs to switch users; it holds no organization secret except the shared one and no model key. The key stays on the organization's server, which adds it to each model request.

## what does a hosted chat cost and who pays — the organization's key, the spending limit and the monthly limit

A hosted chat calls its model through the organization's server, with the organization's key unless you stored your own. Every call is counted to the cent in your My space on that server, and the organization's operator sees only totals. A chat stops at its own spending limit, five dollars unless the operator set another, and you read: `This chat has reached its spending limit of $5.00, so it is paused. Start a new chat to carry on.` Your My space has a monthly limit too, twenty dollars unless the operator set another, and at it you read: `You have reached your monthly limit of $20.00, so this chat is paused. The organization's operator can raise it in Settings.`

A refused message makes no model call and costs nothing. Calls made with your own key are counted too, against both limits. If the model's provider turns down the organization's key, the chat pauses and you read, once, as a plain sentence and not as a failure that keeps retrying: `The organization's model access was refused by its provider, so this chat paused. Ask the organization's operator to check the model key.` If it turns down the key you stored yourself, you read `Your own model key was refused by its provider, so this chat paused. Replace it or remove it, then send your message again.` The organization's key is never used instead of yours. A chat that is making as many requests as it may at once, four, reads: `This chat is already making as many requests as it may at once. Try again in a moment.`

## keep my own model key or GitHub access on the organization's server — stored sealed, only yours

You can give the organization's server your own OpenRouter key and your own GitHub access (a token) for your chats. They go to `PUT /v1/hosted/keys/openrouter` and `PUT /v1/hosted/keys/github` with your sign-in and the body `{"key":"..."}`; `GET /v1/hosted/keys` lists what you stored as the provider, the last four characters and when, never the key, and `DELETE /v1/hosted/keys/<provider>` removes one. codeaf's own chat has no screen for it yet.

Each person's keys are sealed on the server's disk under a key of that person's own, which is itself sealed by the server key the operator set; another person cannot read them, and neither can a chat. With your own model key stored, your chats' calls go out with it and never with the organization's, and if it cannot be opened the chat pauses and says so rather than using the organization's instead. If the operator has set no server key, the server answers that it cannot keep personal keys safe yet and stores nothing; chats then use the organization's key. Signing in with GitHub gives the server your GitHub access for git without storing anything yourself, unless you stored your own.

## use git in a hosted chat — clone and push through the organization's server

A hosted chat is set up so that `git clone https://github.com/<owner>/<repo>.git` and `git push` work with your GitHub access without the chat ever holding it. The commands go to the organization's server, which adds your access to each request and sends it on; only the git hosts the operator allowed (GitHub unless changed) are reached this way, and one line per git operation is kept, which you can read with `GET /v1/hosted/audit` (your own lines only).

If you have no GitHub access stored you read: `You have no GitHub access stored on this server yet. Add it, then try again.` A host the operator did not allow reads: `This server does not reach that git host for chats.` The chat itself holds only access for that one chat, which ends when the chat is stopped. It is not limited to one repository: your GitHub access decides what you can reach.
