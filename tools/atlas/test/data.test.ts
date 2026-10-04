import { expect, test } from "bun:test"
import { existsSync } from "node:fs"
import { join } from "node:path"
import { atlas } from "../src/data"

const repoRoot = join(import.meta.dir, "..", "..", "..")

test("every file path in the atlas exists in the repo", () => {
  const missing = atlas.nodes.flatMap((n) => n.files.map((f) => f.path)).filter((p) => !existsSync(join(repoRoot, p)))
  expect(missing).toEqual([])
})

test("edges and flow steps reference real nodes", () => {
  const ids = new Set(atlas.nodes.map((n) => n.id))
  const edgeIds = new Set(atlas.edges.map((e) => e.id))
  for (const e of atlas.edges) expect(ids.has(e.from) && ids.has(e.to)).toBe(true)
  for (const f of atlas.flows) {
    expect(f.steps.length).toBeGreaterThan(0)
    for (const s of f.steps) {
      expect(ids.has(s.from) && ids.has(s.to)).toBe(true)
      if (s.edge) expect(edgeIds.has(s.edge)).toBe(true)
    }
  }
})

test("the atlas covers pairing, continue-chat and furrow flows", () => {
  const flows = atlas.flows.map((f) => f.id)
  for (const id of ["pairing", "continue", "furrow"]) expect(flows).toContain(id)
  const kinds = new Set(atlas.nodes.map((n) => n.kind))
  for (const k of ["machine", "go", "service", "engine"]) expect(kinds.has(k as never)).toBe(true)
  expect(atlas.nodes.every((n) => !n.summary.includes("PLACEHOLDER"))).toBe(true)
})
