import { expect, type Page } from '@playwright/test';
import type { MockJob, Scenario } from './mock-engine';
import { plainReply } from './scenarios';

// Fixtures for the engine job tab specs: the jobs the mock lists, the workspace that opens one as a terminal tab with
// `job` set, and the world feed (rows + `jobs` rollups) that keeps a tab current with no session stream.
export const JOB_SESSION = 'mock-session-1.jsonl';
/** The mock's conversation has no journal folder, so the world files its jobs under the session handle. */
export const JOB_CHAT = 'mock-1';
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

export const engineJobs = (): MockJob[] => [
  { id: 7, name: 'nightly-bench', command: 'make bench', state: 'running', startedAt: ago(134_000), elapsedMs: 134_000, log: 'goos: darwin\npkg: codeaf/parse\nBenchmarkLoadAll-10\n' },
  { id: 8, name: 'make test', command: 'make test', state: 'done', exitCode: 0, startedAt: ago(240_000), elapsedMs: 120_000, log: 'all green\n' },
];

/** A scenario whose world feed is live, so `engine.setWorld({ jobs })` reaches the open window. */
export function jobsScenario(): Scenario {
  return { ...plainReply(), jobs: engineJobs(), world: { rows: [], items: [], jobs: [] } };
}

/** The rollup the world feed sends when job 7 ends by itself while the tab's own poll still reads "running". */
export const finishedRollup = () => [{
  chatId: JOB_CHAT, running: 0,
  jobs: [{ id: 7, name: 'nightly-bench', command: 'make bench', state: 'done', exitCode: 2, startedAt: ago(200_000), elapsedMs: 150_000 }],
}];

/** Seeds one job tab (first load only, so a reload keeps whatever the window did since) and waits for its header. */
export async function openJobTab(page: Page, jobId: string, title: string) {
  await page.addInitScript(([name, job, session]) => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'job-tab', kind: 'terminal', title: name, draft: '', pinned: false, sessionFile: session, job: { jobId: job } }], groups: [], activeId: 'job-tab', closed: [], nextNumber: 2, recentIds: ['job-tab'] }));
  }, [title, jobId, JOB_SESSION] as const);
  await page.goto('/');
  await expect(page.locator('.terminal-header')).toBeVisible();
}
