import { afterEach, describe, expect, test } from "bun:test"
import { createTestRenderer } from "@opentui/core/testing"
import { createApp, type AtlasApp } from "../src/app"
import { atlas } from "../src/data"

type Setup = Awaited<ReturnType<typeof createTestRenderer>>
let setup: Setup | undefined
let app: AtlasApp | undefined

async function start(width = 120, height = 35) {
  setup = await createTestRenderer({ width, height })
  app = createApp(setup.renderer, atlas)
  await setup.renderOnce()
  return { setup, app }
}

afterEach(() => {
  app?.destroy()
  setup?.renderer.destroy()
  app = undefined
  setup = undefined
})

const first = atlas.nodes[0]!

describe("atlas", () => {
  test("renders the overview with every node label", async () => {
    const { setup } = await start()
    const frame = setup.captureCharFrame()
    if (process.env.ATLAS_PRINT) console.log(frame)
    expect(frame).toContain(atlas.title)
    for (const n of atlas.nodes) expect(frame).toContain(n.label)
    expect(frame).toMatch(/[▶◀▲▼]/) // arrowheads drawn
  })

  test("degrades at 80x24: nodes stay on screen", async () => {
    const { setup, app } = await start(80, 24)
    const frame = setup.captureCharFrame()
    if (process.env.ATLAS_PRINT) console.log(frame)
    for (const n of atlas.nodes) {
      const r = app.nodeRect(n.id)!
      expect(r.x).toBeGreaterThanOrEqual(0)
      expect(r.x + r.w).toBeLessThanOrEqual(80)
      expect(r.y + r.h).toBeLessThanOrEqual(23)
      expect(frame).toContain(n.label)
    }
  })

  test("clicking a node opens its detail pane", async () => {
    const { setup, app } = await start()
    const r = app.nodeRect(first.id)!
    await setup.mockMouse.click(r.x + 2, r.y + 1)
    const frame = await setup.waitForFrame((f) => f.includes("esc close"))
    if (process.env.ATLAS_PRINT) console.log(frame)
    expect(app.state.detail).toBe(first.id)
    expect(frame).toContain(first.summary.slice(0, 20))
  })

  test("tab + enter opens a detail pane from the keyboard", async () => {
    const { setup, app } = await start()
    setup.mockInput.pressTab()
    setup.mockInput.pressEnter()
    await setup.renderOnce()
    expect(app.state.detail).toBe(first.id)
    expect(setup.captureCharFrame()).toContain("esc close")
  })

  test("dragging a node moves it and does not open detail", async () => {
    const { setup, app } = await start()
    const before = { ...app.nodeRect(first.id)! }
    await setup.mockMouse.drag(before.x + 2, before.y + 1, before.x + 22, before.y + 9)
    await setup.renderOnce()
    const after = app.nodeRect(first.id)!
    expect(after.x).toBe(before.x + 20)
    expect(after.y).toBe(before.y + 8)
    expect(app.state.detail).toBeNull()
    const lines = setup.captureCharFrame().split("\n")
    expect(lines[after.y + 1]!.slice(after.x, after.x + after.w)).toContain(first.label)
  })

  test("a flow steps forward with arrow keys and highlights the step", async () => {
    const { setup, app } = await start()
    const flow = atlas.flows[0]!
    setup.mockInput.typeText("1")
    await setup.renderOnce()
    expect(app.state.flow).toBe(0)
    expect(setup.captureCharFrame()).toContain(`step 1/${flow.steps.length}`)
    setup.mockInput.pressArrow("right")
    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    if (process.env.ATLAS_PRINT) console.log(frame)
    expect(app.state.step).toBe(1)
    expect(frame).toContain(`step 2/${flow.steps.length}`)
    expect(frame).toContain(flow.steps[1]!.message.slice(0, 20))
    setup.mockInput.pressArrow("left")
    await setup.renderOnce()
    expect(app.state.step).toBe(0)
  })

  test("clicking next steps the flow; p toggles play", async () => {
    const { setup, app } = await start()
    app.setFlow(1)
    await setup.renderOnce()
    const lines = setup.captureCharFrame().split("\n")
    const y = lines.findIndex((l) => l.includes("next ▶"))
    const x = lines[y]!.indexOf("next ▶")
    await setup.mockMouse.click(x + 1, y)
    await setup.renderOnce()
    expect(app.state.step).toBe(1)
    setup.mockInput.typeText("p")
    expect(app.state.playing).toBe(true)
    setup.mockInput.typeText("p")
    expect(app.state.playing).toBe(false)
  })

  test("? shows the legend; q destroys the renderer", async () => {
    const { setup, app } = await start()
    setup.mockInput.typeText("?")
    await setup.renderOnce()
    const frame = setup.captureCharFrame()
    if (process.env.ATLAS_PRINT) console.log(frame)
    expect(frame).toContain("Legend")
    let destroyed = false
    setup.renderer.once("destroy", () => (destroyed = true))
    setup.mockInput.typeText("q")
    expect(destroyed).toBe(true)
    void app
  })
})
