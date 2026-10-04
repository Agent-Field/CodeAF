// The diagram schema. Everything the explorer shows comes from one AtlasData
// value (src/data.ts); the renderer (src/app.ts) never hard-codes content.

/** What a box is; drives its colour and its legend entry. */
export type NodeKind = "machine" | "go" | "service" | "engine" | "spec"

/** A real file in the repository, relative to the repo root. */
export interface FileRef {
  path: string
  /** Key functions / types in that file worth knowing. */
  symbols?: string[]
  note?: string
}

export interface AtlasNode {
  id: string
  /** Box title; keep it under ~18 columns. */
  label: string
  /** One-line subtitle shown inside the box when there is room. */
  short?: string
  kind: NodeKind
  /** Centre of the box as fractions (0..1) of the map area. */
  pos: { x: number; y: number }
  /** Plain-words description of what it does. */
  summary: string
  /** Extra bullet points for the detail pane. */
  details?: string[]
  files: FileRef[]
}

export interface AtlasEdge {
  id: string
  from: string
  to: string
  /** Short arrow label, a few words. */
  label: string
  /** Longer explanation for the detail pane (what crosses, encrypted?). */
  detail?: string
}

export interface FlowStep {
  from: string
  to: string
  /** What is sent, in plain words; shown in the flow panel. */
  message: string
  /** Optional id of the overview edge this step travels along. */
  edge?: string
  /** Why / where in code. */
  note?: string
}

export interface AtlasFlow {
  id: string
  title: string
  summary: string
  steps: FlowStep[]
}

export interface AtlasData {
  title: string
  subtitle?: string
  nodes: AtlasNode[]
  edges: AtlasEdge[]
  flows: AtlasFlow[]
}
