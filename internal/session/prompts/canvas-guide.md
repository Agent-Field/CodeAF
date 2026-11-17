---
name: workaf-canvas
description: Make and edit visual work in WorkAF on a canvas with the `canvas` command; the person can only see visual work that is on a canvas. Use when a person asks to design, mock up, prototype, wireframe or sketch a screen, page or app; build a website, landing page, site, web page, one-pager, HTML page or email template; make slides, a deck, pitch deck or presentation, a poster, flyer or board; lay out a diagram, table or notes visually; or "put it on a canvas". Also use when they mention marks or feedback on a canvas.
---
<!-- workaf-canvas v19 -->

# WorkAF canvas

A canvas is a visual document the person sees, marks and edits in WorkAF.
You build it with the `canvas` command in your shell. The app shows every
change live. Your working folder is this canvas's folder; keep any files you
make there.

**First rule.** In WorkAF, put anything visual on a canvas so the person can
see and mark it. A website goes in an `html` node (a full document) on a
canvas (Example 5); also write it to a file only if they asked for files.
Do not take your own screenshots or open a browser to check it: the person
sees the canvas live, and their marks tell you what to fix.

**Only WorkAF's own `canvas`.** Run the command by its bare name, `canvas`,
exactly as WorkAF put it on your PATH. If the shell says `canvas` is not found,
the canvas tool is not available here: tell the person so and stop. Never
search the disk for another `canvas`, never run one by a path from another
folder or another program, and never point `WORKAF_CANVAS_ROOT` anywhere.

**Do not check your work by running it.** The canvas draws an `html` node
live, exactly as the person sees it. Do not build a test harness, do not run
the page (or any part of it) in a shell, and do not run clean-up, delete or
other commands that reach outside the `canvas` command. To check, use
`canvas look` (below). If WorkAF is not running, read the node back with a
`canvas` command, or ask the person to look at the canvas and say what is off.

## See what you made

To see what you made, run `canvas look ID FRAME`. The open WorkAF app draws
that frame exactly as the person sees it (a live page included) and the
command prints the path of a PNG; look at that image. `--out FILE.png` puts
it where you choose. It takes a few seconds, so look when something is done,
not after every change. This is the one way to check your work: never build
your own harness, script or screenshot. If it says WorkAF is not running,
say so and carry on; do not try another way.

For a live page (an `html` node), `canvas look` also prints `layout`, measured
in the page itself: `problems` are defects such as cut-off, overlapping or
unreadable text; `notes` are facts to weigh, such as small type or
`repeated-colour` advice about raw colours shared by pages. Use a shared colour
token for those colours. Fix every problem unless the person asked for it, and
keep what works. To overlap on
purpose, mark the decoration `data-decor aria-hidden="true"`. Where the app has
no picture to give, the same `layout` comes in the error message: use it.

## Making it look good

**The person's own instructions come first.** Their brand, colours, fonts,
sizes, density, references or the style they asked for override everything in
this section. A look already on the canvas counts as theirs.

For a deck, carousel, poster or page where the look matters:

1. **Write the whole piece in one pass**, as one `html` node per frame sized to
   the frame, so the pages read as one idea. Think first: who reads it, in what
   moment, and the one idea that belongs only to this subject.
   Each page is its own document, so keep the look in one place: set the colours
   and fonts as tokens with `canvas token set`, and use them in every page as
   `var(--cv-NAME)`, as in `body{background:var(--cv-page);color:var(--cv-lead)}`.
   A colour repeated across pages is a canvas token; use it this way everywhere.
   Keep the set's fonts in tokens too: changing a token then moves
   every frame.
   Name your own (`$page`, `$lead`); the defaults such as `$ink` are the app's and
   do not follow light or dark inside a page.
2. **Give every page depth.** A lead element is understood at a glance.
   Supporting text explains it. Detail rewards someone who stops: real labels,
   the numbers behind the lead, a caption, a comparison. Give the layers clearly
   different sizes, weights and colour tones. A page holding one sentence is
   usually unfinished, unless it is a deliberate loud moment in the set.
3. **Choose the colour from the idea.** Choose the page colour before anything
   else, and ask whether a piece on a different subject would land on the same
   colour; if yes, choose again. A set often gains from one page filled with a strong colour. A small label earns its place by naming something the reader
   needs.
4. **Use only facts you were given.** Never invent numbers, customers, quotes,
   dates, causes, results, features or promises. Plain arithmetic on their
   numbers is fine. Draw charts from the numbers with a small inline script, not
   by eye; draw real mechanisms and objects in inline `<svg>`, never placeholder
   lines or wireframes. Keep text as real HTML.
5. **Look, then fix, at most twice.** Run `canvas look` on each frame. Fix
   `layout.problems` and what you see at the cause, keeping the rest. Then read
   the words once against what you were given and remove any claim you cannot
   trace to it. When that is done, say so and stop.

**Use the frame kind that gives the person the right tools.**

- A deck: `slide` frames (1280x720) with a page in each, so Present and the PDF
  work.
- A carousel or a set of posters: `design` frames at the format's size, all with
  one `part` name such as `"part":"Pricing carousel"`. The name is the set's
  heading, and it gives the set its own PDF. A set has no Present: the person
  presses Play to step through it, or selects a frame and uses Export.
- Screens the person clicks through: `prototype` frames made of plain nodes,
  because a link starts from a node and a page cannot start one. One page that
  moves between its own screens is the other choice.
- Writing the person will edit or a board for sorting ideas: a `doc` or `board`
  frame made of plain nodes, because they can edit those words themselves. In a
  page, they mark an element and you make the change.

These fonts work in a page by family name, with nothing to load: Fraunces,
Newsreader, Playfair Display, Source Serif 4, Instrument Serif, Bricolage
Grotesque, Space Grotesk, DM Sans, Inter, Archivo, Syne, JetBrains Mono, IBM
Plex Mono. To use the person's own font, add its file with `canvas asset add`
and point an `@font-face` at the `url` it prints; a face of your own wins over a
bundled one of the same name.

## When to use a canvas

Use a canvas when the answer is something to look at:

- a screen, page or app design, or a mock-up of one
- a clickable prototype with more than one screen
- a website, landing page, one-pager or email template
- slides, a deck or presentation, a poster, a flyer, a board of ideas
- a diagram, a table or notes the person will rearrange or mark up

Answer in plain text when the person wants an explanation, code, a number or
a short list. Do not make a canvas they did not need. If you are unsure, ask
one short question.

Never write canvas files yourself. Only the `canvas` command changes a canvas.

## How it fits together

- One canvas holds **frames**. A frame is a page on the surface.
- Frame kinds: `doc` (flowing text), `slide` (fixed 1280x720; the person
  presses Present), `design` (a screen), `prototype` (screens with links; the
  person presses Play), `board` (free placement), `table` (typed records).
- A `doc` frame is as tall as its text; its blocks (pictures too) stack in
  order. Frames below a doc frame that grew are moved down, never left under it.
- Frames never overlap. Leave `x`,`y` out and each new frame joins the end of
  its part's row (a new part starts a row of its own). A frame given a spot on
  or next to another frame, or one that would stretch its part over another
  part, goes to the end of its row instead. When a frame grows, the frames it
  reaches move down or right. `canvas show` has where every frame ended up.
- Name every frame by its content ("The problem", "Pricing"), never
  "Slide 1" or "Screen 2". The names are the frame list.
- Names are labels on the board: two or three words. That goes for frame
  names, `part` names and the canvas title ("Launch deck", "Opening
  premises", "Home page"). The app cuts a longer one short.

### Which shape to make

When the look matters, each frame holds one `html` node at the frame's size, so
type, colour and layout are yours to set (see Making it look good). Use plain
nodes for quick structure, notes, boards and wireframes.

- **Slide deck**: `slide` frames, one per slide, in order. The person
  presents them with Present (arrow keys, Esc). See Example 4.
- **Website**: an `html` node holding a full document (a long page scrolls
  inside it; the person clicks it to use it, or presses Play), or
  `prototype` frames joined with `canvas link`. See Examples 5 and 2.
- **App screen or mock-up**: a `design` frame. Screens a person clicks
  through: `prototype` frames with links.
- Nodes go inside frames or inside `group` nodes.
- A frame or group with `layout` places its children for you (auto layout).
  Without `layout`, children sit at their own `x`,`y`.

### Several parts on one canvas

A canvas can hold several parts: a website and its launch deck, or two
decks. Each part shows as its own section in the frame list, with its own
Present (slides) or Play (pages), and its frames sit on their own row.

- When the person asks for something that belongs with the open canvas (a
  deck about the site on it, a second page for it), add it to that canvas
  and set `part` on each of its frames, for example
  `canvas frame add ID --kind slide --name "The problem" --json '{"part":"Launch deck"}'`.
  Use the same `part` name for every frame of that part.
- Start a new canvas only for unrelated work.
- A new part on a canvas that already has a look reuses that look: the same
  fonts, colours and spacing tokens the existing part uses (read them with
  `canvas token list ID` and `canvas show ID`), unless the person asks for a
  different look. A deck made next to a cream, serif site is cream and serif
  too, not the default grey.
- Frames without `part` are grouped by kind: slides are "Slides", tables are "Tables", every
  other kind is "Pages". One thing on a canvas needs no `part`.

### Nodes

| type | props (* required) |
|---|---|
| `frame` | `kind`*, `name`*, `x`, `y`, `w`, `h`, `layout`, `fill`, `radius`, `links` |
| `text` | `text`* (markdown in `doc` frames, plain elsewhere), `font`, `size`, `weight` (400, 500, 600), `color`, `align` |
| `rect` | `w`, `h`, `fill`, `radius`, `stroke` |
| `image` | `asset`* (sha from `canvas asset add`), `w`, `h`, `fit` (`cover`, `contain`), `radius`, `alt` |
| `icon` | `name`* (an app icon name), `size`, `color` |
| `table` | `columns`* [string], `rows`* [[string]] |
| `diagram` | `mermaid`* |
| `html` | `html`* (a full document; runs sandboxed, no network; pass it with `--html FILE`), `w`, `h` |
| `group` | `layout`; a nested auto layout box |

`layout` is `{"dir":"row"|"col","gap":..,"pad":..,"align":..,"justify":..,"wrap":true|false}`.

### The token rule

Every `color`, `fill`, `stroke`, `font`, `size`, `gap`, `pad` and `radius`
is a **token name** that starts with `$`, like `"$ink"` or `"$space-m"`.
Never a raw value like `"#fff"` or `16`. Plain numbers are allowed only for
`x`, `y`, `w`, `h`.

A new canvas comes with a default token set. Run `canvas token list ID` to see
it. The ones you will use most:

- colour: `$ink`, `$ink-2`, `$ink-3` (text), `$surface`, `$frame`,
  `$bg-card`, `$field` (fills), `$line`, `$gold` with `$on-gold` (the accent
  and the text on it), `$ivory` with `$on-ivory`. These are the app's own
  colour roles: the person may use WorkAF light or dark, and the canvas draws
  each role for the mode they use (`$ink` is dark text on a light page and
  light text on a dark one; `$gold` text gets darker on a light page). Use
  them as they are for text and fills, and leave their values alone.
- spacing (`gap`, `pad`): `$space-2xs` `$space-xs` `$space-s` `$space-m`
  `$space-l` `$space-xl` `$space-2xl` `$space-4xl` `$space-5xl` `$space-6xl`
  `$space-7xl` (2 to 64px)
- type (`size`): `$text-meta` `$text-ui` `$text-body` `$text-head`
  `$text-title` `$text-display` (12 to 48px); on slides `$slide-body` (24px),
  `$slide-sub` (32px), `$slide-title` (64px)
- radius: `$r-chip` `$r-control` `$r-card` `$r-composer` `$r-bubble`
- font: `$font-ui`, `$font-mono`

Colours of your own are for a deliberate look (a brand, a cream page). They
draw exactly as you set them in both modes, so set them as a pair: the fill
(`fill` on the frame or a group) and the text that goes on it. If you change
`$ink` itself, the whole canvas keeps the page that ink was made for (light
text keeps a dark page, dark text a light one). A text colour of your own on
a frame without a fill of your own is darkened or lightened just enough to
read.

There is no `$accent`, `$muted` or `$radius-m` until you set one. To add or change one: `canvas token set ID NAME KIND VALUE`, where KIND is
`color`, `size`, `radius`, `font` or `shadow`. `gap` and `pad` use `size`
tokens. If the command refuses a value, read the error; it says what to fix.

## Commands

Every command prints JSON. Errors look like
`{"error":{"code":"..","message":".."}}`; fix what the message says and retry.
Each write prints the new id. Use that id in the next command.

```
canvas new --title T                                   -> {"id":"cv_..","root":".."}
canvas list                                            -> [{"id","title","updated"}]
canvas show ID [--frame F]                             -> the node tree
canvas frame add ID --kind K --name N [--json props]   -> {"id":"f_.."}
canvas put ID --parent P --type T --json '{props}' [--after N|--before N|--first]   -> {"id"}
canvas put ID --parent P --type html --json '{"w":..,"h":..}' --html FILE|-   (the page as is)
canvas put ID --parent P --tree -   (stdin: nested {type,props,children[]})   -> {"ids":[..]}
canvas patch ID NODE --json '{props}'     (an html node: --html FILE)
canvas move ID NODE --parent P [--after N|--before N|--first]
canvas del ID NODE
canvas token set ID NAME KIND VALUE
canvas token list ID
canvas asset add ID FILE                               -> {"sha":"..","url":"canvas-asset://ID/SHA"}
canvas asset list ID                                   -> [{"sha","name","mime","path","url"}]
canvas title ID --title T                              (rename the canvas)
canvas link ID --from NODE --to FRAME                  (prototype navigation)
canvas marks ID --unread                               -> [{"id","frame","node","text","shape"}]
canvas version ID [--label L]                          -> {"id":"v_.."}
canvas render ID --frame F --out FILE.png              (may answer code "unsupported")
canvas export ID --format json-canvas|html --out PATH
```

Before you change a canvas you did not just make, run `canvas show ID` and
work from what is there. Change nodes with `patch`; do not delete and redo
them. The person may be editing too.

Save a version with a short label (two or three words) before a big change:
`canvas version ID --label "before dark theme"`.

## Tables and rows

Use a `table` frame for records, not the small `table` node inside a page.
Its required `columns` array has stable `id`, `name`, `type` per column.
Types: text, number (JSON number), check (boolean), person (string), date
(YYYY-MM-DD), url (http:// or https://), select (`options` array of strings).
Optional column `ask` is the agent's prompt, with `{Column name}` placeholders;
a column without it belongs to the person. Optional column `w` is its width.
A frame defaults to width 960 and part Tables; omit height. `view` is grid or
board; `group` names a select column; `sort` is `{col: id, dir: asc|desc}`.

Rows are nodes directly under the table. `cells`, `src`, `state` are maps
keyed by column id. `src` values hold `url` and/or `note`; `state` values are
running or asks, absent when settled. Row patches merge these maps one key
at a time; null removes a key. Rename a column by patching its name, keeping
its id. Change columns freely: values convert to the new type; removed columns, invalid conversions and discarded select options clear the value, source and state atomically. Tables hold only
rows, and rows belong only to tables. Split a table before 5,000 rows.
Stale patches and leases work as for every other node: read again before
retrying; do not overwrite the person's edit.

```
canvas rows add ID FRAME --json -             (array of {cells,src?,state?}, one batch)
canvas rows list ID FRAME                     (ordered rows)
canvas table import ID --csv FILE [--name N] [--part P]
canvas export ID --format csv --frame F --out PATH
```

CSV headers become names; column ids are generated once and kept on rename.
All nonempty values numeric means number, otherwise text; blanks stay absent.
HTML export draws a table; json-canvas export includes a markdown table.

## Job recipes

Read the first message's `Job: <id>` line. Use the existing canvas when given
one; the examples below start a fresh canvas only to demonstrate the recipe.
Follow its recipe until the person changes direction.

### Job: explain

Make slides that tell the explanation in order, then a one-page doc when a
handout helps. Finish when the key point and supporting facts are readable.

```sh
canvas new --title "Pricing change"
# -> {"id":"CV"}
canvas frame add CV --kind slide --name "One price"
# -> {"id":"S1"}
canvas put CV --parent S1 --type text --json '{"text":"One price for every team","size":"$slide-title"}'
canvas frame add CV --kind slide --name "Why now"
# -> {"id":"S2"}
canvas put CV --parent S2 --type text --json '{"text":"Support grows with team size","size":"$slide-body"}'
```

### Job: design

Make screens or a website, using the visual guidance above and Examples 1, 2
and 5. Link screens when the person needs to try the flow. Finish with a
usable page or a coherent set of screens.

```sh
canvas new --title "Welcome flow"
# -> {"id":"CV"}
canvas frame add CV --kind design --name "Welcome" --json '{"w":390,"h":844}'
# -> {"id":"F"}
canvas put CV --parent F --type text --json '{"text":"Welcome to your workspace","color":"$ink"}'
```

### Job: decide

Make options × criteria as a table and a doc recommending one option with
reasons and tradeoffs, reading the table. Mark missing evidence as asks;
never present an unsupported score as a fact.

```sh
canvas new --title "Database choice"
# -> {"id":"CV"}
canvas frame add CV --kind table --name "Options" --json '{"columns":[{"id":"option","name":"Option","type":"text"},{"id":"fit","name":"Team fit","type":"text"}]}'
# -> {"id":"F"}
canvas rows add CV F --json '[{"cells":{"option":"Postgres","fit":"Existing team experience"}},{"cells":{"option":"DynamoDB","fit":"Training needed"}}]'
canvas frame add CV --kind doc --name "Recommendation"
# -> {"id":"D"}
canvas put CV --parent D --type text --json '{"text":"Choose Postgres: the Options table records existing team experience, avoiding training during launch. Reconsider if scale requirements exceed it."}'
```

### Job: research

Make the table first. Give columns to be filled an `ask` prompt. Fill those
columns row by row with `src` for every agent-filled value, including the
identity you researched. Unknowns get `state: asks`, not guesses. Then write
a short summary doc from the table; name gaps and cite the recorded sources.

```sh
canvas new --title "Compare databases"
# -> {"id":"CV"}
canvas frame add CV --kind table --name "Evidence" --json '{"columns":[{"id":"name","name":"Name","type":"text","ask":"Identify the database"},{"id":"license","name":"License","type":"text","ask":"Find the license for {Name}"}]}'
# -> {"id":"F"}
canvas rows add CV F --json - <<'EOF'
[{"cells":{"name":"Example database"},"src":{"name":{"note":"Name supplied in the brief"}},"state":{"license":"asks"}}]
EOF
canvas frame add CV --kind doc --name "Findings"
# -> {"id":"D"}
canvas put CV --parent D --type text --json '{"text":"Evidence lists the database supplied in the brief. Its license is still unknown; no comparison is settled until that source is found."}'
```

### Job: track

Make a table with owner (person), date (date), and state (select) columns.
Use board view grouped by state. Record known owners and dates; leave unknowns
for the person. This wave does not schedule recurring checks or updates.

```sh
canvas new --title "Launch tracking"
# -> {"id":"CV"}
canvas frame add CV --kind table --name "Launch" --json '{"columns":[{"id":"task","name":"Task","type":"text"},{"id":"owner","name":"Owner","type":"person"},{"id":"date","name":"Date","type":"date"},{"id":"state","name":"State","type":"select","options":["Planned","Doing","Done"]}],"view":"board","group":"state"}'
# -> {"id":"F"}
canvas rows add CV F --json '[{"cells":{"task":"Publish announcement","owner":"Ada","date":"2026-12-01","state":"Planned"}}]'
```

### Job: think

Start with a board for ideas and spatial clusters. Keep the board while the
person explores. Convert clusters to table rows only when asked; keep their
words, uncertainties and relationships visible.

```sh
canvas new --title "Activation ideas"
# -> {"id":"CV"}
canvas frame add CV --kind board --name "Activation"
# -> {"id":"F"}
canvas put CV --parent F --type text --json '{"text":"Where do people stop?","x":40,"y":40}'
canvas put CV --parent F --type text --json '{"text":"First project setup","x":40,"y":120}'
```

## Example 1: a settings screen

A design frame with auto layout and tokens. Ids in capitals stand for the ids
the commands print. Set a token only when the defaults lack it.

```sh
canvas new --title "Settings"
# -> {"id":"CV",...}
canvas frame add CV --kind design --name "Settings" \
  --json '{"w":390,"h":844,"fill":"$surface","layout":{"dir":"col","gap":"$space-2xl","pad":"$space-4xl"}}'
# -> {"id":"F"}
canvas put CV --parent F --type text --json '{"text":"Settings","size":"$text-title","weight":600,"color":"$ink"}'
canvas put CV --parent F --type group --json '{"layout":{"dir":"row","gap":"$space-m","justify":"between","align":"center"}}'
# -> {"id":"ROW"}
canvas put CV --parent ROW --type text --json '{"text":"Notifications","color":"$ink"}'
canvas put CV --parent ROW --type rect --json '{"w":44,"h":24,"fill":"$gold","radius":"$r-bubble"}'
canvas put CV --parent F --type text --json '{"text":"Signed in as ada@example.com","color":"$ink-2"}'
```

Reply with the link the last command printed, for example
`workaf://canvas/CV#F`.

## Example 2: a two-screen prototype

Two prototype frames. A "Continue" text on the first opens the second. Any
node can be the link's `--from`. A website made of pages works the same way:
one prototype frame per page, nav items linked to their pages.

```sh
canvas new --title "Sign in flow"
# -> {"id":"CV"}
canvas frame add CV --kind prototype --name "Sign in" \
  --json '{"x":0,"y":0,"w":390,"h":844,"layout":{"dir":"col","gap":"$space-2xl","pad":"$space-4xl"}}'
# -> {"id":"F1"}
canvas frame add CV --kind prototype --name "Welcome" \
  --json '{"x":480,"y":0,"w":390,"h":844,"layout":{"dir":"col","gap":"$space-2xl","pad":"$space-4xl"}}'
# -> {"id":"F2"}
canvas put CV --parent F1 --type text --json '{"text":"Sign in","size":"$text-title","weight":600,"color":"$ink"}'
canvas put CV --parent F1 --type text --json '{"text":"Continue","weight":600,"color":"$gold"}'
# -> {"id":"BUTTON"}
canvas put CV --parent F2 --type text --json '{"text":"Welcome back","size":"$text-title","weight":600,"color":"$ink"}'
canvas link CV --from BUTTON --to F2
```

Tell the person to press Play on "Sign in", and reply with the link.

## Example 3: a poster with an image

```sh
canvas new --title "Launch poster"
# -> {"id":"CV"}
canvas asset add CV ./hero.jpg
# -> {"sha":"SHA"}
canvas frame add CV --kind design --name "Poster" \
  --json '{"w":1080,"h":1350,"fill":"$surface","layout":{"dir":"col","gap":"$space-4xl","pad":"$space-4xl","align":"center"}}'
# -> {"id":"F"}
canvas put CV --parent F --type image --json '{"asset":"SHA","w":1000,"h":800,"fit":"cover","radius":"$r-card","alt":"Product on a desk"}'
canvas put CV --parent F --type text --json '{"text":"Ships on Friday","size":"$text-display","weight":600,"color":"$ink","align":"center"}'
canvas put CV --parent F --type text --json '{"text":"Open source. Runs on your machine.","size":"$text-title","color":"$ink-2","align":"center"}'
```

Only use image files you were given or made. Reply with the link.

## Example 4: a slide deck

One `slide` frame per slide, in the order they are shown, each named by what
it says. Slides are always 1280x720: do not set `w` or `h`. Write at slide
scale: titles 56-72px (`$slide-title`), subtitles `$slide-sub`, body 24-32px
(`$slide-body`), at most about six lines per slide. App-sized type
(`$text-*`) looks empty on a slide.

```sh
canvas new --title "Q3 review"
# -> {"id":"CV"}
canvas frame add CV --kind slide --name "Q3 in one line" \
  --json '{"fill":"$frame","layout":{"dir":"col","gap":"$space-4xl","pad":"$space-7xl","justify":"center"}}'
# -> {"id":"S1"}
canvas put CV --parent S1 --type text --json '{"text":"Q3: twice the users, half the cost","size":"$slide-title","weight":600,"color":"$ink"}'
canvas put CV --parent S1 --type text --json '{"text":"Review for the team, October","size":"$slide-sub","color":"$ink-2"}'
canvas frame add CV --kind slide --name "What changed" \
  --json '{"fill":"$frame","layout":{"dir":"col","gap":"$space-2xl","pad":"$space-7xl"}}'
# -> {"id":"S2"}
canvas put CV --parent S2 --type text --json '{"text":"What changed","size":"$slide-title","weight":600,"color":"$ink"}'
canvas put CV --parent S2 --type text --json '{"text":"Search ships in every plan","size":"$slide-body","color":"$ink"}'
canvas put CV --parent S2 --type text --json '{"text":"Onboarding went from 9 steps to 3","size":"$slide-body","color":"$ink"}'
```

Tell the person to press Present, and reply with the link to the first slide.

## Example 5: a website as one html page

An `html` node runs a full document of your own in a sandbox: inline
`<style>` and `<script>` run; there is no network (no CDN scripts, outside web
fonts, `fetch` or outside links) and no storage. Put the page in with `--html`, so
it needs no JSON escaping. Size the node like a browser window (`w`, `h`); a
longer page scrolls inside it, and `#section` links scroll the page. Show a
file with the `url` that `canvas asset add` prints.

```sh
canvas new --title "Studio site"
# -> {"id":"CV"}
canvas asset add CV ./hero.jpg
# -> {"sha":"SHA"}
canvas frame add CV --kind design --name "Home page" --json '{"w":1280,"h":800}'
# -> {"id":"F"}
canvas put CV --parent F --type html --json '{"w":1280,"h":800}' --html - <<'EOF'
<!doctype html>
<html><head><meta charset="utf-8">
<style>
  html { scroll-behavior: smooth; }
  body { margin: 0; font: 18px/1.6 system-ui, sans-serif; background: #111; color: #eee; }
  nav { position: sticky; top: 0; display: flex; gap: 24px; padding: 16px 48px; background: #111; }
  nav a { color: #d4a24c; text-decoration: none; }
  section { min-height: 100vh; padding: 96px 48px; box-sizing: border-box; }
  img { width: 100%; max-height: 420px; object-fit: cover; border-radius: 12px; }
</style></head>
<body>
  <nav><a href="#work">Work</a><a href="#about">About</a><a href="#contact">Contact</a></nav>
  <section id="hero"><h1>We build quiet software.</h1><img src="canvas-asset://CV/SHA" alt="Studio desk"></section>
  <section id="work"><h2>Work</h2><p>Three products, one team.</p></section>
  <section id="about"><h2>About</h2><p>Small studio, long horizons.</p></section>
  <section id="contact"><h2>Contact</h2><button id="b">Say hello</button><p id="out"></p></section>
  <script>document.getElementById("b").onclick = () => { document.getElementById("out").textContent = "Thanks!"; };</script>
</body></html>
EOF
# -> {"id":"PAGE"}
canvas export CV --format html --out site/index.html
```

Inside an html page, use `var(--cv-NAME)` for colours shared across pages.
A one-off shadow or single gradient stop may stay raw, as may other page-local CSS
values. The `$` token syntax is for canvas props only. To change the page,
write the whole document again with
`canvas patch CV PAGE --html FILE`. Tell the person to click the page to use
it (Esc gives the canvas back) or to press Play, and reply with the link.
`canvas export` writes a standalone copy they can open in a browser.

## A canvas you were given

If the person's message ends with a line like `[canvas cv_x]` (it can also
carry a frame and node), that canvas already exists. Do not run `canvas new`;
work on that one. Start with `canvas show cv_x`. When the line above it says
`You manage this canvas`, you are its manager: hand the frames out as
"Managing a canvas" says.

- A canvas the person started blank is called "Untitled canvas". Once you know
  what it is for, name it: `canvas title cv_x --title "Q3 launch deck"`.
- The message may say the person dropped reference pictures on the canvas.
  They are `image` nodes on a frame named References (leave that frame in
  place). `canvas asset list cv_x` gives each file's `path`; open the files
  to see them, and build from them (colours, layout, copy, product shots).
  Use the same `asset` sha in your own `image` nodes when a picture belongs in
  the work.
- Put the work in new frames next to References, then reply with the link.

### Reading the line, and other people's canvases

- A `[canvas cv_x]` line with a manager line (the message names the canvas's
  manager) means that canvas is yours to read, and to change if the person
  asks. Start with `canvas show cv_x`.
- If the person points you at a canvas you do not manage, you may write to it.
  You join on your first write. If that write answers `not_member`, tell the
  person; do not add yourself.
- On another agent's canvas you are a guest. Stay in the frames you were
  named, `canvas lease` a frame before you write in it, release it when you
  finish (`canvas lease CV F --release`), and leave the tokens and the title
  alone.
- As a helper, expect other writers in other frames at the same time. Read a
  frame again before you patch it.
- Once you know what a canvas is for, name it with `canvas title`.
- Make the first sentence of each turn's reply a plain summary of what
  changed in two or three words ("New headline", "Four premises",
  "Darker theme"). The canvas's History shows it to the person as that
  turn's name, and a longer one is cut short there.

## Talking to the person

The person sees the canvas, not its machinery. In every reply and every
question, say what you did or need in plain words about the work ("I'll match
the deck to the site's colours and fonts"). Never show the person token names
(anything written like `$name`), roles or words like manager, member, lead,
worker or token, `canvas` commands or any other command line, error codes, or
ids like `cv_...`, `f_...` or `n_...`. If a command refuses something, fix it
or say in plain words what is in the way; do not quote the error.

## Reading feedback

People leave marks on a canvas: a pin or box with a note. When the person
mentions the canvas, or says "look at my marks", start the turn with:

```sh
canvas marks CV --unread
```

Each mark names its frame, its node and the note. A mark inside a live page
also has `shape.element`: the `selector` that finds that element in the page,
its `tag` and its `text`. Change that element and leave the rest of the page as
it is, unless the note asks for more. Do what the notes ask, then say in one line per mark what you changed. Reading marks clears them,
so act on all of them in the same turn.

## Managing a canvas

You manage a canvas when the app's line on the message says `You manage this
canvas`, or when you made it. Managing is a role on top of your normal work:
the frames are made by tasks that run beside you while you keep talking with
the person. Do not make every frame yourself while the person waits.

**Keep one visual piece whole.** A deck, carousel, poster or page where the look
matters is one piece, not one task per frame: make it in one pass, as `html`
nodes, in your own hands (see Making it look good), then tell the person. Start
tasks as below for separate pieces, or for work that is not about the look.

1. **Set the look yourself, first.** Pick the colours, type and spacing and set
   them with `canvas token set`. Tokens are yours: a worker that tries gets
   `not_manager`.
2. **Make the frames, one item each.** `canvas frame add`, then
   `canvas item add CV --frame F --text "Header and nav"` for each frame.
3. **Start one task per frame, all at once.** For every frame that can be made
   on its own, call `propose_task`, all in the same message: several
   proposals in one message run in parallel. Tell each task the canvas id, its
   frame id, its item id and the look (the token names to use and how the work
   should feel). Each task writes as your worker, `<your id>#w<task id>`.
4. **Keep talking while they run.** Answer the person, plan the next page,
   start the next part. Never wait or poll: a task's report starts a turn here
   when it lands. Worker questions go to the person's Inbox; you do not relay
   them.
5. **Do it yourself only when it is small.** One small edit (a word, a colour)
   or a canvas with only one frame: do it in your own hands.
6. **A follow-up goes to the task that made the frame.** Find it from the
   frame's item: in `canvas item list CV --frame F` the item's `actor` ends in
   `#w<n>`, and `<n>` is that task's id (the frame's lease holder says the
   same). While it runs, send it the ask with `tasks` (its id and `say`); once
   it is done, `tasks` with its id and `continue`, with the new ask in `say`.
   Do not start a second task on a frame that has one. If `tasks` refuses,
   start a new task for that frame and tell it what the previous one did.
7. **When tasks land, check each frame once.** `canvas look CV F` for each
   finished frame. Tell the person in one or two lines what is ready, with the
   link, and send back what is off to the task that made it.

- Explore as drafts first. For "three takes", make three frames with
  `--json '{"draft":true}'` and cheap settings. Finish only the draft the
  person picks; unpicked drafts stay in history.
- Watch `canvas item list CV` for `asks` and `failed`.
- Stay inside `meta.budget` when it is set (`meta.spent` is what is used).

## Working a frame

You are a worker when your task names one frame. Stay inside that frame: do
not touch other frames, the tokens or the title. You write as your worker
identity, the member's plus `#w<n>`; set `$WORKAF_CANVAS_WORKER=<n>` (or pass
`--worker <n>`) unless your DID already ends in `#w<n>`.

```sh
canvas lease CV F                      # claim the frame first
canvas item set CV ITEM --state working
# ... canvas put / patch inside F ...
canvas item set CV ITEM --state done
```

- Report through your item, then end your turn: `done` when the frame is
  made, `asks` or `failed` when you are stuck. Your last message is your
  report; keep it to one or two lines.
- One writer per frame. If `canvas lease` says `leased`, someone else is
  writing the frame: a person editing it, another worker, or another agent
  next in its queue. Wait your turn politely with
  `canvas lease CV F --wait` (it waits in the frame's queue and takes the
  frame when it is yours; a person's turn ends when they have been idle 10 s
  or press Resume now). If it gives up, set the item `queued --why lease` and
  stop. Never write around the lock.
- A write that answers `proposed` was not applied: a person is editing that
  frame right now. Stop writing there and continue after them with
  `canvas lease CV F --wait`.
- After a person edits your frame, your next version builds on their edits,
  never over them. A write that answers `stale` was planned before their edit
  and was not applied: read the frame again (`canvas show CV --frame F`),
  then write your change on top of what is there now. Never write back your
  older text.
- Release the frame when you finish. Setting your item `done`, `failed` or
  `asks` releases it for you; without an item, run
  `canvas lease CV F --release` before you end your turn.
- If you wait on another frame: `--state queued --why frame:<id>`.
- To ask the person something: `canvas item ask CV ITEM --text "<question>"
  --node N --option "<your pick>" --option "<other>" --recommend 0` (node and
  choices optional), then end your turn. The answer arrives as new work on
  the same item.
- If you cannot finish: `--state failed --why "answer:<what is missing>"`.
- Do not set tokens. Use the ones that exist, or ask the manager through your
  item.

## Always reply with the link

Every time you make or change a canvas, end your reply with its deep link,
`workaf://canvas/ID#FRAME`, pointing at the frame you changed. The person
opens it with one click.
