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
  dialogBody: $("#dialog-body"), dialogActions: $("#dialog-actions"), toast: $("#toast"), tooltip: $("#tooltip"),
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
  control.dataset.tooltip = label;
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
  elements.workspace.dataset.tooltip = `Source folder: ${path}`;
  elements.protection.textContent = status.watcher_running ? "Live" : "Manual";
  elements.universeCount.textContent = String(state.forks.length);
  const conflicts = new Set(state.forks.flatMap((fork) => fork.conflict_paths || [])).size;
  elements.conflictCount.textContent = String(conflicts);
  elements.snapshotCount.textContent = String(status.snapshots || state.timeline.length);
  elements.head.textContent = status.head ? status.head.slice(0, 16) : "none";
  elements.head.dataset.tooltip = status.head ? `Full snapshot ID: ${status.head}` : "No snapshot yet";
}

function renderTimeline() {
  elements.timeline.replaceChildren();
  elements.timelineCount.textContent = `${state.timeline.length} points`;
  if (!state.timeline.length) {
    elements.timeline.append(empty("No restore points yet"));
    return;
  }
  for (const snapshot of state.timeline) {
    const current = snapshot.id === state.status?.status?.head;
    const item = document.createElement("article");
    item.className = `timeline-item ${snapshot.materialization?.grade === "exact" ? "" : "partial"} ${current ? "current" : ""}`;
    const stem = document.createElement("span"); stem.className = "timeline-stem"; stem.append(document.createElement("i"));
    const copy = document.createElement("div"); copy.className = "timeline-copy";
    const title = document.createElement("strong"); title.textContent = snapshotTitle(snapshot);
    if (current) { const badge = document.createElement("span"); badge.className = "current-badge"; badge.textContent = "Current"; title.append(badge); }
    const meta = document.createElement("div"); meta.className = "timeline-meta";
    for (const value of [relativeTime(snapshot.sealed_at), triggerLabel(snapshot.trigger), snapshot.materialization?.grade, snapshot.id.slice(0, 8)]) {
      const span = document.createElement("span"); span.textContent = value; meta.append(span);
    }
    copy.append(title, meta);
    const actions = document.createElement("div"); actions.className = "timeline-actions";
    actions.append(button(snapshot.pinned ? "pin-off" : "pin", snapshot.pinned ? "Unpin restore point" : "Keep this restore point permanently", () => togglePin(snapshot), snapshot.pinned ? "icon-button selected" : "icon-button"));
    if (!current) actions.append(button("rotate-ccw", "Preview rewind to this point", () => previewRewind(snapshot)));
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
    const entityIcon = document.createElement("span"); entityIcon.className = "entity-icon universe-icon"; entityIcon.append(icon("folder-git-2"));
    const text = document.createElement("div");
    const strong = document.createElement("strong"); strong.textContent = fork.name;
    const kind = document.createElement("span"); kind.className = "entity-kind"; kind.textContent = "Isolated universe";
    text.append(strong, kind); name.append(entityIcon, text); name.dataset.tooltip = `${fork.name} is an isolated copy of the source folder`; nameCell.append(name);
    const stateCell = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `state-badge ${fork.radar_stale ? "stale" : fork.conflicts ? "conflict" : "clear"}`;
    const badgeIcon = fork.radar_stale ? "triangle-alert" : fork.conflicts ? "triangle-alert" : "check";
    badge.append(icon(badgeIcon), document.createTextNode(fork.radar_stale ? "Offline" : fork.conflicts ? `${fork.conflicts} collision${fork.conflicts === 1 ? "" : "s"}` : "Clear"));
    badge.dataset.tooltip = fork.radar_stale
      ? "agit cannot currently read this universe; its last sealed state is preserved"
      : fork.conflicts
        ? "This universe and at least one other universe changed the same path differently"
        : "No other active universe changed the same path differently";
    stateCell.append(badge);
    const cost = document.createElement("td"); cost.className = "cost-cell"; cost.textContent = `${formatTier(fork.tier)} · ${fork.elapsed_ms} ms`; cost.dataset.tooltip = `agit created this isolated workspace in ${fork.elapsed_ms} ms using ${formatTierLong(fork.tier)}`;
    const head = document.createElement("td"); head.className = "head-cell"; const headCode = document.createElement("code"); headCode.textContent = fork.head_snapshot.slice(0, 12); headCode.dataset.tooltip = `Latest universe snapshot: ${fork.head_snapshot}`; head.append(headCode);
    const actions = document.createElement("td"); const actionSet = document.createElement("div"); actionSet.className = "row-actions";
    actionSet.append(
      button("info", `About ${fork.name}`, () => inspectUniverse(fork)),
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
  elements.laneSubtitle.textContent = paths.size ? `${paths.size} overlapping path${paths.size === 1 ? "" : "s"}` : "No overlapping changes";
  if (!paths.size) { elements.lanes.append(empty("Every active universe has a clear lane")); return; }
  for (const [path, forks] of paths) {
    const lane = document.createElement("div"); lane.className = "conflict-lane";
    const pathEntity = document.createElement("div"); pathEntity.className = "path-entity"; pathEntity.dataset.tooltip = `Every named universe changed this file differently`;
    const pathIcon = document.createElement("span"); pathIcon.className = "entity-icon file-icon"; pathIcon.append(icon("file-code-2"));
    const code = document.createElement("code"); code.textContent = path;
    pathEntity.append(pathIcon, code);
    const track = document.createElement("div"); track.className = "lane-track"; track.setAttribute("aria-label", forks.map((fork) => fork.name).join(", "));
    forks.forEach((fork) => {
      const branch = document.createElement("span"); branch.className = "lane-branch"; branch.dataset.tooltip = `${fork.name} has its own version of ${path}`;
      const node = document.createElement("i"); node.className = "lane-node";
      const label = document.createElement("span"); label.textContent = fork.name;
      branch.append(node, label); track.append(branch);
    });
    const claim = document.createElement("span"); claim.className = "claim-label";
    const related = [...state.events].reverse().find((event) => event.path.display === path && event.state !== "resolved");
    claim.textContent = related?.claim_state === "covered" ? "claim exists" : "unclaimed";
    claim.dataset.tooltip = related?.claim_state === "covered" ? "At least one agent declared intent to work on this path" : "No agent declared intent to work on this path";
    lane.append(pathEntity, track, claim); elements.lanes.append(lane);
  }
}

function renderEvents() {
  elements.events.replaceChildren();
  elements.eventCursor.textContent = `${state.events.length} event${state.events.length === 1 ? "" : "s"}`;
  if (!state.events.length) { elements.events.append(empty("No conflict transitions")); return; }
  for (const event of [...state.events].reverse()) {
    const item = document.createElement("article"); item.className = `event-item ${event.state}`;
    const line = document.createElement("div"); line.className = "event-line";
    const eventIcon = document.createElement("span"); eventIcon.className = "event-icon"; eventIcon.append(icon(event.state === "resolved" ? "check" : "triangle-alert"));
    const title = document.createElement("strong"); title.textContent = `${event.state === "resolved" ? "Conflict resolved" : "Conflict detected"} · ${relativeTime(event.occurred_at)}`;
    line.append(eventIcon, title);
    const path = document.createElement("div"); path.className = "event-path"; path.dataset.tooltip = `Changed file · raw path bytes: ${event.path.bytes_hex}`;
    const pathIcon = document.createElement("span"); pathIcon.className = "entity-icon file-icon"; pathIcon.append(icon("file-code-2"));
    const pathCode = document.createElement("code"); pathCode.textContent = event.path.display;
    path.append(pathIcon, pathCode);
    const forks = document.createElement("div"); forks.className = "event-forks"; forks.textContent = `Between ${event.forks.map((fork) => fork.name).join(" and ")}`;
    const meaning = document.createElement("div"); meaning.className = "event-meaning"; meaning.textContent = event.state === "resolved" ? "These universes no longer overlap on this path" : `Both changed this path differently · ${event.claim_state === "covered" ? "claim exists" : "unclaimed"}`;
    item.append(line, path, forks, meaning); elements.events.append(item);
  }
}

function inspectUniverse(fork) {
  const body = document.createDocumentFragment();
  const summary = document.createElement("p"); summary.className = "dialog-summary";
  summary.textContent = `${fork.name} is a complete isolated copy of the source folder. Its files stay separate until you merge or discard this universe.`;
  body.append(summary, detailList([
    ["Type", "Isolated universe"],
    ["Location", fork.destination],
    ["Universe ID", fork.fork_id],
    ["Started from", fork.base_snapshot],
    ["Latest snapshot", fork.head_snapshot],
    ["Contents", `${fork.files} files · ${fork.directories} folders · ${formatBytes(fork.logical_bytes)}`],
    ["Creation", `${formatTierLong(fork.tier)} · ${fork.elapsed_ms} ms`],
    ["Overlap", fork.conflicts ? `${fork.conflicts} collision${fork.conflicts === 1 ? "" : "s"}` : "No overlapping paths"],
  ]));
  openDialog("Universe details", fork.name, body, []);
}

async function inspectFork(fork) {
  try {
    const diff = await post("/api/v1/diff", { target: fork.name });
    const body = document.createDocumentFragment();
    const summary = document.createElement("p"); summary.className = "dialog-summary";
    summary.textContent = `${diff.changes.length} path${diff.changes.length === 1 ? "" : "s"} differ from the source folder.`;
    body.append(summary, changeList(diff.changes));
    openDialog("Universe changes", fork.name, body, []);
  } catch (error) { toast(error.message, true); }
}

async function previewRewind(snapshot) {
  try {
    const plan = await post("/api/v1/rewind/plan", { snapshot: snapshot.id, paths: [] });
    const body = document.createDocumentFragment();
    const summary = document.createElement("p"); summary.className = "dialog-summary";
    summary.textContent = plan.changes.length
      ? `${plan.changes.length} path${plan.changes.length === 1 ? "" : "s"} will change. agit creates a complete undo point before restoring anything.`
      : "The source folder already matches this restore point. Rewind would not change any files.";
    const id = document.createElement("code"); id.className = "dialog-id"; id.textContent = snapshot.id;
    body.append(summary, id, changeList(plan.changes));
    const apply = commandButton("rotate-ccw", "Restore this point", async (control) => {
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
      const apply = commandButton("git-merge", "Merge verified result", async (control) => {
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

function detailList(rows) {
  const list = document.createElement("dl"); list.className = "detail-list";
  for (const [label, value] of rows) {
    const term = document.createElement("dt"); term.textContent = label;
    const detail = document.createElement("dd"); detail.textContent = value;
    list.append(term, detail);
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
function triggerLabel(trigger) {
  return ({
    initial: "Initial protection", manual: "Manual checkpoint", watcher: "Automatic checkpoint",
    pre_rewind: "Before rewind", fork_base: "Universe started", agent_run: "Agent checkpoint",
    merge_source: "Universe merge source", pre_merge: "Before merge", merge: "Merged universe",
    claim: "Path claimed", release: "Path released",
  })[trigger] || trigger.replaceAll("_", " ");
}
function snapshotTitle(snapshot) {
  const label = snapshot.label;
  if (!label) return triggerLabel(snapshot.trigger);
  if (snapshot.trigger === "fork_base" && label.startsWith("fork base: ")) return `Universe started: ${label.slice(11)}`;
  if (snapshot.trigger === "pre_merge" && label.startsWith("before merge from ")) return `Before merge: ${label.slice(18)}`;
  if (snapshot.trigger === "merge_source" && label.startsWith("merge source for ")) return `Merge source: ${label.slice(17)}`;
  return label;
}
function formatTier(tier) { return ({ "native-cow": "CoW", "atomic-copy": "Atomic copy", "streaming-copy": "Copy" })[tier] || tier; }
function formatTierLong(tier) { return ({ "native-cow": "native copy-on-write", "atomic-copy": "atomic filesystem copy", "streaming-copy": "streaming copy" })[tier] || tier; }
function formatBytes(bytes) { const value = Number(bytes || 0); if (value < 1024) return `${value} B`; if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`; if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`; return `${(value / 1024 ** 3).toFixed(1)} GiB`; }
function decodeByteArray(value) { if (typeof value === "string") return value; if (!Array.isArray(value)) return "unknown"; return new TextDecoder("utf-8", { fatal: false }).decode(new Uint8Array(value)); }
function toast(message, error = false) { elements.toast.textContent = message; elements.toast.className = `toast visible ${error ? "error" : ""}`; clearTimeout(toast.timer); toast.timer = setTimeout(() => { elements.toast.className = "toast"; }, 3200); }

let activeTooltip = null;
function showTooltip(trigger) {
  const message = trigger?.dataset.tooltip;
  if (!message) return;
  activeTooltip?.removeAttribute("aria-describedby");
  activeTooltip = trigger;
  elements.tooltip.textContent = message;
  elements.tooltip.classList.add("visible");
  trigger.setAttribute("aria-describedby", "tooltip");
  const triggerRect = trigger.getBoundingClientRect();
  const tooltipRect = elements.tooltip.getBoundingClientRect();
  let top = triggerRect.top - tooltipRect.height - 8;
  if (top < 8) top = triggerRect.bottom + 8;
  const left = Math.min(Math.max(8, triggerRect.left + triggerRect.width / 2 - tooltipRect.width / 2), window.innerWidth - tooltipRect.width - 8);
  elements.tooltip.style.left = `${left}px`;
  elements.tooltip.style.top = `${top}px`;
}
function hideTooltip() {
  activeTooltip?.removeAttribute("aria-describedby");
  activeTooltip = null;
  elements.tooltip.classList.remove("visible");
}

$("#refresh-button").addEventListener("click", refresh);
$("#dialog-close").addEventListener("click", () => elements.inspector.close());
elements.inspector.addEventListener("click", (event) => { if (event.target === elements.inspector) elements.inspector.close(); });
document.addEventListener("pointerover", (event) => {
  const trigger = event.target.closest?.("[data-tooltip]");
  if (trigger && trigger !== activeTooltip) showTooltip(trigger);
});
document.addEventListener("pointerout", (event) => {
  if (activeTooltip && !activeTooltip.contains(event.relatedTarget)) hideTooltip();
});
document.addEventListener("focusin", (event) => {
  const trigger = event.target.closest?.("[data-tooltip]");
  if (trigger) showTooltip(trigger);
});
document.addEventListener("focusout", (event) => {
  if (activeTooltip && !activeTooltip.contains(event.relatedTarget)) hideTooltip();
});
window.addEventListener("resize", hideTooltip);
document.addEventListener("scroll", hideTooltip, true);
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
