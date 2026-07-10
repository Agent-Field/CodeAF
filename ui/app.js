"use strict";

const state = {
  token: "",
  status: null,
  timeline: [],
  forks: [],
  events: [],
  cursor: null,
  config: { merge_apply: false, poll_interval_ms: 5000 },
  refreshing: false,
};

const $ = (selector) => document.querySelector(selector);
const elements = {
  workspace: $("#workspace-name"), connection: $("#connection-state"), protection: $("#protection-value"),
  universeCount: $("#universe-count"), conflictCount: $("#conflict-count"), snapshotCount: $("#snapshot-count"),
  head: $("#head-value"), timelineCount: $("#timeline-count"), timeline: $("#timeline-list"), forks: $("#fork-table"),
  lanes: $("#conflict-lanes"), laneSubtitle: $("#conflict-subtitle"), events: $("#event-list"), eventCursor: $("#event-cursor"),
  inspector: $("#inspector"), dialogKicker: $("#dialog-kicker"), dialogTitle: $("#dialog-title"),
  dialogBody: $("#dialog-body"), dialogActions: $("#dialog-actions"), toast: $("#toast"),
};

function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", `/assets/icons.svg#${name}`);
  svg.append(use);
  return svg;
}

function button(name, label, handler, extraClass = "icon-button") {
  const control = document.createElement("button");
  control.type = "button";
  control.className = extraClass;
  control.setAttribute("aria-label", label);
  control.title = label;
  control.append(icon(name));
  control.addEventListener("click", (event) => { event.stopPropagation(); handler(control); });
  return control;
}

function extractToken() {
  const fragment = new URLSearchParams(location.hash.slice(1));
  const launchToken = fragment.get("token");
  if (launchToken) {
    sessionStorage.setItem("agit-ui-token", launchToken);
    history.replaceState(null, "", location.pathname);
  }
  state.token = sessionStorage.getItem("agit-ui-token") || "";
  if (!state.token) throw new Error("Mission Control capability is missing. Reopen it with `agit ui`.");
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.token}`);
  if (options.method === "POST") {
    headers.set("Content-Type", "application/json");
    headers.set("X-Agit-UI", "1");
  }
  const response = await fetch(path, { ...options, headers, cache: "no-store" });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(payload.error?.message || `Request failed (${response.status})`);
    error.code = payload.error?.code;
    error.remedy = payload.error?.remedy;
    throw error;
  }
  return payload;
}

const post = (path, body) => api(path, { method: "POST", body: JSON.stringify(body) });

async function refresh() {
  if (state.refreshing || document.hidden) return;
  state.refreshing = true;
  $("#refresh-button").classList.add("spinning");
  try {
    const bootstrapping = state.status === null;
    const eventPath = state.cursor ? `/api/v1/events?limit=100&after=${encodeURIComponent(state.cursor)}` : "/api/v1/events?limit=100";
    const [status, timeline, forks, eventPage, config] = await Promise.all([
      api(bootstrapping ? "/api/v1/status?fidelity=true" : "/api/v1/status"),
      api("/api/v1/timeline?limit=100"),
      api("/api/v1/forks"),
      api(eventPath),
      bootstrapping ? api("/api/v1/config") : Promise.resolve(state.config),
    ]);
    state.status = bootstrapping ? status : { ...state.status, status };
    state.timeline = timeline;
    state.forks = forks;
    state.config = config;
    if (!eventPage.cursor_found) {
      state.cursor = null;
      state.events = [];
    } else {
      state.events.push(...eventPage.events.filter((event) => !state.events.some((item) => item.event_id === event.event_id)));
      state.events = state.events.slice(-100);
      if (eventPage.events.length) state.cursor = eventPage.events.at(-1).cursor;
    }
    render();
    elements.connection.className = "connection online";
    elements.connection.lastChild.textContent = " Live";
  } catch (error) {
    elements.connection.className = "connection error";
    elements.connection.lastChild.textContent = " Disconnected";
    toast(error.message, true);
  } finally {
    state.refreshing = false;
    $("#refresh-button").classList.remove("spinning");
  }
}

function render() {
  renderSummary();
  renderTimeline();
  renderForks();
  renderConflictLanes();
  renderEvents();
}

function renderSummary() {
  const status = state.status?.status || {};
  const path = status.workspace || "Workspace";
  elements.workspace.textContent = path.split("/").filter(Boolean).at(-1) || path;
  elements.workspace.title = path;
  elements.protection.textContent = status.watcher_running ? "Live" : "Manual";
  elements.universeCount.textContent = String(state.forks.length);
  const conflicts = new Set(state.forks.flatMap((fork) => fork.conflict_paths || [])).size;
  elements.conflictCount.textContent = String(conflicts);
  elements.snapshotCount.textContent = String(status.snapshots || state.timeline.length);
  elements.head.textContent = status.head ? status.head.slice(0, 16) : "none";
  elements.head.title = status.head || "";
}

function renderTimeline() {
  elements.timeline.replaceChildren();
  elements.timelineCount.textContent = `${state.timeline.length} points`;
  if (!state.timeline.length) {
    elements.timeline.append(empty("No restore points yet"));
    return;
  }
  for (const snapshot of state.timeline) {
    const item = document.createElement("article");
    item.className = `timeline-item ${snapshot.materialization?.grade === "exact" ? "" : "partial"}`;
    const stem = document.createElement("span"); stem.className = "timeline-stem"; stem.append(document.createElement("i"));
    const copy = document.createElement("div"); copy.className = "timeline-copy";
    const title = document.createElement("strong"); title.textContent = snapshot.label || snapshot.trigger.replaceAll("_", " ");
    const meta = document.createElement("div"); meta.className = "timeline-meta";
    for (const value of [relativeTime(snapshot.sealed_at), snapshot.trigger, snapshot.materialization?.grade, snapshot.id.slice(0, 8)]) {
      const span = document.createElement("span"); span.textContent = value; meta.append(span);
    }
    copy.append(title, meta);
    const actions = document.createElement("div"); actions.className = "timeline-actions";
    actions.append(
      button(snapshot.pinned ? "pin-off" : "pin", snapshot.pinned ? "Unpin restore point" : "Pin restore point", () => togglePin(snapshot), snapshot.pinned ? "icon-button selected" : "icon-button"),
      button("rotate-ccw", "Preview rewind", () => previewRewind(snapshot)),
    );
    item.append(stem, copy, actions);
    elements.timeline.append(item);
  }
}

function renderForks() {
  elements.forks.replaceChildren();
  if (!state.forks.length) {
    const row = document.createElement("tr"); row.className = "empty-row";
    const cell = document.createElement("td"); cell.colSpan = 5; cell.textContent = "No active universes"; row.append(cell); elements.forks.append(row); return;
  }
  for (const fork of state.forks) {
    const row = document.createElement("tr");
    const nameCell = document.createElement("td");
    const name = document.createElement("div");
    name.className = `fork-name ${fork.radar_stale ? "stale" : fork.conflicts ? "conflicted" : ""}`;
    const mark = document.createElement("i"); mark.className = "lane-mark";
    const text = document.createElement("div");
    const strong = document.createElement("strong"); strong.textContent = fork.name;
    const id = document.createElement("code"); id.textContent = fork.fork_id.slice(0, 12); id.title = fork.fork_id;
    text.append(strong, id); name.append(mark, text); nameCell.append(name);
    const stateCell = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `state-badge ${fork.radar_stale ? "stale" : fork.conflicts ? "conflict" : "clear"}`;
    const badgeIcon = fork.radar_stale ? "triangle-alert" : fork.conflicts ? "triangle-alert" : "check";
    badge.append(icon(badgeIcon), document.createTextNode(fork.radar_stale ? "Offline" : fork.conflicts ? `${fork.conflicts} collision${fork.conflicts === 1 ? "" : "s"}` : "Clear"));
    stateCell.append(badge);
    const cost = document.createElement("td"); cost.className = "cost-cell"; cost.textContent = `${fork.tier} · ${fork.elapsed_ms} ms`;
    const head = document.createElement("td"); head.className = "head-cell"; const headCode = document.createElement("code"); headCode.textContent = fork.head_snapshot.slice(0, 12); headCode.title = fork.head_snapshot; head.append(headCode);
    const actions = document.createElement("td"); const actionSet = document.createElement("div"); actionSet.className = "row-actions";
    actionSet.append(
      button("file-diff", "Inspect changes", () => inspectFork(fork)),
      button("git-merge", "Preview merge", () => previewMerge(fork)),
      button("trash-2", "Discard universe", () => confirmDiscard(fork)),
    );
    actions.append(actionSet); row.append(nameCell, stateCell, cost, head, actions); elements.forks.append(row);
  }
}

function renderConflictLanes() {
  elements.lanes.replaceChildren();
  const paths = new Map();
  for (const fork of state.forks) for (const path of fork.conflict_paths || []) {
    if (!paths.has(path)) paths.set(path, []);
    paths.get(path).push(fork);
  }
  elements.laneSubtitle.textContent = paths.size ? `${paths.size} shared dirty path${paths.size === 1 ? "" : "s"}` : "No shared dirty paths";
  if (!paths.size) { elements.lanes.append(empty("Every active universe has a clear lane")); return; }
  for (const [path, forks] of paths) {
    const lane = document.createElement("div"); lane.className = "conflict-lane";
    const code = document.createElement("code"); code.textContent = path; code.title = path;
    const track = document.createElement("div"); track.className = "lane-track"; track.setAttribute("aria-label", forks.map((fork) => fork.name).join(", "));
    forks.forEach((fork) => {
      const branch = document.createElement("span"); branch.className = "lane-branch"; branch.title = fork.name;
      const node = document.createElement("i"); node.className = "lane-node";
      const label = document.createElement("span"); label.textContent = fork.name;
      branch.append(node, label); track.append(branch);
    });
    const claim = document.createElement("span"); claim.className = "claim-label";
    const related = [...state.events].reverse().find((event) => event.path.display === path && event.state !== "resolved");
    claim.textContent = related?.claim_state || "unclaimed";
    lane.append(code, track, claim); elements.lanes.append(lane);
  }
}

function renderEvents() {
  elements.events.replaceChildren();
  elements.eventCursor.textContent = state.cursor ? `cursor ${state.cursor.split(":").at(-1)}` : "cursor -";
  if (!state.events.length) { elements.events.append(empty("No conflict transitions")); return; }
  for (const event of [...state.events].reverse()) {
    const item = document.createElement("article"); item.className = `event-item ${event.state}`;
    const line = document.createElement("div"); line.className = "event-line";
    const eventIcon = document.createElement("span"); eventIcon.className = "event-icon"; eventIcon.append(icon(event.state === "resolved" ? "check" : "triangle-alert"));
    const title = document.createElement("strong"); title.textContent = `${capitalize(event.state)} · ${relativeTime(event.occurred_at)}`;
    line.append(eventIcon, title);
    const path = document.createElement("code"); path.textContent = event.path.display; path.title = event.path.bytes_hex;
    const forks = document.createElement("div"); forks.className = "event-forks"; forks.textContent = `${event.forks.map((fork) => fork.name).join(" ↔ ")} · ${event.claim_state}`;
    item.append(line, path, forks); elements.events.append(item);
  }
}

async function inspectFork(fork) {
  try {
    const diff = await post("/api/v1/diff", { target: fork.name });
    openDialog("Changes", fork.name, changeList(diff.changes), []);
  } catch (error) { toast(error.message, true); }
}

async function previewRewind(snapshot) {
  try {
    const plan = await post("/api/v1/rewind/plan", { snapshot: snapshot.id, paths: [] });
    const body = document.createDocumentFragment();
    const summary = document.createElement("p"); summary.className = "dialog-summary"; summary.textContent = `${plan.changes.length} path${plan.changes.length === 1 ? "" : "s"} will change. A complete undo point is created first.`;
    const id = document.createElement("code"); id.className = "dialog-id"; id.textContent = snapshot.id;
    body.append(summary, id, changeList(plan.changes));
    const apply = commandButton("rewind", "Restore this point", async (control) => {
      await guarded(control, async () => {
        await post("/api/v1/rewind/apply", { snapshot: snapshot.id, confirm_snapshot: snapshot.id, preview_digest: plan.preview_digest, paths: [], sqlite_consistent: false });
        elements.inspector.close(); toast("agit restored the workspace"); await refresh();
      });
    }, "primary");
    apply.disabled = plan.changes.length === 0;
    openDialog("Rewind preview", snapshot.label || snapshot.id.slice(0, 12), body, [apply]);
  } catch (error) { toast(error.message, true); }
}

async function previewMerge(fork) {
  try {
    const preview = await post("/api/v1/merge/preview", { fork: fork.name });
    const body = document.createDocumentFragment();
    const summary = document.createElement("p"); summary.className = "dialog-summary";
    summary.textContent = preview.conflicts.length ? `${preview.conflicts.length} conflict${preview.conflicts.length === 1 ? "" : "s"} must be resolved before merge.` : `${preview.changes} change${preview.changes === 1 ? "" : "s"} can merge cleanly.`;
    body.append(summary, changeList(preview.conflicts.map((conflict) => ({ action: conflict.kind, path: decodeByteArray(conflict.path) }))));
    const actions = [];
    if (!preview.conflicts.length) {
      const apply = commandButton("merge", "Merge verified result", async (control) => {
        await guarded(control, async () => {
          await post("/api/v1/merge/apply", { fork: fork.name, preview_digest: preview.preview_digest });
          elements.inspector.close(); toast("agit merged the verified universe"); await refresh();
        });
      }, "primary");
      apply.disabled = !state.config.merge_apply;
      apply.title = state.config.merge_apply ? "" : "Restart agit ui with --merge-check to enable merge";
      actions.push(apply);
    }
    openDialog("Merge preview", fork.name, body, actions);
  } catch (error) { toast(error.message, true); }
}

function confirmDiscard(fork) {
  const body = document.createDocumentFragment();
  const summary = document.createElement("p"); summary.className = "dialog-summary"; summary.textContent = "The universe directory and its independent timeline will be removed. The source workspace is not changed.";
  const id = document.createElement("code"); id.className = "dialog-id"; id.textContent = fork.fork_id; body.append(summary, id);
  const discard = commandButton("trash-2", "Discard universe", async (control) => {
    await guarded(control, async () => {
      await post("/api/v1/forks/discard", { fork_id: fork.fork_id, confirm_fork_id: fork.fork_id });
      elements.inspector.close(); toast("agit discarded the universe"); await refresh();
    });
  }, "danger");
  openDialog("Discard universe", fork.name, body, [discard]);
}

async function togglePin(snapshot) {
  try {
    await post("/api/v1/pins", { snapshot: snapshot.id, pinned: !snapshot.pinned });
    toast(snapshot.pinned ? "Restore point unpinned" : "Restore point pinned"); await refresh();
  } catch (error) { toast(error.message, true); }
}

function changeList(changes) {
  const list = document.createElement("ul"); list.className = "change-list";
  if (!changes?.length) { const item = document.createElement("li"); item.textContent = "No path changes"; list.append(item); return list; }
  for (const change of changes) {
    const item = document.createElement("li"); const action = document.createElement("span"); action.textContent = change.action; const path = document.createElement("code"); path.textContent = change.path; item.append(action, path); list.append(item);
  }
  return list;
}

function openDialog(kicker, title, body, actions) {
  elements.dialogKicker.textContent = kicker;
  elements.dialogTitle.textContent = title;
  elements.dialogBody.replaceChildren(body);
  elements.dialogActions.replaceChildren(...actions);
  if (!elements.inspector.open) elements.inspector.showModal();
}

function commandButton(iconName, label, handler, variant = "") {
  const control = button(iconName, label, handler, `command-button ${variant}`.trim());
  control.append(document.createTextNode(label));
  return control;
}

async function guarded(control, operation) {
  control.disabled = true;
  control.setAttribute("aria-busy", "true");
  try { await operation(); } catch (error) { toast(error.remedy ? `${error.message}. ${error.remedy}` : error.message, true); }
  finally { control.disabled = false; control.removeAttribute("aria-busy"); }
}

function empty(message) { const element = document.createElement("div"); element.className = "conflict-empty"; element.textContent = message; return element; }
function relativeTime(seconds) { const delta = Math.max(0, Math.floor(Date.now() / 1000) - Number(seconds)); if (delta < 60) return `${delta}s ago`; if (delta < 3600) return `${Math.floor(delta / 60)}m ago`; if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`; return `${Math.floor(delta / 86400)}d ago`; }
function capitalize(value) { return value ? value[0].toUpperCase() + value.slice(1) : "Event"; }
function decodeByteArray(value) { if (typeof value === "string") return value; if (!Array.isArray(value)) return "unknown"; return new TextDecoder("utf-8", { fatal: false }).decode(new Uint8Array(value)); }
function toast(message, error = false) { elements.toast.textContent = message; elements.toast.className = `toast visible ${error ? "error" : ""}`; clearTimeout(toast.timer); toast.timer = setTimeout(() => { elements.toast.className = "toast"; }, 3200); }

$("#refresh-button").addEventListener("click", refresh);
$("#dialog-close").addEventListener("click", () => elements.inspector.close());
elements.inspector.addEventListener("click", (event) => { if (event.target === elements.inspector) elements.inspector.close(); });
document.querySelectorAll(".mobile-tabs button").forEach((tab) => tab.addEventListener("click", () => {
  document.body.dataset.view = tab.dataset.view;
  document.querySelectorAll(".mobile-tabs button").forEach((button) => button.setAttribute("aria-pressed", String(button === tab)));
}));

try {
  extractToken();
  const poll = async () => {
    await refresh();
    setTimeout(poll, state.config.poll_interval_ms);
  };
  poll();
  document.addEventListener("visibilitychange", () => { if (!document.hidden) refresh(); });
} catch (error) {
  elements.connection.className = "connection error";
  elements.connection.lastChild.textContent = " Locked";
  toast(error.message, true);
}
