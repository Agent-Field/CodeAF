import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockJob } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// d5-tab-jobs-test (TJ-01..05, TX-03): an engine job opened as a terminal tab with `job` set draws the job pane,
// not the PTY pane. Header words, read-only log, Stop, Copy, trimmed line, Ask (Q5) and the reload/gone cases.
const SESSION = 'mock-session-1.jsonl';
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const jobs = (): MockJob[] => [
  { id: 7, name: 'nightly-bench', command: 'make bench', state: 'running', startedAt: ago(134_000), elapsedMs: 134_000, log: 'goos: darwin\npkg: codeaf/parse\nBenchmarkLoadAll-10\n' },
  { id: 8, name: 'make test', command: 'make test', state: 'done', exitCode: 0, startedAt: ago(240_000), elapsedMs: 120_000, log: 'all green\n' },
  { id: 9, name: 'long-log', command: 'make long', state: 'done', exitCode: 0, startedAt: ago(240_000), elapsedMs: 1000, log: 'tail line\n', cut: true },
];

async function openJob(page: Page, jobId: string, title: string) {
  await page.addInitScript(([name, job, session]) => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'job-tab', kind: 'terminal', title: name, draft: '', pinned: false, sessionFile: session, job: { jobId: job } }], groups: [], activeId: 'job-tab', closed: [], nextNumber: 2, recentIds: ['job-tab'] }));
  }, [title, jobId, SESSION] as const);
  await page.goto('/');
  await expect(page.locator('.terminal-header')).toBeVisible();
}
const state = (page: Page) => page.locator('.terminal-header .terminal-state');
const screen = (page: Page) => page.locator('.terminal-pane .xterm-rows');

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { try { localStorage.removeItem('codeaf-theme'); } catch { /* none */ } }); });

test('a running engine job shows name, job meta, Running words and its log, read-only', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '7', 'nightly-bench');
  await expect(page.locator('.terminal-title')).toHaveText('nightly-bench');
  await expect(page.locator('.terminal-meta')).toHaveText('job');
  await expect(state(page)).toHaveText(/^Running · 2m \d+s$/);
  await expect(page.locator('.terminal-mark')).toHaveAttribute('data-tone', 'running');
  await expect(screen(page)).toContainText('BenchmarkLoadAll-10');
  await expect(page.getByRole('button', { name: 'Stop' })).toBeVisible();
  // No input reaches the log: the screen is not interactive, so the engine sees no terminal write.
  await expect(page.locator('.terminal-field')).toHaveAttribute('data-kind', 'job');
});

test('Stop calls the job stop route and the header reads stopped', async ({ page }) => {
  const engine = await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '7', 'nightly-bench');
  await page.getByRole('button', { name: 'Stop' }).click();
  await expect(state(page)).toHaveText(/^stopped/);
  expect(engine.calls.some(c => c.method === 'POST' && c.path.endsWith('/jobs/7/stop'))).toBe(true);
  await expect(page.getByRole('button', { name: 'Stop' })).toHaveCount(0);
});

test('a finished job reads exit 0 · ago, the tab says exit 0, and Copy puts the log on the clipboard', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined);
  await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '8', 'make test');
  await expect(state(page)).toHaveText(/^exit 0 · 2m \d+s ago$/);
  await expect(page.getByRole('tab', { name: /make test/ }).locator('.workspace-tab-meta')).toHaveText('exit 0');
  await expect(screen(page)).toContainText('all green');
  await page.evaluate(() => { (window as unknown as { __copied: string }).__copied = ''; navigator.clipboard.writeText = async (text: string) => { (window as unknown as { __copied: string }).__copied = text; }; });
  await page.getByRole('button', { name: 'Copy output' }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { __copied: string }).__copied)).toContain('all green');
});

test('a log the engine cut says so in one muted line', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '9', 'long-log');
  const line = page.locator('.job-trim');
  await expect(line).toHaveText('Earlier output was trimmed. Showing the end of the log.');
  const [color, ink3] = await line.evaluate(el => { const probe = document.createElement('span'); probe.style.color = 'var(--ink-3)'; el.appendChild(probe); const c = getComputedStyle(probe).color; probe.remove(); return [getComputedStyle(el).color, c]; });
  expect(color).toBe(ink3);
});

test('Ask codeaf about this output opens a new conversation with the log attached', async ({ page }) => {
  const engine = await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '8', 'make test');
  await expect(screen(page)).toContainText('all green');
  const field = page.getByRole('textbox', { name: 'Ask codeaf about this output', exact: true });
  await field.fill('Is this fine?');
  await field.press('Enter');
  await expect(page.getByRole('tab')).toHaveCount(2);
  const turn = engine.calls.filter(c => c.path.endsWith('/turn')).at(-1)!.body as { text: string; files: { name: string; dataBase64: string }[] };
  expect(turn.text).toBe('Is this fine?');
  expect(turn.files[0].name).toBe('make-test-output.txt');
  expect(Buffer.from(turn.files[0].dataBase64, 'base64').toString()).toContain('all green');
});

test('after a reload the tab reattaches by job id; a job the engine lost says so in one line', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), jobs: jobs() });
  await openJob(page, '8', 'make test');
  await page.reload();
  await expect(screen(page)).toContainText('all green');
  await expect(state(page)).toHaveText(/^exit 0/);
  await page.evaluate(() => localStorage.clear());
  const gone = await page.context().newPage();
  await installMockEngine(gone, { ...plainReply(), jobs: jobs() });
  await openJob(gone, '99', 'old job');
  await expect(gone.locator('.terminal-note')).toHaveText('The engine no longer has this job.');
  await expect(gone.getByRole('textbox', { name: 'Ask codeaf about this output' })).toHaveCount(0);
});
