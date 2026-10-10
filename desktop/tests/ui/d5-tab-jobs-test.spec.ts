import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { expectAccessible } from './contracts';
import { engineJobs, finishedRollup, jobsScenario, openJobTab } from './support/jobs-fixtures';

// d5-tab-jobs-test (TJ-01..07): the engine job tab end to end, in both browsers and both schemes. The pane's own
// cases live in d5-tab-jobs-pane.spec.ts; this file adds the world-feed case, the schemes and the narrow width.
const state = (page: import('@playwright/test').Page) => page.locator('.terminal-header .terminal-state');
const screen = (page: import('@playwright/test').Page) => page.locator('.terminal-pane .xterm-rows');

test('an engine job opens as a job tab with its header and log', async ({ page }) => {
  await installMockEngine(page, jobsScenario());
  await openJobTab(page, '7', 'nightly-bench');
  await expect(page.locator('.terminal-title')).toHaveText('nightly-bench');
  await expect(page.locator('.terminal-meta')).toHaveText('job');
  await expect(state(page)).toHaveText(/^Running · /);
  await expect(screen(page)).toContainText('BenchmarkLoadAll-10');
  await expect(page.getByRole('tab', { name: /nightly-bench/ })).toBeVisible();
});

test('Stop posts to the job route and the header turns to exit words', async ({ page }) => {
  const engine = await installMockEngine(page, jobsScenario());
  await openJobTab(page, '7', 'nightly-bench');
  await page.getByRole('button', { name: 'Stop' }).click();
  await expect(state(page)).toHaveText(/^stopped/);
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/jobs/7/stop'))).toHaveLength(1);
});

test('a world jobs record updates a background job tab with no session stream', async ({ page }) => {
  const engine = await installMockEngine(page, jobsScenario());
  await openJobTab(page, '7', 'nightly-bench');
  await expect(state(page)).toHaveText(/^Running · /);
  engine.setWorld({ jobs: finishedRollup() });
  await expect(state(page)).toHaveText(/^exit 2 · /);
  await expect(page.getByRole('tab', { name: /nightly-bench/ }).locator('.workspace-tab-meta')).toHaveText('exit 2');
  await expect(page.getByRole('button', { name: 'Stop' })).toHaveCount(0);
});

test('Ask codeaf about this output opens a new conversation with the log attached', async ({ page }) => {
  const engine = await installMockEngine(page, jobsScenario());
  await openJobTab(page, '8', 'make test');
  await expect(screen(page)).toContainText('all green');
  const field = page.getByRole('textbox', { name: 'Ask codeaf about this output', exact: true });
  await field.fill('Is this fine?');
  await field.press('Enter');
  await expect(page.getByRole('tab')).toHaveCount(2);
  const turn = engine.calls.filter(c => c.path.endsWith('/turn')).at(-1)!.body as { files: { name: string; dataBase64: string }[] };
  expect(turn.files[0].name).toBe('make-test-output.txt');
  expect(Buffer.from(turn.files[0].dataBase64, 'base64').toString()).toContain('all green');
});

test('a reload reattaches the job tab; a job the engine lost says so', async ({ page }) => {
  await installMockEngine(page, jobsScenario());
  await openJobTab(page, '8', 'make test');
  await page.reload();
  await expect(screen(page)).toContainText('all green');
  await expect(state(page)).toHaveText(/^exit 0/);
  const gone = await page.context().newPage();
  await installMockEngine(gone, { ...jobsScenario(), jobs: engineJobs().filter(j => j.id !== 8) });
  await gone.evaluate(() => localStorage.clear()).catch(() => undefined);
  await openJobTab(gone, '8', 'make test');
  await expect(gone.locator('.terminal-note')).toHaveText('The engine no longer has this job.');
});

for (const scheme of ['light', 'dark'] as const) {
  test(`the job tab passes axe in ${scheme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await installMockEngine(page, jobsScenario());
    await openJobTab(page, '7', 'nightly-bench');
    await expect(screen(page)).toContainText('BenchmarkLoadAll-10');
    await expectAccessible(page);
  });
}

test('at 320px the job tab has no horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 700 });
  await installMockEngine(page, jobsScenario());
  await openJobTab(page, '7', 'nightly-bench');
  await expect(screen(page)).toContainText('BenchmarkLoadAll-10');
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});
