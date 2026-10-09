import type { PlanTaskPage, PlanTaskRow } from './plan-model';
/** Explicit UI fixture only. Never returned as an engine snapshot. */
export const planPreviewRows: readonly PlanTaskRow[] = [
 { ID: 'preview-root', Title: 'Prepare the release', Status: 'running', Done: 1, Running: 1, Queued: 2, Failed: 0, Total: 4 },
 { ID: 'preview-review', Parent: 'preview-root', Title: 'Review the changes', Status: 'done', Note: 'The review is recorded.' },
 { ID: 'preview-tests', Parent: 'preview-root', Title: 'Check cross-platform behavior', Status: 'running', Live: { Command: 'Review the Linux and macOS checks' } },
 { ID: 'preview-copy', Parent: 'preview-root', Title: 'Confirm the release notes', Status: 'ready', Paused: true, Note: 'Choose which changes belong in this release.' },
 { ID: 'preview-package', Parent: 'preview-root', Title: 'Prepare the package', Status: 'pending', Waits: ['preview-tests', 'preview-copy'] },
];
export function planPreviewPage(id: string): PlanTaskPage|null {
 const row = planPreviewRows.find(value => value.ID === id);
 return row ? { Row: row, Description: 'A read-only example of a conversation task. No work is running.', ...(id === 'preview-review' ? { Result: 'Example review completed.', Checks: ['Example accessibility review'] } : {}) } : null;
}
