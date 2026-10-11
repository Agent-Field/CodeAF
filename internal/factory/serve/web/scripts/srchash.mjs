// Writes dist/.srchash: the hash of every input of the build, so a Go test
// (internal/factory/serve/web/embed_test.go, which computes the same hash
// the same way) can fail when dist/ is stale and say "run make web".
//
// The hash: for each input file in byte order of its slash path, the line
// "<path> <sha256 hex of its bytes>\n"; the sha256 hex of all those lines.
// Change the inputs or the recipe here and in embed_test.go together.
import { createHash } from "node:crypto"
import { readdirSync, readFileSync, writeFileSync } from "node:fs"
import { join, relative, sep } from "node:path"
import { fileURLToPath } from "node:url"

const root = fileURLToPath(new URL("..", import.meta.url))
const files = [
  "package.json", "package-lock.json", "index.html", "vite.config.ts", "components.json",
  "tsconfig.json", "tsconfig.app.json", "tsconfig.node.json", "scripts/srchash.mjs",
]

function walk(dir) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)
    if (e.isDirectory()) walk(p)
    else files.push(relative(root, p).split(sep).join("/"))
  }
}
walk(join(root, "src"))

const lines = files
  .sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
  .map((f) => `${f} ${createHash("sha256").update(readFileSync(join(root, f))).digest("hex")}\n`)
const sum = createHash("sha256").update(lines.join("")).digest("hex")
writeFileSync(join(root, "dist", ".srchash"), sum + "\n")
console.log(`dist/.srchash ${sum}`)
