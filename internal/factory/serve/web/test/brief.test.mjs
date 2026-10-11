import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"
import test from "node:test"
import ts from "typescript"

// Transpile the same helper the dashboard imports, so this proof needs no
// browser and works on every Node version supported by the web build.
const source = await readFile(new URL("../src/api/brief.ts", import.meta.url), "utf8")
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
})
const { approveLabel, approveConsequence } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`)

const proof = { state: "needs_you", ready: true, gate: "proof", number: 12, kind: "issue", steps: [], pr: null }

test("no-diff approval names the issue comment in both button and confirmation", () => {
  const job = { ...proof, publish: "comment", claims: [{ proven: true }] }
  assert.equal(approveLabel(job), "Approve → comment on #12")
  assert.equal(approveConsequence(job), "Approving the proof posts a comment on #12. 1 of 1 claims have evidence.")
})

test("a branch with new commits still offers a pull request", () => {
  const job = { ...proof, publish: "pr" }
  assert.equal(approveLabel(job), "Approve → open pull request")
  assert.equal(approveConsequence(job), "Approving the proof opens the pull request.")
})

test("the publish hint does not change plan approval or offer approval on a done job", () => {
  assert.equal(approveLabel({ ...proof, gate: "plan", publish: "comment" }), "Approve plan → continue")
  assert.equal(approveLabel({ ...proof, state: "done", ready: false, publish: "comment" }), "")
})
