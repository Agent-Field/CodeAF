import { createCliRenderer } from "@opentui/core"
import { createApp } from "./app"
import { atlas } from "./data"

const renderer = await createCliRenderer({ exitOnCtrlC: true, targetFps: 30 })
try {
  createApp(renderer, atlas)
} catch (err) {
  renderer.destroy()
  throw err
}
