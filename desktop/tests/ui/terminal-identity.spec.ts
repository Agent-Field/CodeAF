import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// A terminal tab names its shell on the pane itself (sessionFile + target.terminalId). These journeys prove
// that a copy under a new id, a handoff, a reload and a tab saved before the change all show the SAME engine
// terminal, and that none of them starts a second process or hides a missing terminal.
const SESSION = 'mock-session-1.jsonl';
const WORKSPACE = 'codeaf.desktop.workspace.v1';
const BINDINGS = 'codeaf.desktop.terminals.v1';
const scenario = (): Scenario => ({
  ...plainReply(),
  terminals: [{ id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'goos: darwin\r\n\x1b[32mok\x1b[0m codeaf/parse 4.2s\r\n' }],
});
type Seed = { tabs: object[]; bindings?: object };
const named = (id: string, terminalId: string) => ({ id, kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false, sessionFile: SESSION, target: { terminalId } });

/** Seeds the workspace once per test (not on reload), optionally with legacy bindings. */
async function seed(page: Page, { tabs, bindings }: Seed) {
  await page.addInitScript(([key, bkey, data, saved]) => {
    if (localStorage.getItem(key)) return;
    const ids = (data as { id: string }[]).map(t => t.id);
    localStorage.setItem(key, JSON.stringify({ tabs: data, groups: [], activeId: ids[0], closed: [], nextNumber: ids.length + 1, recentIds: ids }));
    if (saved) localStorage.setItem(bkey, JSON.stringify(saved));
  }, [WORKSPACE, BINDINGS, tabs, bindings ?? null] as const);
}
const screenText = (page: Page) => page.locator('.terminal-pane .xterm-rows');
const header = (page: Page) => page.locator('.terminal-header');
const starts = (engine: Awaited<ReturnType<typeof installMockEngine>>) => engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals'));
const savedPanes = (page: Page) => page.evaluate(key => (JSON.parse(localStorage.getItem(key)!).tabs as { id: string; sessionFile?: string; target?: object }[]), WORKSPACE);

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { try { localStorage.removeItem('codeaf-theme'); } catch { /* none */ } }); });

test('a copy of a bound terminal under a new pane id shows the same output and starts nothing', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await seed(page, { tabs: [named('orig', 'job-1'), named('copy-with-new-id', 'job-1')] });
  await page.goto('/');
  await expect(screenText(page)).toContainText('ok codeaf/parse 4.2s');
  await page.getByRole('tab').nth(1).click();
  await expect(header(page).locator('.terminal-title')).toHaveText('nightly-bench');
  await expect(screenText(page)).toContainText('ok codeaf/parse 4.2s');
  await expect(page.locator('.terminal-note')).toHaveCount(0);
  expect(starts(engine)).toHaveLength(0);
});

test('a tab received by handoff keeps the terminal named by its target and replays it', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  // What the receiving window stores after paneFromHandoff: the kind, the title, the session file and the target; no bridge session id, no binding.
  await seed(page, { tabs: [{ id: 'received', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false, sessionFile: SESSION, target: { terminalId: 'job-1' } }] });
  await page.goto('/');
  await expect(screenText(page)).toContainText('goos: darwin');
  expect(starts(engine)).toHaveLength(0);
  expect(await page.evaluate(key => localStorage.getItem(key), BINDINGS)).toBeNull();
});

test('a started shell survives a reload with no binding store at all, and never restarts', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Backquote');
  await expect(screenText(page)).toContainText('mock$');
  expect(starts(engine)).toHaveLength(1);
  const [, shell] = await savedPanes(page);
  expect(shell.sessionFile).toBeTruthy();
  expect(shell.target).toEqual({ terminalId: 'mock-term-1' });
  await page.evaluate(key => localStorage.removeItem(key), BINDINGS);
  await page.reload();
  await expect(header(page).locator('.terminal-title')).toHaveText('zsh');
  await expect(screenText(page)).toContainText('mock$');
  expect(starts(engine)).toHaveLength(1);
});

test('a tab saved before the durable target still opens its shell, and is upgraded in place', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await seed(page, {
    tabs: [{ id: 'old', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false }],
    bindings: { old: { sessionFile: SESSION, terminalId: 'job-1' } },
  });
  await page.goto('/');
  await expect(screenText(page)).toContainText('ok codeaf/parse 4.2s');
  await expect.poll(async () => (await savedPanes(page))[0].target).toEqual({ terminalId: 'job-1' });
  expect((await savedPanes(page))[0].sessionFile).toBe(SESSION);
  expect(starts(engine)).toHaveLength(0);
});

test('a terminal the engine no longer has says so, and no second process is started', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await seed(page, { tabs: [named('lost', 'job-vanished')] });
  await page.goto('/');
  await expect(page.locator('.terminal-note')).toContainText('This terminal is gone.');
  expect(starts(engine)).toHaveLength(0);
});

test('Stop reaches the original terminal once, and the stopped state is still there after a reload', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await seed(page, { tabs: [named('job', 'job-1')] });
  await page.goto('/');
  await expect(header(page).locator('.terminal-state')).toHaveText(/^Running/);
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect(header(page).locator('.terminal-state')).toHaveText(/^closed/);
  await page.reload();
  await expect(header(page).locator('.terminal-state')).toHaveText(/^closed/);
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/terminals/job-1/close'))).toHaveLength(1);
  expect(starts(engine)).toHaveLength(0);
});
