package filedoor

import "html/template"

// browse.go is the whole browse page, and it is ONE FILE WITH NOTHING OUTSIDE
// IT — no CDN, no framework, no font download, no second request for a
// stylesheet. Two reasons, and both of them are the point of this package.
//
// The first is that a door onto somebody's far machine must not open a
// connection to anywhere else. A page that pulls a script off the internet is a
// page whose behaviour is decided by a third party at load time, and this one
// is looking at a private workspace.
//
// The second is that the page has to work where the person actually is: an ssh
// session into a machine on a plane, a laptop with no route out. The listing
// arrives; a spinner waiting on a font does not.
//
// ── THE VOICE IS THE SURFACE'S VOICE ────────────────────────────────────────
//
// This repo's aesthetic is restraint (internal/tui): dim telemetry, no borders
// shouting, nothing coloured that has not earned it. So the page is a system
// font, two weights, one accent, and rules that are barely there. It is honest
// in both themes because a person's browser already told us which one they are
// in, and asking again with a toggle would be one more decision for somebody
// who only wanted to look at a log.

// browseData is what the template is given: whose disk this is and the ceiling
// — interpolated rather than spelled in the JavaScript, so the number lives in
// exactly one place.
//
// THERE IS NO TOKEN IN IT. The page authorises itself with the cookie the door
// set on the way in (filedoor.go's handToken), which means the secret is in
// neither the address bar nor the document, and a fetch this page makes carries
// it without anything here having to hold it.
type browseData struct {
	Host     string
	MaxBytes int64
}

var browsePage = template.Must(template.New("browse").Parse(browseSource))

const browseSource = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<!-- Nothing navigated to from here learns where it was opened from. Belt as
     well as braces now the address holds no token, and the braces the day
     somebody puts one back. -->
<meta name="referrer" content="no-referrer">
<title>{{.Host}} — aforge files</title>
<style>
:root {
  color-scheme: light dark;
  --paper: #fcfcfb;
  --ink: #1c1c1a;
  --dim: #86867e;
  --rule: #e6e5e0;
  --hover: #f2f1ec;
  --accent: #3d6b8e;
  --warm: #8a6a3a;
}
@media (prefers-color-scheme: dark) {
  :root {
    --paper: #16171a;
    --ink: #d8d8d3;
    --dim: #74767c;
    --rule: #26282c;
    --hover: #1e2024;
    --accent: #7fa8c9;
    --warm: #c0a071;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  padding: 2.2rem 2rem 4rem;
  background: var(--paper);
  color: var(--ink);
  font: 14px/1.55 system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
}
main { max-width: 62rem; margin: 0 auto; }
header { max-width: 62rem; margin: 0 auto 1.6rem; }
.host { font-size: 12px; letter-spacing: .09em; text-transform: uppercase; color: var(--dim); }
nav { margin-top: .35rem; font-size: 16px; }
nav span.sep { color: var(--dim); padding: 0 .35em; }
nav a { color: var(--accent); text-decoration: none; cursor: pointer; }
nav a:hover { text-decoration: underline; }
nav b { font-weight: 600; }
.resolved {
  margin-top: .3rem; color: var(--dim); font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
table { width: 100%; border-collapse: collapse; }
td { padding: .34rem .6rem; border-bottom: 1px solid var(--rule); vertical-align: baseline; }
tr:last-child td { border-bottom: none; }
tr:hover td { background: var(--hover); }
td.name { overflow-wrap: anywhere; }
td.size { text-align: right; white-space: nowrap; color: var(--dim); width: 7rem; font-variant-numeric: tabular-nums; }
td.when { white-space: nowrap; color: var(--dim); width: 11rem; font-variant-numeric: tabular-nums; }
td.note { white-space: nowrap; color: var(--warm); width: 10rem; font-size: 12px; }
a.row { color: var(--accent); text-decoration: none; cursor: pointer; }
a.row:hover { text-decoration: underline; }
.dir::after { content: "/"; color: var(--dim); }
.plain { color: var(--ink); }
.aside { max-width: 62rem; margin: .9rem auto 0; color: var(--dim); font-size: 12px; min-height: 1.4em; }
.aside.bad { color: var(--warm); }
.empty { color: var(--dim); padding: .6rem; }
#veil {
  position: fixed; inset: 0; display: none; place-items: center;
  background: color-mix(in srgb, var(--paper) 86%, transparent);
  font-size: 15px; color: var(--dim);
  border: 2px dashed var(--rule); border-radius: 2px;
}
body.dropping #veil { display: grid; }
</style>
</head>
<body>
<header>
  <div class="host">{{.Host}}</div>
  <nav id="crumbs"></nav>
  <div class="resolved" id="resolved"></div>
</header>
<main>
  <table><tbody id="rows"></tbody></table>
  <div class="aside" id="aside"></div>
</main>
<div id="veil">drop a file to send it over</div>
<script>
// No token anywhere on this page: every call below is same-origin, so the
// browser sends the door's HttpOnly cookie with it and the page never handles
// the secret at all.
const HOST = {{.Host}};
const MAX_BYTES = {{.MaxBytes}};
const MAX_LABEL = Math.round(MAX_BYTES / (1024 * 1024)) + "MB";

const rows = document.getElementById("rows");
const crumbs = document.getElementById("crumbs");
const resolved = document.getElementById("resolved");
const aside = document.getElementById("aside");

let here = ".";

// Sizes are for a person deciding whether to open something, so one number and
// one unit is the whole job; a byte count with five digits in it is telemetry.
function humanSize(n) {
  if (n < 1024) return n + " B";
  const units = ["KB", "MB", "GB", "TB"];
  let value = n / 1024, unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  return (value >= 10 ? Math.round(value) : value.toFixed(1)) + " " + units[unit];
}

// An unknown time renders as nothing at all, never as 1970 — the emptiness law
// the rest of this program is written under.
function humanTime(stamp) {
  if (!stamp) return "";
  const when = new Date(stamp);
  if (isNaN(when)) return "";
  const now = new Date();
  const opts = { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hour12: false };
  if (when.getFullYear() !== now.getFullYear()) opts.year = "numeric";
  return when.toLocaleString(undefined, opts);
}

function join(base, name) {
  if (base === "." || base === "") return name;
  return base.replace(/\/+$/, "") + "/" + name;
}

function say(text, bad) {
  aside.textContent = text || "";
  aside.classList.toggle("bad", !!bad);
}

function drawCrumbs() {
  crumbs.textContent = "";
  const parts = here === "." ? [] : here.split("/").filter(Boolean);
  const add = (label, target, last) => {
    if (last) {
      const strong = document.createElement("b");
      strong.textContent = label;
      crumbs.appendChild(strong);
      return;
    }
    const link = document.createElement("a");
    link.textContent = label;
    link.addEventListener("click", () => go(target));
    crumbs.appendChild(link);
    const sep = document.createElement("span");
    sep.className = "sep";
    sep.textContent = "/";
    crumbs.appendChild(sep);
  };
  add(".", ".", parts.length === 0);
  let walked = "";
  parts.forEach((part, i) => {
    walked = walked ? walked + "/" + part : part;
    add(part, walked, i === parts.length - 1);
  });
}

function drawRows(entries, truncated) {
  rows.textContent = "";
  const sorted = entries.slice().sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
  if (sorted.length === 0) {
    const tr = rows.insertRow();
    const td = tr.insertCell();
    td.colSpan = 4;
    td.className = "empty";
    td.textContent = "nothing here";
    return;
  }
  for (const entry of sorted) {
    const tr = rows.insertRow();
    const name = tr.insertCell();
    name.className = "name";
    const target = join(here, entry.name);
    // A FILE TOO BIG TO CROSS IS NOT DRAWN AS A LINK. A link that cannot open
    // is the surface claiming a door it has not got; the row says the size and
    // says why, and the person is spared the click that spins and fails.
    const heavy = !entry.dir && entry.size > MAX_BYTES;
    if (heavy) {
      const span = document.createElement("span");
      span.className = "plain";
      span.textContent = entry.name;
      name.appendChild(span);
    } else {
      const link = document.createElement("a");
      link.className = "row" + (entry.dir ? " dir" : "");
      link.textContent = entry.name;
      if (entry.dir) {
        link.addEventListener("click", () => go(target));
      } else {
        link.href = "/api/file?path=" + encodeURIComponent(target);
        link.target = "_blank";
        // noreferrer as well as noopener: the tab that opens is looking at a
        // file off somebody else's disk, and it is told neither what opened it
        // nor where from.
        link.rel = "noopener noreferrer";
      }
      name.appendChild(link);
    }
    const size = tr.insertCell();
    size.className = "size";
    size.textContent = entry.dir ? "" : humanSize(entry.size);
    const when = tr.insertCell();
    when.className = "when";
    when.textContent = humanTime(entry.mtime);
    const note = tr.insertCell();
    note.className = "note";
    note.textContent = heavy ? "too big to cross" : "";
  }
  if (truncated) {
    const tr = rows.insertRow();
    const td = tr.insertCell();
    td.colSpan = 4;
    td.className = "empty";
    // The count is whatever actually arrived rather than a ceiling repeated
    // here: the cap belongs to the engine, and a number copied is a number that
    // drifts the day the engine changes its mind.
    td.textContent = "first " + sorted.length + " entries";
  }
}

async function go(where) {
  const asked = where || ".";
  try {
    const answer = await fetch("/api/ls?path=" + encodeURIComponent(asked));
    if (!answer.ok) { say((await answer.text()).trim() || "that listing did not come back", true); return; }
    const listing = await answer.json();
    here = asked;
    drawCrumbs();
    resolved.textContent = listing.path || "";
    drawRows(listing.entries || [], listing.truncated);
    say("");
  } catch (err) {
    say("that listing did not come back", true);
  }
}

// Dragging anywhere on the page is the gesture, because a person dropping a
// file is not aiming at a widget — they are giving it to the machine on the
// other side of this window.
let dragDepth = 0;
window.addEventListener("dragenter", (e) => { e.preventDefault(); dragDepth++; document.body.classList.add("dropping"); });
window.addEventListener("dragover", (e) => { e.preventDefault(); });
window.addEventListener("dragleave", (e) => { e.preventDefault(); if (--dragDepth <= 0) { dragDepth = 0; document.body.classList.remove("dropping"); } });
window.addEventListener("drop", (e) => {
  e.preventDefault();
  dragDepth = 0;
  document.body.classList.remove("dropping");
  const files = Array.from((e.dataTransfer && e.dataTransfer.files) || []);
  if (files.length) sendAll(files);
});

async function sendAll(files) {
  for (const file of files) {
    if (file.size > MAX_BYTES) {
      say(file.name + " is bigger than " + MAX_LABEL + " and the most one file may cross this connection is " + MAX_LABEL, true);
      continue;
    }
    try {
      const landed = await send(file);
      say("landed at " + landed + " on " + HOST);
    } catch (err) {
      say(String(err && err.message ? err.message : err) || "that file did not go over", true);
    }
  }
  go(here);
}

// XMLHttpRequest and not fetch, for the one thing fetch still cannot do: tell a
// person how far their upload has got. A quiet percentage is the whole
// difference between waiting and wondering.
function send(file) {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append("file", file, file.name);
    const request = new XMLHttpRequest();
    request.open("POST", "/api/put");
    request.upload.addEventListener("progress", (e) => {
      if (!e.lengthComputable) { say("sending " + file.name + "…"); return; }
      say("sending " + file.name + " — " + Math.round((e.loaded / e.total) * 100) + "%");
    });
    request.addEventListener("load", () => {
      if (request.status >= 200 && request.status < 300) {
        try { resolve(JSON.parse(request.responseText).landed); }
        catch (err) { reject(new Error("that file went over but the answer did not come back")); }
        return;
      }
      reject(new Error((request.responseText || "").trim() || "that file did not go over"));
    });
    request.addEventListener("error", () => reject(new Error("that file did not go over")));
    say("sending " + file.name + "…");
    request.send(form);
  });
}

go(".");
</script>
</body>
</html>
`
