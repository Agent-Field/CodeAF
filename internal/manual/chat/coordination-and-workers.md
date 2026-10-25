# Coordination and workers

## Relay directory upgrade pauses and saved chats

A relay directory upgrade can briefly pause pairing and chat moves. Retry after the relay is ready. The upgrade refusal is `codeaf: directory upgrade is incomplete; retry shortly; saved chats are still here`. It does not delete saved chats, files, branches or the encrypted vault. The relay keeps the old directory as a backup and never makes it a second writer after the new directory takes over. A row that cannot be copied safely leaves the upgrade paused for the operator; it is never truncated to make it fit.


## Can my phone lead, be the leader or coordinate my computers?

There is no codeaf phone app yet, so a phone cannot lead yet. A phone browser's pairing page only helps approve a computer; it does not join the phone to the cluster. A joined laptop or desktop with persistent storage and sync on can lead: run `codeaf cluster lead` on that computer and keep codeaf open. `codeaf cluster status` shows its joined node and the current coordinator.

A phone would need a supported client, persistent storage, enrollment and a live connection before it could take this role. Those requirements are not a working phone capability today. Leadership coordinates metadata; it does not move a chat or run its tools.

## Joined computers, learner capacity and an outdated build

An open, paired home with sync on joins the hosted relay, or a self-hosted `codeaf relay --store <dir>`, as a durable learner. Its store lives under the home, and reopening reuses its node. Losing that store enrolls a fresh node and retires the old authority. Three anchors vote until a personal computer is explicitly asked to lead, so pairing alone never requires either laptop to stay awake. Joining starts while the paired home is open, or when you ask for `codeaf cluster status`.

If joining has no new data to apply, the client keeps its store for the next retry round instead of polling continuously. A status activation can say `this computer is still joining the cluster; retry shortly; your saved chats are still here`.

The membership protocol admits at most 32 live learners and retains retired records instead of reusing their authority. A protocol mismatch says `this codeaf build must be updated before it can join this cluster; your saved chats are still here`. Its capacity refusal is `this cluster has reached its enrollment limit; remove a joined computer or ask the relay operator for capacity`. A worker setup command is not available through the production client yet.

## What is cluster lead — what does codeaf cluster lead do, make this computer the leader

`codeaf cluster status` on the hosted relay, or a self-hosted `codeaf relay` with storage, reports the committed voters and current leader after a fresh majority read. A paired computer joins with persistent storage as a learner before status shows its node. Readiness requires its own durable application through the advertised join position. Status names `this computer`, `learning computers`, `ready computers` and `voting computers` when known. `codeaf cluster lead` on an unjoined computer says `this computer must join the cluster with persistent storage before it can lead`.

On the hosted relay or a self-hosted `codeaf relay` with storage, run `codeaf cluster lead` on the joined computer that should coordinate. It first catches up, replaces the third anchor’s vote through a joint change, durably finishes that change with the third anchor still learning, and transfers leadership. The voters are then two anchors and this computer. Keep codeaf open to keep its member running; a command used with no open terminal runs its member only for that command. Leadership moves coordination metadata, never chat content or chat ownership.

Do not treat pairing or a saved leadership request as a successful transfer. Success requires the selected computer to be elected, commit its current term and pass a fresh majority read; its receipt names the computer, term, revision and read barrier. Retrying `codeaf cluster lead` preserves an already saved signed request. Continue chats through the ordinary continue door.

Three voters tolerate one unavailable member. Losing a majority pauses coordination; saved chats remain. The refusal is `this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here`.

## Hosted relay while this computer leads

The hosted relay keeps serving directory requests, watches, pairing decisions and worker assignments while this computer leads. It routes signed coordination requests through the computer’s outbound connection and waits for its own durable copy before replying. `codeaf cluster status` on either joined home reports the elected computer and the same three voters.

## Pending leadership request, retry and refusal

A lost reply or restarted home keeps the original signed leadership request. Retry `codeaf cluster lead` to resume it; a pending request cannot choose a different computer. A completed refusal says `the leadership request could not complete; refresh cluster status and try again` and is acknowledged before another request is saved. Only one configuration change can run at a time. A different personal coordinator cannot directly replace an active personal coordinator; let the anchors resume, then request leadership again.

## What happens if my leader goes away — my leading laptop went to sleep or the leader computer was turned off

On the hosted relay and a self-hosted relay with storage, when a personal coordinator sleeps, exits or turns sync off, the two remaining anchor voters can elect a coordinator and restore the third anchor after it catches up. The missing computer need not approve its removal. A returning computer catches up as a learner and does not automatically regain leadership: request `codeaf cluster lead` again. Losing a majority pauses coordination and keeps saved chats. The refusal is `this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here`.

Coordination ticks every 5 seconds. The provisioned anchors’ ranked election windows are 6–7 ticks (30–35 seconds), 8–9 ticks (40–45 seconds) and 10–11 ticks (50–55 seconds); other computers use 12–13 ticks (60–65 seconds). Healthy fallback targets about 65 seconds. Scheduling delays or a partition can take longer. Fully idle groups stop periodic ticks only after every voter has applied the same idle barrier; a new operation or a leading computer’s disconnection wakes coordination and needs a fresh majority response.

## Coordinator and anchor both unavailable, lost majority

While a personal computer leads, the voters are that computer and two anchors. Losing that computer and either voting anchor leaves no majority, even though the third anchor retains its learning copy. The response is `this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here`. codeaf preserves the existing cluster and saved work; it cannot make a writable minority or remove voters to manufacture a majority.


## Does an open connection keep my chat running on this computer?

The sync connection carries presence hints and committed lease acknowledgments. An open socket or an answered ping alone cannot keep ownership of a chat. codeaf skips an ordinary heartbeat only while it has a recent acknowledgment for that exact chat and ownership number, with enough time left before expiry. Without that acknowledgment it keeps sending ordinary heartbeats. A delayed acknowledgment for an earlier owner cannot take the chat back, and a delayed renewal cannot undo a clean quit.

## Does syncing keep sending requests when nothing is happening?

A home that has never paired sends no sync traffic. `CODEAF_SYNC_URL=off` also blocks new sync requests from clients that were opened before the setting changed. Presence pings on an open view are hints; they do not write chat ownership. On the hosted relay and the native relay, an open idle home uses its learner’s applied vault pointer to notice shared memory and preference changes. Local edits are detected locally and pushed on the next state round. Over ten idle minutes, caught-up open homes make zero HTTP requests, zero peer or forwarding frames and zero client attempts, including while one home leads and after it sleeps and the anchors resume. A caught-up idle home sends no recurring state polls or peer keepalives. Home also follows its local applied directory for display changes; its roster and backstop read that same local hint. Quiet neighbours’ presence can age out without forcing a network ping. Continuing a chat and all ownership decisions still use fresh majority reads. A committed change wakes peer delivery and the next state round pulls the changed vault. If an older hosted relay has no learner door, the home keeps its existing five-second state polling. On the consensus directory, a group with no work or held chats can stop its periodic coordination timer after an idle barrier has reached every voter. The first operation wakes coordination and must obtain a fresh majority response. A saved leader address cannot answer that operation by itself. Losing the majority leaves syncing unavailable and keeps local work intact.

## Hosted idle presence sweeps and sleeping connections

With its three anchors voting, or with a joined computer leading, a hosted group with no work or held chats stops its maintenance alarm after every voter applies the idle barrier. Its three anchors then schedule no recurring alarms. Reconstructing the sleeping coordinator leaves its learner and forwarding sockets open; a presence sweep neither wakes the anchors nor reconnects those computers. The first real request obtains a fresh majority response before answering.

A compatibility events viewer pings every 10 seconds. The platform answers without running coordination. While somebody watches presence, the relay checks at the next 25-second silence deadline so a stopped computer appears offline within 30 seconds. With two viewers pinging together, the simulated ten-minute measurement uses 29 short sweeps; those sweeps make no member calls or data writes. With no events viewer, no presence sweep recurs. Home follows the local learner once joined, so its ordinary display needs no remote presence viewer.

A connection naming held chats still renews through committed changes every 30 seconds while fresh hold evidence is available. Presence alone never renews a chat.

## What can a worker do — can a worker read all my chats or approve pairing?

A delegated worker uses its own key and an expiring certificate naming selected chats. It receives only sealed grants for those chats, rather than the full exported identity. It cannot approve pairing, rotate the identity, read the identity vault, enroll other devices or request leadership. Richer capabilities, worker labels and execution credentials stay encrypted; only the existing platform and sandbox capability shape is readable by the relay.

Worker setup and assignment execution are not available as a terminal command yet. Registration and a fresh signed heartbeat only record availability; they never start tools or give the worker ownership of a chat. Removing a worker blocks later requests after that removal commits. Expiry and removal cannot take back bytes the worker already downloaded.

## Worker restarts, draining and learner metadata visibility

A worker restart registers a newer process number. Delayed requests and heartbeats from its previous process cannot refresh availability or save work. A drained worker cannot receive new claims; already held work can still save and let go. Restarting does not clear draining. Expired and removed enrollment records remain retained; reaching their capacity limit refuses new enrollment instead of deleting records or saved chats.

A scoped API worker does not join the coordination log. Joining as a learner requires explicit consent in its certificate: the full log reveals identity-wide routing and ownership metadata, while chat content, names and task instructions remain encrypted. Use scoped API access for an untrusted third-party worker. A delegated learner gets no vote and cannot become leader. The production client does not yet activate delegated learners; selected API access is available without joining the log. Existing paired personal computers keep their shared-root trust model.


## How does a worker claim an assignment or run parallel tasks?

The coordination service accepts an explicit selected chat and target worker. Instructions and credentials stay encrypted. Registration and availability alone cannot start work: the worker first downloads the saved chat, then commits a conditional claim for the assigned chat's observed holder and fence. Continuing that same observed chat elsewhere competes with the claim; only one can win. A changed assignment, expired delegation, drained worker or newer worker process can refuse the claim. A claim returns the committed head, which the worker finishes downloading before opening it.

Parallel tasks use distinct child chats, each with its own holder. Finishing a child records its already published result; incorporating that result into the parent still requires the parent's current holder and fence. The worker uploads the complete saved result before publishing its head. Leadership needs neither those files nor their keys.

Worker setup and assignment execution are not available as a terminal command yet. The internal worker adapter supports a small scripted worker; phone controls and creating remote machines are not available yet.

## Will an interrupted worker replay tools after a crash or reassignment?

No. The worker saves its claim request before sending it and records an execution intent before invoking tools. A lost coordination reply can retry that same request, retaining its original fence. Once execution may have started, reopening keeps the records and reports `this work may be incomplete; its saved records have been kept`. It never invokes that attempt again automatically. A recorded finish can retry its coordination acknowledgment without running tools.

Reassignment changes the run revision and requires a new conditional claim. The replacement can recover saved records, but the scripted adapter refuses automatic execution of a reassigned run. Missing or incomplete calls are not instructions to execute them. Cancellation prevents future claims; stopping a claimed assignment does not cancel an already running shell process. Another take changes the fence and blocks the displaced holder from publishing. Already downloaded bytes and uploaded frames stay retained.

## Why can an older computer list chats but not continue a chat that is still running?

Older builds can still list, save, publish and release chats through a compatible relay. A relay using quorum-backed coordination refuses an older request that tries to take a live chat without naming its observed holder and ownership number. It uses the existing refusal, `another device continued this chat first`; an old build does not necessarily show an extra update hint. Quit on the computer holding the chat before continuing it elsewhere, or update codeaf on the computer trying to continue. A released or expired chat can still be taken. A conditional choice never silently changes to a newer holder.


## Hosting a relay with persistent storage

`codeaf relay --store <dir>` serves pairing, encrypted files and the chat directory
with one command. Each identity uses three local persistent coordination copies,
with separate databases under `<dir>/raft/`. Changes and authority reads require
a fresh majority response. All three copies still depend on this computer and
its disk; this setup does not survive losing the relay host. Without `--store`,
the relay is only a transport and pairing mailbox.

On first use, an existing directory is frozen, copied into the three stores and
checked through a majority before requests resume. The original directory stays
as a frozen backup under `<dir>/directory/`. An interrupted upgrade resumes with
the same identity and stores; an incomplete or damaged import refuses requests
instead of reopening the old writer. Keep the whole store directory when
restarting or backing up the relay. Chat content stays encrypted in the existing
blob store. No switch selects the former directory writer.

Legal directory requests from older builds up to 1 MiB keep their original HTTP
limit on both relays. Large signed metadata is staged in bounded pieces and becomes
visible atomically, after the complete original proof and current authority are
checked. Interrupted requests can recover without exposing partial changes. All
frame hints, sealed names and key wraps are preserved; over 1 MiB is refused. The
identity’s storage capacity still applies, and existing records are never removed
to make room.

## Hosted relay directory upgrade and older clients

The hosted relay records directory changes on three persistent members. Two must
agree before a take, publish or fresh listing succeeds. Pairing one or two
computers does not require either computer to stay awake. The three hosted copies
still depend on the same hosting provider and relay connection.

An existing identity upgrades on first authenticated use. An interrupted upgrade
can briefly ask you to retry; it keeps the saved directory and encrypted frames.
The old directory never becomes a second writer. Older clients keep pairing,
listing, publishing, releasing and continuing a chat they observed. A very old
client that blindly forces a live chat receives `another device continued this chat first`.
Quit on the holder first, or update that client before continuing a live chat.
Watch sockets show presence; older clients still send their normal heartbeats.


## Scoped worker requests and committed watch receipts

The relay's selected-worker API accepts an original signed command or a signed request for one chat or assignment. A delegated worker receives only its selected sealed grant. Each read commits current time, waits for a fresh majority response and checks its current scope and process number. A retained claim alone cannot authorize execution after another computer takes the chat. A worker cannot use this API to pair computers, rotate the identity or promote itself.

Native and hosted watch connections carry committed receipts to clients that ask for them. Each receipt names the exact chat and ownership number, durable position and hold-until time. Older clients receive their original version and presence frames and keep ordinary heartbeats. A ping without a receipt cannot suppress those heartbeats. A released chat cannot be revived by a delayed ping. Idle presence pings do not start a recurring directory request loop.
