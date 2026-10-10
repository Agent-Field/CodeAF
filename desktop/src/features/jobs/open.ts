// Opening an engine background job as a tab (Shell 3c: a job log is the same kind of tab as an interactive shell).
// The conversation does not hold the strip, so the open is sent on the desktop tab-action event the strip already
// listens for. The listener decides whether to add a tab or select the one that already follows this job.
import { desktopTabEvent } from '../../lib/desktopMenuRoute.ts';
import { newTab, panesOf } from '../tabs/helpers.ts';
import type { Tab } from '../tabs/types.ts';

/** The job to show. `jobId` is the decimal the jobs route uses, as text, so job 9 is `"9"`. */
export type JobOpen = { sessionFile: string; jobId: string; title: string };

/** `background` is the ⌘-click / middle-click gesture: the new tab does not take the strip's focus. */
export type JobOpenOptions = { background?: boolean };

/** What the strip receives. `tab` is the terminal tab to add when this job is not already showing. */
export type OpenJobDetail = {
  type: 'open-job';
  sessionFile: string;
  jobId: string;
  title: string;
  background: boolean;
  tab: Tab;
};

/** What the strip should do with one request: open the tab, select the one already showing it, or leave the strip alone. */
export type JobTabAction =
  | { type: 'select'; id: string }
  | { type: 'open'; tab: Tab; background: boolean };

const text = (value: unknown): value is string => typeof value === 'string';

/**
 * A terminal tab that follows one engine job. The kind is terminal because Shell 3c draws one glyph for an
 * interactive shell and a job log. `target.terminalId` stays unset: that field attaches a PTY, and this job is
 * the engine's own background job, named by `job`.
 */
export function jobTab(spec: Pick<JobOpen, 'sessionFile' | 'jobId' | 'title'>): Tab {
  return newTab({
    kind: 'terminal',
    title: spec.title,
    titleSource: 'manual',
    sessionFile: spec.sessionFile,
    job: { jobId: spec.jobId },
  });
}

/** The pane already showing this conversation's job, including one pane of a split. */
export function findJobPane(tabs: readonly Tab[], spec: Pick<JobOpen, 'sessionFile' | 'jobId'>): string | undefined {
  for (const tab of tabs) {
    const hit = panesOf(tab).find(pane => pane.kind === 'terminal' && pane.sessionFile === spec.sessionFile && pane.job?.jobId === spec.jobId);
    if (hit) return hit.id;
  }
  return undefined;
}

/** True when `value` is a job-open detail this module built. A menu command is a string and fails here. */
export function isOpenJobDetail(value: unknown): value is OpenJobDetail {
  if (!value || typeof value !== 'object') return false;
  const detail = value as Partial<OpenJobDetail>;
  const tab = detail.tab;
  if (detail.type !== 'open-job' || !text(detail.sessionFile) || !detail.sessionFile) return false;
  if (!text(detail.jobId) || !detail.jobId || !text(detail.title) || typeof detail.background !== 'boolean') return false;
  if (!tab || tab.kind !== 'terminal' || tab.sessionFile !== detail.sessionFile || tab.job?.jobId !== detail.jobId) return false;
  return text(tab.id) && !!tab.id && text(tab.title) && text(tab.draft) && typeof tab.pinned === 'boolean';
}

/**
 * Select the pane that already shows this job, or open `detail.tab`.
 * A background request for a job that is already showing changes nothing: the person asked to stay put.
 */
export function jobOpenAction(tabs: readonly Tab[], detail: OpenJobDetail): JobTabAction | undefined {
  const existing = findJobPane(tabs, detail);
  if (existing) return detail.background ? undefined : { type: 'select', id: existing };
  return { type: 'open', tab: detail.tab, background: detail.background };
}

/** The detail for one open, or undefined when the session or the job id is blank (nothing to find the tab by later). */
export function openJobDetail(spec: JobOpen, options: JobOpenOptions = {}): OpenJobDetail | undefined {
  const sessionFile = spec.sessionFile.trim();
  const jobId = spec.jobId.trim();
  if (!sessionFile || !jobId) return undefined;
  const title = spec.title.trim();
  return { type: 'open-job', sessionFile, jobId, title, background: options.background === true, tab: jobTab({ sessionFile, jobId, title }) };
}

/**
 * Opens `spec` as a terminal tab with `job` set, by dispatching on the desktop tab-action event.
 * Returns the tab that would be added. When a tab for this job is already on the strip, the listener selects
 * that tab and does not add the returned one. A blank session or job id opens nothing.
 */
export function openJobTab(spec: JobOpen, options: JobOpenOptions = {}): Tab | undefined {
  const detail = openJobDetail(spec, options);
  if (!detail) return undefined;
  globalThis.window?.dispatchEvent(new CustomEvent(desktopTabEvent, { detail }));
  return detail.tab;
}
