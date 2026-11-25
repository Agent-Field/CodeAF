# Inviting people to an organization — invite by email, a team and workstreams, Send again, joining, removing someone

## invite someone — add a person to the organization by email, with a team and workstreams

**Settings › Organizations › <organization> › People in <organization>** has **Invite someone**: an email (`name@company.com`), a **Team** (or **No team**) and the **Workstreams** they join. **Send invite** makes the invite. Only the organization's admin (the person who runs its server) sees this.

An invite is a link that works for **7 days** for one person. The person lands in the team and in exactly those workstreams the moment they join, and sees no other workstream unless someone shares it with them. A workstream inside another one takes its parent's people, so inviting someone to it puts them on the parent.

Inviting the same email again adds the new team and workstreams to the same invite and makes a fresh link; it never invites them twice.

## does it send an email? — Copy an invite link when the server has no mail

When the organization's server is set up to send mail, the line under the form says **They get an email with a link. Or Copy an invite link that works for 7 days.** and **Send invite** emails it.

When it is not, nothing is emailed: the button reads **Copy an invite link**, and the link is copied for you to send them yourself. The server sends mail only when whoever runs it sets `CODEAF_SMTP_URL` (`smtp://user:password@host:587`) and `CODEAF_SMTP_FROM` before `codeaf factory serve`. There is no hosted sign-up and no account outside the organization's own server.

## who has joined — the People table, Joined, Invited · Send again, Expired

The **People** table under the form lists everyone with their **Name**, **Email**, **Team**, **Workstreams** and **State**:

- **You · admin** — you; your Workstreams read **All N**.
- **Joined 10:42** — when they joined (a date once it is not today).
- **Invited · Send again** — the invite is waiting.
- **Expired · Send again** — the 7 days ran out.

**Send again** makes a new link for 7 more days, emails it (or copies it), and the old link stops working. **Withdraw invite…** (in the row's menu) ends an invite nobody has used: they leave its workstreams and the tasks they were given come back to you. A joined person cannot be sent an invite again. An invite made here is listed only here, not again among the plain invite links.

## give a task to someone who hasn't joined yet — it waits until they join

An invited person is already one of their workstreams' people, marked **invited**, under the name or email they were invited with. You can make them a task's owner or the person who does it.

A task done by someone who has not joined **waits**. It is not failed, and nobody can start it for them: Start says "<name> has not joined yet; this waits until they do". When they join, every task they were given becomes theirs and can start.

## invited but never joined — Hand to someone else? after 3 days

If someone invited still has not joined 3 days after the invite, every open task waiting on them gets one Ask from Afi to its owner: **Hand to someone else?** ("Ana was invited three days ago and has not joined, so “Design the badge” has not started."). When the owner is the person who has not joined, whoever looks after the workstream is asked instead. The answers:

- **Hand it to me** — the task becomes yours to do.
- **Give it to Afi** — Afi does it (offered when the invited person was to do it).
- **Keep waiting for Ana** — nothing changes; you are asked again in another 3 days.

To give it to a colleague, use Hand to on the task. If they join while the Ask is open, it closes by itself and the task is theirs. A withdrawn or expired invite is asked about the same way.

## what a new teammate sees first — added you to, Got it, Handed to you

After joining through the link and signing in, a person lands on their organization's page, not on a setup conversation. At the top: **<Name> added you to <organization>** and **You are in N workstreams and the <team>. Your Afi and your chats are private to you.** **Got it** puts it away for good.

**Handed to you** lists the tasks someone gave them, each with who does it and its due date or what it needs. A task leaves the list once they open it. Nothing anyone said to their own Afi is visible to the newcomer.

## remove someone from the organization — Remove from, sign-ins ended, work offered to the admin

In the People table, a joined person's menu has **Remove from <organization>…**. Removing someone takes them off the People table, ends their sign-ins and stops their bots, takes them out of every team and every workstream, and gives the workstreams they owned to you. The tasks they owned are **offered to you to hand over**: they arrive in your **Handed to you**, marked as handed over by them, so nothing is left without an owner.

It works the same whichever way the person joined, a named invitation or an anonymous access link: they leave the People table either way, and every share others gave them on a chat or a workstream ends, so joining again later, by a new link or the old one, brings no share back. Their spent use of the link stays spent, so a link that admitted one person does not reopen for someone else. A person who joined by a link that admits several people leaves only themselves; the others stay.

## Teams — a group you add to a workstream in one step

**Teams in <organization>** lists the organization's teams with **New team**. A team is a group of people you can add to a workstream in one step. Inviting someone with a team puts them in it when they join.
