// Rendering and interaction. Content comes only from the AtlasData passed in.
import {
  BoxRenderable,
  FrameBufferRenderable,
  TextRenderable,
  bold,
  fg,
  t,
  type CliRenderer,
  type KeyEvent,
  type MouseEvent,
  type RGBA,
  type StyledText,
} from "@opentui/core"
import type { AtlasData, AtlasEdge, AtlasNode, FlowStep } from "./types"
import { kindName, rgba, theme } from "./theme"

export interface Rect { x: number; y: number; w: number; h: number }

export interface AppState {
  /** Keyboard selection (tab cycles it). */
  selected: string | null
  /** Node whose detail pane is open. */
  detail: string | null
  /** Index into data.flows, or null for the plain overview. */
  flow: number | null
  step: number
  playing: boolean
  help: boolean
  /** Box centres as fractions of the map area; drag edits these. */
  positions: Record<string, { x: number; y: number }>
}

export interface AtlasApp {
  state: AppState
  /** Current on-screen rectangle of a node (global cells). */
  nodeRect(id: string): Rect | undefined
  openDetail(id: string | null): void
  setFlow(index: number | null): void
  stepBy(delta: number): void
  togglePlay(): void
  toggleHelp(): void
  /** Re-run layout and redraw. */
  refresh(): void
  destroy(): void
}

const PLAY_MS = 1400
const SIDE_PANE_W = 46

type Dir = "r" | "l" | "u" | "d"

export function createApp(renderer: CliRenderer, data: AtlasData): AtlasApp {
  const nodeById = new Map(data.nodes.map((n) => [n.id, n]))
  const state: AppState = {
    selected: null,
    detail: null,
    flow: null,
    step: 0,
    playing: false,
    help: false,
    positions: Object.fromEntries(data.nodes.map((n) => [n.id, { ...n.pos }])),
  }
  let timer: ReturnType<typeof setInterval> | null = null
  let destroyed = false
  let detailShown: string | null = null
  let helpFilled = false

  // ---------- static tree ----------
  const canvas = new FrameBufferRenderable(renderer, {
    id: "atlas-canvas",
    position: "absolute",
    left: 0,
    top: 0,
    width: Math.max(1, renderer.width),
    height: Math.max(1, renderer.height),
    zIndex: 0,
  })
  renderer.root.add(canvas)

  const header = new BoxRenderable(renderer, {
    id: "atlas-header", position: "absolute", left: 0, top: 0, width: "100%", height: 1,
    backgroundColor: theme.panel, flexDirection: "row", paddingLeft: 1, gap: 2, zIndex: 20,
  })
  const titleText = new TextRenderable(renderer, { content: "", selectable: false })
  header.add(titleText)
  const tabTexts = data.flows.map((f, i) => {
    const tx = new TextRenderable(renderer, {
      id: `atlas-tab-${f.id}`, content: "", selectable: false,
      onMouseDown: (e: MouseEvent) => { e.preventDefault(); setFlow(state.flow === i ? null : i) },
    })
    header.add(tx)
    return tx
  })
  renderer.root.add(header)

  const status = new BoxRenderable(renderer, {
    id: "atlas-status", position: "absolute", left: 0, bottom: 0, width: "100%", height: 1,
    backgroundColor: theme.panel, paddingLeft: 1, zIndex: 20,
  })
  const statusText = new TextRenderable(renderer, { content: "", selectable: false, fg: theme.muted })
  status.add(statusText)
  renderer.root.add(status)

  // Flow panel (bottom).
  const flowPanel = new BoxRenderable(renderer, {
    id: "atlas-flow", position: "absolute", left: 0, bottom: 1, width: "100%", height: 5,
    backgroundColor: theme.panel, border: ["top"], borderColor: theme.accent, borderStyle: "single",
    flexDirection: "row", paddingLeft: 1, paddingRight: 1, gap: 1, zIndex: 30, visible: false,
  })
  const prevBtn = new TextRenderable(renderer, {
    id: "atlas-prev", content: t`${fg(theme.accent)("◀ prev")}`, selectable: false,
    onMouseDown: (e: MouseEvent) => { e.preventDefault(); stopPlay(); stepBy(-1) },
  })
  const flowBody = new TextRenderable(renderer, { id: "atlas-flow-body", content: "", selectable: false, flexGrow: 1, flexShrink: 1, wrapMode: "word" })
  const nextBtn = new TextRenderable(renderer, {
    id: "atlas-next", content: t`${fg(theme.accent)("next ▶")}`, selectable: false,
    onMouseDown: (e: MouseEvent) => { e.preventDefault(); stopPlay(); stepBy(1) },
  })
  flowPanel.add(prevBtn)
  flowPanel.add(flowBody)
  flowPanel.add(nextBtn)
  renderer.root.add(flowPanel)

  // Detail pane (right side, or full overlay when narrow).
  const detailPane = new BoxRenderable(renderer, {
    id: "atlas-detail", position: "absolute", right: 0, top: 1, width: SIDE_PANE_W, height: 10,
    backgroundColor: theme.panel, border: true, borderStyle: "rounded", borderColor: theme.edgeHi,
    titleAlignment: "left", bottomTitle: " esc close ", bottomTitleAlignment: "right",
    paddingLeft: 1, paddingRight: 1, zIndex: 40, visible: false, overflow: "hidden",
  })
  renderer.root.add(detailPane)

  // Help overlay.
  const helpBox = new BoxRenderable(renderer, {
    id: "atlas-help", position: "absolute", left: 2, top: 2, width: 60, height: 20,
    backgroundColor: theme.panelHi, border: true, borderStyle: "rounded", borderColor: theme.accent,
    title: " help · legend ", titleAlignment: "center", bottomTitle: " ? or esc closes ", bottomTitleAlignment: "center",
    paddingLeft: 2, paddingRight: 2, zIndex: 100, visible: false, overflow: "hidden",
  })
  renderer.root.add(helpBox)

  // Node boxes.
  interface NodeView { node: AtlasNode; box: BoxRenderable; label: TextRenderable; sub: TextRenderable; rect: Rect }
  const views = new Map<string, NodeView>()
  let drag: { id: string; offX: number; offY: number; moved: boolean } | null = null

  for (const node of data.nodes) {
    const color = theme.kinds[node.kind]
    const box = new BoxRenderable(renderer, {
      id: `node-${node.id}`, position: "absolute", left: 0, top: 0, width: 10, height: 4,
      backgroundColor: theme.bg, border: true, borderStyle: "rounded", borderColor: color,
      paddingLeft: 1, paddingRight: 1, zIndex: 10, overflow: "hidden",
      onMouseDown: (e: MouseEvent) => {
        if (e.button !== 0) return
        e.preventDefault()
        const v = views.get(node.id)!
        drag = { id: node.id, offX: e.x - v.rect.x, offY: e.y - v.rect.y, moved: false }
        state.selected = node.id
        refresh()
      },
      onMouseDrag: (e: MouseEvent) => {
        if (!drag || drag.id !== node.id) return
        moveNodeTo(node.id, e.x - drag.offX, e.y - drag.offY)
        drag.moved = true
      },
      onMouseUp: () => {
        if (!drag || drag.id !== node.id) return
        const clicked = !drag.moved
        drag = null
        if (clicked) openDetail(node.id)
      },
    })
    const label = new TextRenderable(renderer, { content: node.label, fg: theme.fg, selectable: false, wrapMode: "none" })
    const sub = new TextRenderable(renderer, { content: node.short ?? "", fg: theme.dim, selectable: false, wrapMode: "none" })
    box.add(label)
    box.add(sub)
    renderer.root.add(box)
    views.set(node.id, { node, box, label, sub, rect: { x: 0, y: 0, w: 10, h: 4 } })
  }

  // ---------- layout ----------
  function geometry() {
    const W = renderer.width
    const H = renderer.height
    const compact = W < 100 || H < 30
    const flowH = state.flow === null ? 0 : compact ? 4 : 5
    const sidePane = state.detail !== null && W >= 100
    const mapTop = 1
    const mapW = Math.max(10, W - (sidePane ? SIDE_PANE_W : 0))
    const mapH = Math.max(4, H - 2 - flowH)
    return { W, H, compact, flowH, sidePane, mapTop, mapW, mapH }
  }

  function nodeSize(node: AtlasNode, compact: boolean) {
    const text = Math.max(node.label.length, compact ? 0 : (node.short ?? "").length)
    const w = compact ? Math.min(text + 4, 18) : Math.max(text + 4, 14)
    return { w, h: compact ? 3 : 4 }
  }

  function moveNodeTo(id: string, x: number, y: number) {
    const g = geometry()
    const v = views.get(id)!
    const { w, h } = v.rect
    const cx = clamp(x, 0, g.mapW - w)
    const cy = clamp(y, g.mapTop, g.mapTop + g.mapH - h)
    state.positions[id] = {
      x: g.mapW > w ? (cx + w / 2) / g.mapW : 0.5,
      y: g.mapH > h ? (cy - g.mapTop + h / 2) / g.mapH : 0.5,
    }
    refresh()
  }

  function layoutNodes(g: ReturnType<typeof geometry>) {
    for (const v of views.values()) {
      const { w, h } = nodeSize(v.node, g.compact)
      const p = state.positions[v.node.id]!
      const x = clamp(Math.round(p.x * g.mapW - w / 2), 0, Math.max(0, g.mapW - w))
      const y = clamp(Math.round(g.mapTop + p.y * g.mapH - h / 2), g.mapTop, Math.max(g.mapTop, g.mapTop + g.mapH - h))
      v.rect = { x, y, w, h }
      v.box.left = x
      v.box.top = y
      v.box.width = w
      v.box.height = h
      v.sub.visible = !g.compact && !!v.node.short
      const color = theme.kinds[v.node.kind]
      const inFlow = activeStep()
      const isActive = inFlow && (inFlow.from === v.node.id || inFlow.to === v.node.id)
      const isSel = state.selected === v.node.id || state.detail === v.node.id
      v.box.borderStyle = isSel ? "double" : "rounded"
      v.box.borderColor = isActive ? theme.accent : color
      v.box.backgroundColor = isActive ? theme.accentBg : isSel ? theme.panelHi : theme.bg
      v.label.content = isSel || isActive ? t`${bold(fg(isActive ? theme.accent : color)(v.node.label))}` : t`${fg(color)(v.node.label)}`
    }
  }

  // ---------- edges ----------
  function center(r: Rect) { return { x: r.x + Math.floor(r.w / 2), y: r.y + Math.floor(r.h / 2) } }

  function isHorizontal(a: Rect, b: Rect) {
    const ac = center(a), bc = center(b)
    const dx = bc.x - ac.x, dy = bc.y - ac.y
    return Math.abs(dx) > Math.abs(dy) * 2.5 && (b.x >= a.x + a.w + 2 || a.x >= b.x + b.w + 2)
  }

  /** Which side of `a` a route towards `b` leaves from. */
  function sideOf(a: Rect, b: Rect): Dir {
    const ac = center(a), bc = center(b)
    if (isHorizontal(a, b)) return bc.x > ac.x ? "r" : "l"
    return bc.y >= ac.y ? "d" : "u"
  }

  /** Orthogonal Z-route from the edge of a to just outside b; offsets spread ports along a side. */
  function route(a: Rect, b: Rect, offA = 0, offB = 0): { x: number; y: number }[] {
    const ac = center(a), bc = center(b)
    const dx = bc.x - ac.x, dy = bc.y - ac.y
    if (isHorizontal(a, b)) {
      const sx = dx > 0 ? a.x + a.w : a.x - 1
      const ex = dx > 0 ? b.x - 1 : b.x + b.w
      const sy = clamp(ac.y + offA, a.y + 1, a.y + a.h - 2)
      const ey = clamp(bc.y + offB, b.y + 1, b.y + b.h - 2)
      const mx = Math.round((sx + ex) / 2)
      return [{ x: sx, y: sy }, { x: mx, y: sy }, { x: mx, y: ey }, { x: ex, y: ey }]
    }
    const sy = dy >= 0 ? a.y + a.h : a.y - 1
    const ey = dy >= 0 ? b.y - 1 : b.y + b.h
    const sx = clamp(ac.x + offA, a.x + 1, a.x + a.w - 2)
    const ex = clamp(bc.x + offB, b.x + 1, b.x + b.w - 2)
    const my = Math.round((sy + ey) / 2)
    return [{ x: sx, y: sy }, { x: sx, y: my }, { x: ex, y: my }, { x: ex, y: ey }]
  }

  /** Port offsets: edges sharing a node side are spread apart, ordered by where they go. */
  function portOffsets(edges: { from: string; to: string }[]) {
    const groups = new Map<string, { i: number; end: "a" | "b"; key: number }[]>()
    edges.forEach((e, i) => {
      const a = views.get(e.from)?.rect, b = views.get(e.to)?.rect
      if (!a || !b) return
      const sa = sideOf(a, b)
      const sb: Dir = isHorizontal(a, b) ? (sa === "r" ? "l" : "r") : sa === "d" ? "u" : "d"
      const along = (r: Rect, side: Dir) => (side === "l" || side === "r" ? center(r).y : center(r).x)
      const push = (k: string, item: { i: number; end: "a" | "b"; key: number }) => {
        if (!groups.has(k)) groups.set(k, [])
        groups.get(k)!.push(item)
      }
      push(`${e.from}:${sa}`, { i, end: "a", key: along(b, sa) })
      push(`${e.to}:${sb}`, { i, end: "b", key: along(a, sb) })
    })
    const off = edges.map(() => ({ a: 0, b: 0 }))
    for (const [k, items] of groups) {
      const vertSide = k.endsWith(":l") || k.endsWith(":r")
      const spacing = vertSide ? 1 : 3
      items.sort((p, q) => p.key - q.key)
      items.forEach((it, j) => { off[it.i]![it.end] = Math.round((j - (items.length - 1) / 2) * spacing) })
    }
    return off
  }

  function rasterize(pts: { x: number; y: number }[]) {
    const cells: { x: number; y: number }[] = [{ ...pts[0]! }]
    for (let i = 1; i < pts.length; i++) {
      const to = pts[i]!
      let cur = cells[cells.length - 1]!
      while (cur.x !== to.x || cur.y !== to.y) {
        const nx = cur.x + Math.sign(to.x - cur.x)
        const ny = nx === cur.x ? cur.y + Math.sign(to.y - cur.y) : cur.y
        cur = { x: nx, y: ny }
        cells.push(cur)
      }
    }
    return cells
  }

  const dirOf = (a: { x: number; y: number }, b: { x: number; y: number }): Dir =>
    b.x > a.x ? "r" : b.x < a.x ? "l" : b.y > a.y ? "d" : "u"
  const LINE: Record<Dir, string> = { r: "─", l: "─", u: "│", d: "│" }
  const HEAD: Record<Dir, string> = { r: "▶", l: "◀", u: "▲", d: "▼" }
  const CORNER: Record<string, string> = {
    rd: "┐", ru: "┘", ld: "┌", lu: "└", dr: "└", dl: "┘", ur: "┌", ul: "┐",
  }

  function drawEdge(fromId: string, toId: string, label: string, color: string, showLabel: boolean, labelBg?: string, offA = 0, offB = 0) {
    const a = views.get(fromId)?.rect, b = views.get(toId)?.rect
    if (!a || !b) return
    const cells = rasterize(route(a, b, offA, offB))
    const fb = canvas.frameBuffer
    const c = rgba(color), bgc = rgba(theme.bg)
    for (let i = 0; i < cells.length; i++) {
      const cell = cells[i]!
      let ch: string
      const din = i > 0 ? dirOf(cells[i - 1]!, cell) : undefined
      const dout = i < cells.length - 1 ? dirOf(cell, cells[i + 1]!) : undefined
      if (!dout) ch = HEAD[din ?? "r"]
      else if (!din || din === dout) ch = LINE[dout]
      else ch = CORNER[din + dout] ?? "┼"
      put(cell.x, cell.y, ch, c, bgc)
    }
    if (showLabel && label && cells.length > 2) {
      const mid = cells[Math.floor(cells.length / 2)]!
      const maxLen = Math.max(4, Math.min(30, renderer.width - 2))
      const text = ` ${label.length > maxLen ? label.slice(0, maxLen - 1) + "…" : label} `
      const x = clamp(mid.x - Math.floor(text.length / 2), 0, Math.max(0, canvas.frameBuffer.width - text.length))
      fb.drawText(text, x, mid.y, c, rgba(labelBg ?? theme.bg))
    }
  }

  function put(x: number, y: number, ch: string, c: RGBA, b: RGBA) {
    const fb = canvas.frameBuffer
    if (x < 0 || y < 0 || x >= fb.width || y >= fb.height) return
    fb.setCell(x, y, ch, c, b)
  }

  function activeStep(): FlowStep | undefined {
    if (state.flow === null) return undefined
    return data.flows[state.flow]?.steps[state.step]
  }

  function edgeForStep(s: FlowStep): AtlasEdge | undefined {
    if (s.edge) return data.edges.find((e) => e.id === s.edge)
    return data.edges.find((e) => (e.from === s.from && e.to === s.to) || (e.from === s.to && e.to === s.from))
  }

  function drawEdges(g: ReturnType<typeof geometry>) {
    canvas.frameBuffer.clear(rgba(theme.bg))
    const step = activeStep()
    const stepEdge = step ? edgeForStep(step) : undefined
    const focus = state.detail ?? state.selected
    const offs = portOffsets(data.edges)
    data.edges.forEach((e, i) => {
      if (e === stepEdge) return
      const touches = focus !== null && (e.from === focus || e.to === focus)
      const color = step ? theme.edge : touches ? theme.edgeHi : focus ? theme.edge : theme.muted
      drawEdge(e.from, e.to, e.label, color, !step && (touches || !g.compact), undefined, offs[i]!.a, offs[i]!.b)
    })
    if (step) {
      // Travel along the overview edge's ports when the step follows one.
      const i = stepEdge ? data.edges.indexOf(stepEdge) : -1
      const o = i >= 0 ? offs[i]! : { a: 0, b: 0 }
      const same = stepEdge && stepEdge.from === step.from
      drawEdge(step.from, step.to, `${state.step + 1}. ${step.message}`, theme.accent, true, theme.accentBg, same ? o.a : o.b, same ? o.b : o.a)
    }
  }

  // ---------- panels ----------
  function updateChrome(g: ReturnType<typeof geometry>) {
    titleText.content = t`${bold(fg(theme.accent)("◆ " + data.title))}${g.W >= 110 && data.subtitle ? fg(theme.dim)("  " + data.subtitle) : ""}`
    tabTexts.forEach((tx, i) => {
      const f = data.flows[i]!
      const name = g.compact ? f.title.split(" ")[0]! : f.title
      tx.content = state.flow === i
        ? t`${bold(fg(theme.bg)(` ${i + 1} ${name} `))}`
        : t`${fg(theme.muted)(`${i + 1}`)} ${fg(theme.fg)(name)}`
      tx.bg = state.flow === i ? theme.accent : undefined
    })
    const hints = state.flow !== null
      ? "←/→ step · p play · 0 overview · tab select · ? help · q quit"
      : "drag boxes · click/enter details · tab select · 1-" + data.flows.length + " flows · ? help · q quit"
    statusText.content = g.W < hints.length + 2 ? hints.slice(0, g.W - 3) + "…" : hints

    // flow panel
    flowPanel.visible = state.flow !== null
    if (state.flow !== null) {
      flowPanel.height = g.flowH
      const f = data.flows[state.flow]!
      const s = f.steps[state.step]!
      const from = nodeById.get(s.from)?.label ?? s.from
      const to = nodeById.get(s.to)?.label ?? s.to
      const dots = f.steps.map((_, i) => (i === state.step ? "●" : i < state.step ? "•" : "·")).join("")
      flowBody.content = t`${bold(fg(theme.accent)(f.title))} ${fg(theme.dim)(`step ${state.step + 1}/${f.steps.length}`)} ${fg(theme.accent)(dots)}${state.playing ? fg(theme.kinds.go)("  ▶ playing") : ""}
${bold(fg(theme.fg)(`${from} → ${to}:`))} ${fg(theme.fg)(s.message)}${s.note && !g.compact ? fg(theme.muted)(`\n${s.note}`) : ""}`
    }

    // detail pane
    detailPane.visible = state.detail !== null
    if (state.detail === null) detailShown = null
    if (state.detail !== null) {
      const n = nodeById.get(state.detail)
      if (n) {
        if (g.sidePane) {
          detailPane.width = SIDE_PANE_W
          detailPane.top = 1
          detailPane.height = g.mapH
        } else {
          detailPane.width = g.W
          detailPane.top = 1
          detailPane.height = g.mapH
        }
        detailPane.title = ` ${n.label} `
        detailPane.titleColor = theme.kinds[n.kind]
        detailPane.borderColor = theme.kinds[n.kind]
        if (detailShown !== n.id) { setLines(detailPane, detailContent(n), true); detailShown = n.id }
      }
    }

    // help
    helpBox.visible = state.help
    if (state.help) {
      if (!helpFilled) { setLines(helpBox, helpContent(), false); helpFilled = true }
      const w = Math.min(64, g.W - 4)
      const h = Math.min(24, g.H - 2)
      helpBox.width = w
      helpBox.height = h
      helpBox.left = Math.max(0, Math.floor((g.W - w) / 2))
      helpBox.top = Math.max(0, Math.floor((g.H - h) / 2))
    }
  }

  type Line = StyledText | string

  /** Replace a container's children with one Text per line. */
  function setLines(box: BoxRenderable, lines: Line[], wrap: boolean) {
    for (const child of box.getChildren()) child.destroyRecursively()
    for (const line of lines) {
      box.add(new TextRenderable(renderer, { content: line, fg: theme.fg, wrapMode: wrap ? "word" : "none", selectable: wrap, flexShrink: 0 }))
    }
  }

  function detailContent(n: AtlasNode): Line[] {
    const color = theme.kinds[n.kind]
    const out = data.edges.filter((e) => e.from === n.id)
    const inn = data.edges.filter((e) => e.to === n.id)
    const L: Line[] = [t`${fg(color)(kindName[n.kind])}`, "", t`${fg(theme.fg)(n.summary)}`]
    for (const d of n.details ?? []) L.push(t`${fg(theme.fg)("• " + d)}`)
    if (n.files.length) {
      L.push("", t`${bold(fg(theme.accent)("Files"))}`)
      for (const f of n.files) {
        L.push(t`${fg(theme.kinds.spec)(f.path)}`)
        if (f.symbols?.length) L.push(t`${fg(theme.muted)("  " + f.symbols.join(", "))}`)
        if (f.note) L.push(t`${fg(theme.dim)("  " + f.note)}`)
      }
    }
    if (out.length || inn.length) {
      L.push("", t`${bold(fg(theme.accent)("Talks to"))}`)
      const row = (arrow: string, other: string, e: AtlasEdge) =>
        t`${fg(theme.fg)(arrow + " " + (nodeById.get(other)?.label ?? other))} ${fg(theme.muted)(e.label)}${fg(theme.dim)(e.detail ? " — " + e.detail : "")}`
      for (const e of out) L.push(row("→", e.to, e))
      for (const e of inn) L.push(row("←", e.from, e))
    }
    return L
  }

  function helpContent(): Line[] {
    const L: Line[] = [t`${bold(fg(theme.fg)("Keys & mouse"))}`]
    for (const [key, what] of [
      ["drag a box", "move it; arrows follow"],
      ["click a box", "open its detail pane"],
      ["tab / S-tab", "select next / previous box"],
      ["enter", "open detail for selection"],
      ["1-" + data.flows.length + " / f", "open a flow / cycle flows"],
      ["← →  h l", "step the flow (or click prev/next)"],
      ["p / space", "play / pause the flow"],
      ["0 / esc", "back to overview / close pane"],
      ["?", "toggle this help"],
      ["q / ctrl+c", "quit and restore the terminal"],
    ] as const) L.push(t`${bold(fg(theme.accent)(key.padEnd(14)))}${fg(theme.fg)(what)}`)
    L.push("", t`${bold(fg(theme.fg)("Legend"))}`)
    for (const kind of Object.keys(theme.kinds) as (keyof typeof theme.kinds)[]) {
      L.push(t`${fg(theme.kinds[kind])("■ ")}${fg(theme.fg)(kindName[kind])}`)
    }
    L.push(t`${fg(theme.accent)("─▶ active flow step")}   ${fg(theme.edgeHi)("─▶ edges of the selected box")}`)
    return L
  }

  // ---------- actions ----------
  function refresh() {
    if (destroyed) return
    const g = geometry()
    if (canvas.frameBuffer.width !== g.W || canvas.frameBuffer.height !== g.H) {
      canvas.width = Math.max(1, g.W)
      canvas.height = Math.max(1, g.H)
    }
    layoutNodes(g)
    drawEdges(g)
    updateChrome(g)
    renderer.requestRender()
  }

  function openDetail(id: string | null) {
    state.detail = id
    if (id) state.selected = id
    refresh()
  }

  function setFlow(index: number | null) {
    stopPlay(false)
    state.flow = index !== null && data.flows[index] ? index : null
    state.step = 0
    refresh()
  }

  function stepBy(delta: number) {
    if (state.flow === null) return
    const n = data.flows[state.flow]!.steps.length
    state.step = (state.step + delta + n) % n
    refresh()
  }

  function stopPlay(redraw = true) {
    if (timer) clearInterval(timer)
    timer = null
    state.playing = false
    if (redraw) refresh()
  }

  function togglePlay() {
    if (state.flow === null) setFlow(0)
    if (state.playing) return stopPlay()
    state.playing = true
    timer = setInterval(() => stepBy(1), PLAY_MS)
    refresh()
  }

  function toggleHelp() { state.help = !state.help; refresh() }

  function cycleSelection(delta: number) {
    const ids = data.nodes.map((n) => n.id)
    const i = state.selected ? ids.indexOf(state.selected) : -1
    state.selected = ids[(i + delta + ids.length) % ids.length] ?? null
    if (state.detail) state.detail = state.selected
    refresh()
  }

  const onKey = (key: KeyEvent) => {
    if (destroyed) return
    if (key.name === "q" || (key.ctrl && key.name === "c")) return destroy(true)
    if (key.sequence === "?") return toggleHelp()
    if (key.name === "escape") {
      if (state.help) return toggleHelp()
      if (state.detail) return openDetail(null)
      if (state.flow !== null) return setFlow(null)
      state.selected = null
      return refresh()
    }
    if (state.help) return
    if (key.name === "tab") return cycleSelection(key.shift ? -1 : 1)
    if (key.name === "return" || key.name === "enter") {
      if (state.selected) return openDetail(state.detail === state.selected ? null : state.selected)
      return
    }
    if (key.name === "right" || key.name === "l") { stopPlay(false); return stepBy(1) }
    if (key.name === "left" || key.name === "h") { stopPlay(false); return stepBy(-1) }
    if (key.name === "p" || key.name === "space") return togglePlay()
    if (key.name === "f") return setFlow(state.flow === null ? 0 : (state.flow + 1) % data.flows.length)
    if (key.name === "0" || key.name === "o") return setFlow(null)
    const digit = Number(key.name)
    if (Number.isInteger(digit) && digit >= 1 && digit <= data.flows.length) return setFlow(digit - 1)
  }
  const onResize = () => refresh()
  renderer.keyInput.on("keypress", onKey)
  renderer.on("resize", onResize)
  renderer.once("destroy", () => destroy(false))

  /** Stop timers and listeners; `alsoRenderer` restores the terminal too. */
  function destroy(alsoRenderer = true) {
    if (destroyed) return
    destroyed = true
    if (timer) clearInterval(timer)
    timer = null
    renderer.keyInput.off("keypress", onKey)
    renderer.off("resize", onResize)
    if (alsoRenderer) renderer.destroy()
  }

  refresh()

  return {
    state,
    nodeRect: (id) => views.get(id)?.rect,
    openDetail,
    setFlow,
    stepBy,
    togglePlay,
    toggleHelp,
    refresh,
    destroy: () => destroy(true),
  }
}

function clamp(v: number, lo: number, hi: number) { return Math.max(lo, Math.min(hi, v)) }
