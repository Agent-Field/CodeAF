import { RGBA } from "@opentui/core"
import type { NodeKind } from "./types"

// One palette for the whole app (Tokyo-Night-ish: dark, restrained, one hot accent).
export const theme = {
  bg: "#16161e",
  panel: "#1f2335",
  panelHi: "#292e42",
  fg: "#c0caf5",
  dim: "#565f89",
  muted: "#737aa2",
  edge: "#3b4261",
  edgeHi: "#7aa2f7",
  accent: "#ff9e64",
  accentBg: "#3d2a1f",
  kinds: {
    machine: "#7aa2f7",
    go: "#9ece6a",
    service: "#e0af68",
    engine: "#bb9af7",
    spec: "#7dcfff",
  } satisfies Record<NodeKind, string>,
}

export const kindName: Record<NodeKind, string> = {
  machine: "machine",
  go: "codeaf (Go)",
  service: "relay / hosted",
  engine: "furrow (Rust)",
  spec: "spec / doc",
}

export const rgba = (hex: string) => RGBA.fromHex(hex)
