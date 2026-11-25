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

You sign in first. Set `CODEAF_ORG_TOKEN`, or run `codeaf attach <url> --token <token>` once for that server. The token is the `access_token` that joining or signing in answers with: see *join or sign in to an organization from a terminal*. Without either you read: `Sign in to <host> first: set CODEAF_ORG_TOKEN, or run codeaf attach <url> --token <token>.` The address must be `https://`; a plain `http://` address is accepted only for the computer you are on, because a sign-in must not cross a network in the clear.

The chat's folder is chosen on the server, so `--org` cannot take `--no-compact`, `--yolo`, `--max-hours`, `--max-cost`, `--one-model` or `--no-host`, nor be combined with `--host` or `--at`. It says which one cannot travel and why.

## join or sign in to an organization from a terminal — where the sign-in token for `codeaf chat --org` comes from, with no desktop app

The token `--org` wants (`CODEAF_ORG_TOKEN`, or `codeaf attach <url> --token <token>`) is the `access_token` the organization's server answers with when you join or sign in. The desktop app asks for it for you; from a terminal you ask yourself, with `curl`, at the organization's `https://` address (`ORG` below):

- **Join with an invite link** (from the operator; one person, seven days): `curl -sS $ORG/v1/factory-access -H 'Content-Type: application/json' -d '{"verb":"sign_up","link":"<the invite link>","username":"you","password":"<12 or more characters>","name":"Your Name"}'`. The answer says `"state":"joined"` and holds `"access_token"`.
- **Sign in again**: `curl -sS $ORG/v1/auth/password -H 'Content-Type: application/json' -d '{"username":"you","password":"..."}'` answers with a new `access_token`.

A refusal comes back as `200` with `"state":"ready"` and a `message`, such as `A password cannot contain your username.`; a link already used says `"state":"used_up"`. A password is taken only over `https://` (or from the server's own machine, which a Docker setup does not count as its own). The token ends after 30 days without use, and after 90 days at most; keep it like a password. `docs/organizations.md`, "From a terminal", has the operator's side: claiming the organization and making an invite link.

## is my code on the organization's server when I use --org — what stays on your computer and what does not

Everything the chat does happens on the server, in a folder that belongs to that one chat: the files it makes, the repositories it clones, what it writes to its shell history. None of it is copied to your computer unless you ask for it. Nothing on your computer is sent to the server except what you type and attach in the chat.

The chat does not see your computer's files, your keys or your other chats. It starts in an empty folder. To work on code, it clones or is given the code there.

The organization's operator keeps the machine and its disk. See *what can the operator of the server see* for what that means for you.

## can other people on the server see my hosted chats — private by default, shared only when you share it, and anyone else gets `There is no such chat.`

A hosted chat is private to the person who made it, unless the organization's policy makes chats of this kind start otherwise (the policy's default for `session`). A private chat is left out of every other person's list and out of the organization's live updates: nobody else is told it exists, who owns it or what state it is in, and that includes the operator unless the policy says the operator sees private things (see *can the operator open my hosted chat*). Someone else who asks for it, to open it, list it, stop it or delete it, gets the same answer as for a chat that was never made, `There is no such chat.`

You can share a chat with a person, a team or the whole organization the way you share anything else (`POST /v1/resources/session/<chat>/grants` with who and a role, such as `{"subject":{"kind":"person","id":"<their username>"},"role":"view"}`; see *Sharing*). Once you do, it shows up in that person's `shared` list. Taking the access away takes it off their list again, and cuts them off if they are watching it at that moment: see *what can someone I shared my hosted chat with do*.

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

Everything else on the chat's wire stays with the owner whatever role a person holds: approving a command, changing a setting, opening the chat's files, stopping its work. A command that needs approval waits for the owner, unless the organization's policy says otherwise: see *can someone I shared my hosted chat with approve a command*. Stopping or deleting the chat itself is a separate door that needs the `manage` permission, which the `full` role holds: see *stop or delete a chat on the server*.

## what happens when I give someone run access to my hosted chat — they act with my GitHub identity, files and spending limits

**A person you let send messages acts with your GitHub identity, your files and your spending limits.** The chat is yours: its commands run in your folder on the server, as your user, with your model access, and its git goes through your GitHub access, whoever typed the message. What it costs is yours too (see *who pays*), against your chat limit and month. The server notes who asked on each message, on each charge and on each use of your GitHub access, so you can read it afterwards. If you would rather nobody else sends messages, share at `view` or `comment`, which do not hold `run`, or ask the operator to take `run` out of the roles in the organization's policy: nothing else needs to change.

## why was I refused in a shared hosted chat — the sentences, the keyboard and a removed share

You read `This chat is shared with you. Only its owner can do that.` for what only the owner can do. Sending without the permission reads `You can watch this chat, but you were not given permission to send messages in it. Ask its owner.`, steering without it `You can watch this chat, but you were not given permission to steer it. Ask its owner.`, and answering without it `You can watch this chat, but you were not given permission to answer its questions. Ask its owner.`

Only one window types at a time. While the owner has the chat open with the keyboard, a message or a redirect from someone else is refused by the chat with its own sentence about the keyboard; taking the keyboard (a person who may send can) moves it to their window and tells the owner's, interrupting and answering are not affected, and a message goes through once the owner has closed their window.

When the owner takes your access away, or the policy changes so you no longer have it, your window is ended within a few seconds (five unless the operator set another heartbeat) and reads `Your access to this chat was removed.`

Opening a chat that is not running starts it for its owner, which can be refused while its owner or the machine is running as many chats as are allowed. Then you read `This chat is not running, and there is no room to start it right now. Try again later.` If the organization's policy keeps you from starting a chat, you read `This chat is not running. Starting it takes permission to send messages in it. Ask its owner.` or `This chat is not running. Only its owner can start it. Ask them to open it.`: see *who may start a hosted chat that is not running*.

## can someone I shared my hosted chat with approve a command — the owner by default, and the policy setting that lets people who answer allow one

By default no. A command that needs approval under your approval mode waits for you, whatever role the person holds, and that is true however their window sends the answer: if they try, they read `This chat is shared with you. Only its owner can do that.` and the chat never hears it. Answering the chat's other questions is not affected: a person with the `answer` permission still answers those.

The operator can change that in the organization's policy. `PUT /v1/org/policy` takes `"chats": {"approvals_by": "owner"}`, which is the default and what an organization with no setting has, or `"chats": {"approvals_by": "answer"}`. With `answer`, a person who holds the `answer` permission (the `edit` and `full` roles do) can allow one command once, or refuse it. Allowing a tool for the rest of the chat, putting the approval away and the chat's own approval setting stay with you, and a person who does not hold `answer` still reads that they were not given permission to answer. What they allow runs as you, in your folder, with your GitHub identity and your limits, and the server notes who answered next to what it charges. A change reaches a window that is already open within a few seconds. Any other word is refused when the policy is saved. The desktop app has no screen for this setting yet.

## who may start a hosted chat that is not running — anyone who can see it by default, and the policy setting that limits it

Opening a shared chat that is not running starts it, for its owner and in the owner's place among the chats they may have running. By default anyone who can see the chat does that, including a person with only `view` who runs `codeaf chat --org <server> --session hs_... --once`: it starts the chat in one-shot mode, their own message is refused, and the owner's window then reads `this conversation is already open in a different interactive or headless mode; close it before retrying` until the chat is stopped or stops itself when idle.

The operator can change that in the organization's policy. `PUT /v1/org/policy` takes `"chats": {"wake_by": "view"}`, which is the default and what an organization with no setting has, `"chats": {"wake_by": "run"}` or `"chats": {"wake_by": "owner"}`. With `run`, only a person who holds the `run` permission starts it; anyone else reads `This chat is not running. Starting it takes permission to send messages in it. Ask its owner.` With `owner`, only its owner does, and everyone else reads `This chat is not running. Only its owner can start it. Ask them to open it.` Nothing is started in either case. A chat that is already running opens for everyone who can see it, under every setting, and the owner's own window is never held back. The rule is checked when a window opens. Any other word is refused when the policy is saved. The desktop app has no screen for this setting yet.

## who pays for a shared chat — when someone else is in my hosted chat, it runs as the owner, with the owner's access and limits

A shared chat always runs in the owner's folder on the machine, with the owner's model access and the owner's spending limits: what a guest's messages cost is counted to the owner, in the owner's My space, against the owner's chat limit and month. The server notes who last put words into the chat next to each charge, so the owner can see it was a guest, but that is approximate: a model call is credited to whoever last sent a message, redirected a turn or answered through the server, and the owner opening their own window puts it back to the owner. The owner's activity list (`GET /v1/hosted/audit`) holds one line for each message a guest sent, with who sent it (never what it said), and each line for the owner's GitHub access carries who was asking at the time.

## can the operator open my hosted chat — only metadata, and only if the policy makes the chat visible

By default the operator holds no right on a private chat: it is refused with `There is no such chat.` and is not told the chat exists. When the policy makes new chats not private, or says the operator sees private things, the operator's list shows the chat's identifier, owner, state and times and nothing else, and the operator still cannot open it, read its history or see its folder. Whoever runs the machine itself is another matter: see *what can the operator of the server see*.

## bots and hosted chats — what a bot may do with a chat, and what it may not, such as whether a bot with view scope can start a hosted chat

A bot can start a hosted chat if the organization's policy lets agents create things and the bot's scope holds `run`. By default, `edit` and `full` hold `run`, and `view` and `comment` leave it out. A bot without `run` reads `This bot's scope does not let it run chats, so it cannot start one. Ask the person who made it.` The server makes no chat. A chat the bot starts belongs to the person who made the bot and is recorded as made by the bot.

A bot can list, open, stop and delete hosted chats within the access its creator holds, narrowed by its scope. If the creator loses access to someone else's chat, the bot loses it on its next request, and the server closes the bot's window within a heartbeat. A bot cannot touch a person's stored keys (`/v1/hosted/keys`), which answers `a bot cannot use this yet`. A bot that holds `run` on its creator's own chat can type into it as its creator would. A bot whose scope leaves `run` out is treated like a guest.

## may I still start a chat on my own computer after joining an organization — the hosting policy

An organization is its own account, separate from your personal one: you switch between them, and what you make in the organization's account belongs to the organization. When the organization runs hosted chats, every chat in its account is a hosted chat, and a chat does not start on your computer from inside it. There is no setting on your side to override that; the way to work on your own computer is to switch to your personal account, which does not see the organization's chats or files.

The organization's policy has a `hosting` section for this. `local_sessions` is `allow`, `warn` or `refuse`; left empty it means `refuse` when the organization runs hosted chats and `allow` when it does not, so an organization with no machine for chats behaves as before. `GET /v1/hosted/sessions` reports the value that applies as `hosting.local_sessions`, with the words to show for it: for `refuse` `This organization runs your chats on its own server, so they do not start on this computer. To work here instead, switch to your personal account, which does not see the organization's chats or files.` and for `warn` `This organization keeps your chats on its own server. A chat started on this computer is not part of the organization's account.` Only the operator changes the policy, with `PUT /v1/org/policy`. `max_session_cost_usd` is what one hosted chat may spend before it is paused; zero keeps `CODEAF_HOSTED_SESSION_CAP_USD` (five dollars unless set). This version reports the setting and enforces the cost limit; `codeaf` on a computer does not yet act on `local_sessions`.

## what can the operator of the server see — honest limits of a hosted chat

Whoever runs the machine, and whoever is its administrator on the hosting platform, can read everything on its disk. A hosted chat is not private from them. The disk is not encrypted by the server software itself.

The chat can reach the internet. The sandbox allows TCP connections on the ports the operator allowed (443, 80, 22 and 9418 by default) and the ports used for the organization's own server address. Other TCP ports fail with `permission denied`, even for a service the chat itself started. Any reachable host on an allowed port can answer, including internal services and a cloud metadata address such as `169.254.169.254`. See *can a hosted chat reach the cloud metadata address* for what the operator should block.

A chat has no key of the organization's, no key of yours and no GitHub access of yours. It reaches a model only through the organization's server, with access made for that chat alone, and reaches git only through the same server, which adds your GitHub access to each request. That access ends after seven days or at once when you stop or delete the chat or the operator removes you from the organization. If the chat machine answers during the removal, the server deletes your chats. A restart of the organization's server leaves a running chat's valid access in place.

If you do not want your code on a machine the organization runs, do not open it with `--org`.

## can a hosted chat reach the cloud metadata address or other services on my network — the limit is by port, not by address

The Linux kernel's Landlock fence filters TCP connections by port. It allows 443, 80, 22 and 9418 by default (`CODEAF_SESSIONHOST_CONNECT_PORTS`) and the ports used for the organization server's own address. An allowed port can reach any host the chat's machine can reach, including services on the private network, the Docker host at the container network's gateway, and cloud metadata at `169.254.169.254` or AWS ECS task credentials at `169.254.170.2`. UDP is unfiltered. A command that reaches a cloud metadata service can read the machine's cloud credentials. The chat gets a short fixed environment, so the container's cloud credential variables do not pass into it. Text in a repository or web page can make the chat's agent run a command too.

The port fence worked on a flat test network, where the metadata address did not answer, so reachability on a cloud machine is untested. When the sessionhost starts it connects to those addresses, sends nothing, and logs one warning line naming any that answer.

The operator should block `169.254.0.0/16` and, on AWS, `fd00:ec2::254` at the host or VPC firewall for traffic from the machine that runs chats. On AWS also require IMDSv2 and set the hop limit to 1. Give the stack its own network, firewall the Docker bridge from host services, and keep internal services off allowed ports such as 80 and 443 or give each its own sign-in. On Railway, put codeaf in its own project and keep other services out of that environment. See `deploy/org/README.md`, *Known limits*, for the operator's firewall and cloud identity guardrails. codeaf has no address filter built in.

## is there a disk or memory limit for each hosted chat — what happens to a person's chats when I remove them while the chat machine is down, and seccomp

A hosted chat has no total disk or memory quota. Its Linux user is limited to 512 processes, and commands are limited to 4096 open files and 10 GiB per file, with no core dumps. A 200 MB write and a 256 MB allocation both succeeded in testing. One chat can fill the sessions volume or the machine's memory and slow or stop others. `CODEAF_SESSIONHOST_MAX_RUNNING` and `CODEAF_SESSIONHOST_MAX_PER_PERSON` default to 20 and 3. Bound the `sessionhost` service with memory, process and CPU limits, a fixed-size volume and a disk alert.

Removing someone immediately ends their chats' model and git access and clears their stored keys and GitHub access. If the chat machine does not answer, removal still succeeds. The server ends the remaining chats' shares and makes them private so every route answers `There is no such chat.` Their Linux users and folders stay on the machine for now, and the server finishes the deletion by itself: it keeps a record that the removal is still owed, tries again with a longer wait each time (up to five minutes) until the machine confirms, and picks the work up again if the server restarts. Until then nobody can wake those chats or give them model access. A chat the same handle starts after being removed and added again is not touched. The operator does not need to remove the person a second time.

The sessionhost relies on the container runtime's default seccomp profile to block user namespaces and calls such as `mount` and `chroot`. It sets no-new-privileges and drops every capability from chat processes. Never use `seccomp=unconfined` or a profile looser than Docker's default. Check that your platform applies equivalent restrictions. With a looser profile, testing showed user namespaces and user ID maps allowed, which exposes more of the kernel to chat commands. Landlock and the chat's Linux user still apply inside those namespaces by design. That was untested.

Testing used no front proxy, so request framing and WebSocket upgrades through one (Railway's edge, nginx) are untested. The test stub model had no price, so the priced-model path was read in the code and not run end to end.

## what happens to a hosted chat when I disconnect — the turn goes on and I come back to it

A turn that is running on the server when you close its desktop window, disconnect, or quit `codeaf chat --org` goes on running there. Reopen the chat in the organization window or with `codeaf chat --org` to read the result. Stop in the desktop chat interrupts the current turn. The server's stop action below ends the chat's processes.

An idle chat stops its own program after a while and starts it again, with its conversation, the next time you open it. Files stay in place the whole time. Processes you started that were still running stay running until you stop the chat.

If the machine that runs chats restarts, its running processes end and its files stay. Your first attach afterwards works: the organization's server notices the machine lost your chat's model access and hands over new access before anything is sent. If the organization's server restarts instead, the chats keep working and so does the access they hold.

## how many chats can I have on the server at once — limits and waiting

The operator sets how many chats may run at once, and how many one person may have running. When you are over your own limit you read: `You already have 2 chats running; stop one first.` (the number is the operator's). When the whole server is full you read: `Waiting for a free slot (3 of 3 in use).` Try again in a moment, or stop a chat you are not using.

A chat counts as running while any process of it is running, including a background one you started. Stopping a chat ends all of them.

## stop or delete a chat on the server — and what is left behind

Stopping a chat ends every process it started, including ones that detached themselves, and takes away its model access. Its own folder and files stay, and opening the chat again starts it again.

Each stop, delete or start clears what the chat's Linux user left in `/tmp`, `/dev/shm` and `/var/tmp` and in System V shared memory, message queues and semaphores. These areas are shared by every chat on the machine. The cleanup keeps a later chat that gets the same Linux user from finding the leftovers. Deleting a chat also removes its own folder. Afterwards the server answers `There is no such chat.`

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

Only if you shared the chat with a role that holds `run` (the `edit` and `full` roles do), and even then one window writes at a time. While you have the chat open with the keyboard, a message or a redirect from your teammate is refused by the chat with its own sentence about the keyboard. It goes through once you close your window, or after they take the keyboard (a person who may send can), which tells your window. Interrupting a turn and answering a question are not held up. Whatever they send runs as you: in your folder, with your GitHub identity and your spending limits, and a command that needs approval waits for you, unless the organization's policy lets people who answer allow one (see *can someone I shared my hosted chat with approve a command*).

## the organization's server is down or my sign-in ended — a hosted chat never runs on my computer instead

If the server cannot be reached, the desktop window says `<host> is unreachable.` once, with **Try again**, and the chat stays where it is. If your sign-in is missing or has ended it says `Sign in to <host> first.` In neither case does the app start a chat on your computer in its place: a hosted chat never falls back to running locally. A turn that was already running on the server goes on without you, and the conversation is there when the server answers again.

## turn on chats on the server — for the operator

Hosted chats are off by default. The operator turns them on by running the chats' machine beside the organization's server, in the organization's deployment (`deploy/org/README.md`, "Hosted chats"), and giving both the same long secret, which `./setup.sh` makes.

On the organization's server set `CODEAF_HOSTED_URL` to the chats' machine, `CODEAF_HOSTED_SECRET`, `CODEAF_HOSTED_MODEL_UPSTREAM` (the model endpoint the organization pays for; OpenRouter's when empty) and `CODEAF_HOSTED_MODEL_KEY` (or, instead, an organization key an operator saves in Settings, Organizations; the `OPENROUTER_API_KEY` variable is not what the model door uses). Set `CODEAF_SERVER_KEY` too (64 hexadecimal characters; `./setup.sh` makes it): people's own keys are sealed with it, and a server that holds keys sealed with another one, or with none, refuses to start instead of carrying on. Optional: `CODEAF_HOSTED_SESSION_CAP_USD` (what one chat may spend, default 5) and `CODEAF_HOSTED_GIT_HOSTS` (the git hosts chats may reach, default `https://github.com`). On the chats' machine set `CODEAF_SESSIONHOST_SECRET` to the same secret, and optionally `CODEAF_SESSIONHOST_MAX_RUNNING`, `CODEAF_SESSIONHOST_MAX_PER_PERSON`, `CODEAF_SESSIONHOST_IDLE`, `CODEAF_SESSIONHOST_MODEL` and `CODEAF_SESSIONHOST_CONNECT_PORTS` (the TCP ports commands may connect to, default `443,80,22,9418`).

`codeaf sessionhost probe` prints what the machine can do and exits non-zero when it cannot keep chats apart. The machine runs as root inside its container and keeps only the capabilities it needs to switch users; it holds no organization secret except the shared one and no model key. The key stays on the organization's server, which adds it to each model request.

## what does a hosted chat cost and who pays — the organization's key, the spending limit and the monthly limit, and why my chat says it is already making as many requests as it may at once

A hosted chat calls its model through the organization's server with the organization's key unless you stored your own. Calls are counted to the cent in your My space, and the operator sees only totals. At the chat's spending limit (five dollars by default), you read `This chat has reached its spending limit of $5.00, so it is paused. Start a new chat to carry on.` At your monthly limit (twenty dollars by default), you read `You have reached your monthly limit of $20.00, so this chat is paused. The organization's operator can raise it in Settings.`

A refused message makes no model call and costs nothing. Calls with your own key count against both limits. If the provider refuses the organization's key, the chat pauses without retrying and you read `The organization's model access was refused by its provider, so this chat paused. Ask the organization's operator to check the model key.` If it refuses your own key, you read `Your own model key was refused by its provider, so this chat paused. Replace it or remove it, then send your message again.` The chat never falls back to the organization's key.

A hosted chat can make up to four model calls at once, subject to what the calls already running hold against its spending limit. A priced call holds its estimated cost, and an unpriced call holds half of the limit that was left when the chat's model connection opened (never more than what is left), so once the chat has spent anything a second unpriced call is refused while the first is running, and once half the limit is spent only one such call can run at a time. A fifth request or one that cannot fit because of what is held never reaches the model and costs nothing, and you read `This chat is already making as many requests as it may at once. Try again in a moment.`

## keep my own model key or GitHub access on the organization's server — stored sealed, only yours

You can give the organization's server your own OpenRouter key and your own GitHub access (a token) for your chats. They go to `PUT /v1/hosted/keys/openrouter` and `PUT /v1/hosted/keys/github` with your sign-in and the body `{"key":"..."}`; `GET /v1/hosted/keys` lists what you stored as the provider, the last four characters and when, never the key, and `DELETE /v1/hosted/keys/<provider>` removes one. codeaf's own chat has no screen for it yet.

Each person's keys are sealed on the server's disk under a key of that person's own, which is itself sealed by the server key the operator set; another person cannot read them, and neither can a chat. With your own model key stored, your chats' calls go out with it and never with the organization's, and if it cannot be opened the chat pauses and says so rather than using the organization's instead. If the operator has set no server key, the server answers that it cannot keep personal keys safe yet and stores nothing; chats then use the organization's key. Signing in with GitHub gives the server your GitHub access for git without storing anything yourself, unless you stored your own.

## use git in a hosted chat — clone and push through the organization's server

A hosted chat is set up so that `git clone https://github.com/<owner>/<repo>.git` and `git push` work with your GitHub access without the chat ever holding it. The commands go to the organization's server, which adds your access to each request and sends it on; only the git hosts the operator allowed (GitHub unless changed) are reached this way, and one line per git operation is kept, which you can read with `GET /v1/hosted/audit` (your own lines only).

If you have no GitHub access stored you read: `You have no GitHub access stored on this server yet. Add it, then try again.` A host the operator did not allow reads: `This server does not reach that git host for chats.` The chat itself holds access for that one chat. It ends after seven days or at once if you stop or delete the chat or the operator removes you from the organization. Your GitHub access decides which repositories you can reach.
