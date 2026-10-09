/** Canonical read-only transport from session.PlanTaskRow. No engine policy lives here. */
export type PlanTaskRow = {
 ID: string; Title: string; Status: string; Parent?: string; Hold?: string;
 Stopped?: boolean; Interrupted?: boolean; Archived?: boolean;
 // Optional until the canonical read projection carries status-independent flags.
 Paused?: boolean; Waiting?: boolean; Waits?: readonly string[];
 Seat?: string; Note?: string; Steps?: number; Started?: string; Ended?: string;
 Program?: string; Stage?: string;
 Live?: { Step?: number; Command?: string; Since?: string };
 Done?: number; Running?: number; Queued?: number; Failed?: number; Total?: number;
};
export type PlanTaskPage = {
 Row: PlanTaskRow; Description?: string; Result?: string; Checks?: readonly string[];
 Notes?: readonly { Author?: string; Person?: boolean; Body: string; At?: string }[];
 Steps?: readonly { step?: number; command?: string; observation?: string }[];
};
export type TaskStanding = { label: string; phase: 'working'|'waiting'|'completed'|'stopped'|'failed'|'staged'; attention: boolean };
export function taskStanding(row: PlanTaskRow): TaskStanding {
 if (row.Stopped) return { label: 'Stopped', phase: 'stopped', attention: false };
 if (row.Interrupted) return { label: 'Interrupted', phase: 'stopped', attention: true };
 if (row.Paused || row.Status === 'paused') return { label: 'Needs your input', phase: 'waiting', attention: true };
 if (row.Waiting) return { label: 'Waiting', phase: 'waiting', attention: false };
 if (row.Hold && ['ready','running'].includes(row.Status)) return { label: 'Queued', phase: 'staged', attention: false };
 switch (row.Status.trim()) {
  case 'pending': return { label: row.Waits?.length ? 'Waiting on prerequisites' : 'Queued', phase: 'staged', attention: false };
  case 'ready': return { label: 'Ready', phase: 'staged', attention: false };
  case 'claimed': return { label: 'Assigned', phase: 'working', attention: false };
  case 'running': return { label: 'Running', phase: 'working', attention: false };
  case 'done': return { label: 'Completed', phase: 'completed', attention: false };
  case 'failed': return { label: 'Incomplete', phase: 'failed', attention: true };
  case 'cancelled': return { label: 'Cancelled', phase: 'stopped', attention: false };
  default: return { label: 'State unavailable', phase: 'staged', attention: false };
 }
}
export function planForest(rows: readonly PlanTaskRow[]): { roots: PlanTaskRow[]; children: Map<string, PlanTaskRow[]> } {
 const ids = new Set(rows.map(row => row.ID));
 const children = new Map<string, PlanTaskRow[]>();
 const roots: PlanTaskRow[] = [];
 for (const row of rows) {
  if (!row.Parent || !ids.has(row.Parent) || row.Parent === row.ID) roots.push(row);
  else children.set(row.Parent, [...(children.get(row.Parent) ?? []), row]);
 }
 // A corrupt cycle must not hide the entire plan or recurse forever.
 const seen = new Set<string>();
 const visit = (row: PlanTaskRow) => { if (seen.has(row.ID)) return; seen.add(row.ID); (children.get(row.ID) ?? []).forEach(visit); };
 roots.forEach(visit);
 rows.forEach(row => { if (!seen.has(row.ID)) { roots.push(row); visit(row); } });
 return { roots, children };
}

/** Canonical upstream-to-downstream typed dependency, independent of containment. */
export type PlanDependency = { from: string; to: string; kind: 'feeds_into'|'blocks'|'suggests' };
