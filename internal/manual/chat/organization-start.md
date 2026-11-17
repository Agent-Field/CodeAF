# Starting an organization and Afi's set-up

## start your organization — create an organization, name it, what it does in a line

There is no hosted account and no plan to pick. An organization is a server someone runs with `codeaf factory serve`; you sign in to it in the desktop app (**Sign in to WorkAF**: a passkey first, then a password, then GitHub, as the server takes them).

The first time the operator signs in to an organization nobody has set up, the app opens **Start your organization**:

- **Start an organization**, "For your company or team. You invite people next.", with a **Name** and **What it does, in a line. Afi reads this.** The button is **Create** and the name. Under it: "Nothing is shared until you invite someone."
- **Join one**, "Paste an invite link someone sent you.", which opens the sign-in page for a link.
- **Just me, on this computer**, a quiet link back to Personal.

The name and the line are kept on the organization's server. Every Afi in the organization (the set-up conversation and each workstream's agent) is told them, so Afi knows what the organization does. Someone who is not the operator never sees this page; they land on Work.

## Afi sets up the organization — one card, workstreams, teams and invites in one press

After **Create**, Afi asks: "What is on your plate this month? Tell me the way you would tell a new colleague." Answer in a few sentences, with the people and their emails if you have them.

Afi answers with one card, made with its tool `org_propose_setup`:

- **Workstreams**: each with what it is for, who is in it, and the workstreams inside it. A piece of work by the same people goes inside its area (Launch 0.9 inside Marketing); different people make a separate workstream.
- **People to invite**: each with an email (Afi never invents one; type it in the empty field) and a team, such as an Engineering team for two engineers.

Untick a workstream, a workstream inside one, or a person; remove someone from a workstream; or tell Afi what to change, and it proposes the whole card again. "Each person sees only the workstreams they are in."

NOTHING IS MADE AND NOBODY IS INVITED UNTIL YOU PRESS THE BUTTON. It counts what it makes, for example **Create 5 workstreams and invite 3 people**, and makes the teams, then the workstreams, then the ones inside them, then the invites, with each invite carrying its team and workstreams. Anyone the server did not email (no address, or a server that sends no mail) gets an invite link instead: when it is done, the page lists them with **Copy link** for each, then **Open Work**. With no one like that, the tab becomes **Work** at once.

Only the organization's operator has this conversation, and only while the organization has no workstreams.

## set-up failed — a part could not be made, press again

If a part fails, the card says which and why, for example "Hiring could not be made: … Press again to make what is left." The button then reads **Make the rest**. Pressing again makes only what is still missing: nothing is made twice, and nobody is invited twice.

## set up by hand — skip Afi's set-up

**Set up by hand**, at the top of the set-up page, skips Afi's card. Nothing is made; the organization is marked set up and the tab opens a new, empty workstream to start from. **Work** and **People** work as usual.

## Afi can't talk on the set-up page — no model key

Afi's set-up runs on the organization's server, with the organization's model key. Without one the page says: "Afi needs the organization's model key first. Add one in Settings › Organizations, or set it up by hand."

## still to set up — connect a repository, add what Afi should know

An organization's **Work** page starts with **Still to set up** while something is missing:

- **Connect a repository** · "The factory needs one before it can fix or build anything." · **Connect GitHub**, which opens the organization's Repos page.
- **Add what Afi should know** · "A brief, pricing, your brand voice. They go in the Library and every workstream can read them." · **Add files**, which opens the organization's Library. Only where the server keeps a Library.

Each row goes when it is done, and the block goes when both are.
