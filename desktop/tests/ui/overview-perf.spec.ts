import { expect, test, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// Loading is measured separately from the shell's entrance animation; normal motion is covered by overview.spec.ts.
test.use({ reducedMotion: 'reduce' });

/** Measure the visible DOM commit, keeping later animation-frame scheduling separate from content loading. */
async function openVisible(page: Page, selector: string, text: string) {
  return page.evaluate(async ({ selector, text }) => {
    const start = performance.now();
    (document.querySelector('button[aria-label="All tabs"]') as HTMLElement).click();
    const visibleMs = await new Promise<number>(resolve => {
      const observer = new MutationObserver(check);
      function check() {
        const content = document.querySelector(selector);
        if (content?.textContent?.includes(text) && content.getBoundingClientRect().width > 0) {
          observer.disconnect();
          resolve(performance.now() - start);
        }
      }
      observer.observe(document, { subtree: true, childList: true, attributes: true });
      check();
    });
    const frameDelayMs = await new Promise<number>(resolve => requestAnimationFrame(() => resolve(performance.now() - start - visibleMs)));
    return { visibleMs, frameDelayMs };
  }, { selector, text });
}

for (const count of [1, 12]) {
  test(`cached filmstrip content opens instantly with ${count} tabs and a 300ms bridge`, async ({ page }) => {
    const reply = 'The cached reply is ready for the filmstrip.';
    const scenario = plainReply();
    await installMockEngine(page, { ...scenario, initial: { ...scenario.initial, entries: [{ Role: 'assistant', Text: reply }] } });
    const attempts: string[] = [];
    await page.route('**/api/engine/**', async route => {
      const path = new URL(route.request().url()).pathname;
      if (/\/sessions(?:\/[^/]+)?$/.test(path)) attempts.push(path);
      await new Promise(resolve => setTimeout(resolve, 300));
      await route.fallback();
    });
    const tabs = Array.from({ length: count }, (_, index) => ({ id: `perf-${index}`, title: `Preview ${index}`, titleSource: 'manual', kind: 'conversation', draft: '', pinned: false, sessionFile: `perf-${index}.jsonl` }));
    await page.addInitScript(state => {
      localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
      localStorage.setItem('codeaf.desktop.overview.view', 'film');
    }, { tabs, groups: [], closed: [], activeId: tabs[0].id, nextNumber: count + 1, recentIds: tabs.map(tab => tab.id) });
    await page.goto('/');
    await expect(page.locator('.conversation-view').getByText(reply, { exact: true })).toBeVisible();
    const overview = page.getByRole('dialog', { name: 'All tabs overview' });
    const roundTrips = () => attempts.length;
    const open = async () => {
      const timing = await openVisible(page, '.overview-film-item[data-cursor="true"] .overview-film-pane', reply);
      console.log(JSON.stringify(timing));
      return timing.visibleMs;
    };
    const before = roundTrips();
    const first = await open();
    await expect(overview).toBeVisible();
    const firstTrips = roundTrips() - before;
    await overview.getByRole('button', { name: 'Done', exact: true }).click();
    const reopenBefore = roundTrips();
    const reopen = await open();
    const reopenTrips = roundTrips() - reopenBefore;
    console.log(JSON.stringify({ count, firstMs: Math.round(first), reopenMs: Math.round(reopen), firstTrips, reopenTrips }));
    expect(first).toBeLessThan(100);
    expect(reopen).toBeLessThan(100);
    expect(firstTrips).toBe(0);
    if (count === 12) expect(await overview.locator('.overview-live').count()).toBeLessThan(count);
    expect(reopenTrips).toBe(0);
    expect(await overview.locator('.overview-film-pane').first().evaluate(el => ({ width: getComputedStyle(el).width, scale: getComputedStyle(el).transform }))).toEqual({ width: '1040px', scale: 'matrix(0.5, 0, 0, 0.5, 0, 0)' });
  });
}

test('cold kind previews read in parallel once and share their cached contents with the filmstrip', async ({ page }) => {
  const scenario = plainReply();
  await installMockEngine(page, { ...scenario,
    files: { 'a.ts': { mime: 'text/plain', dataBase64: Buffer.from('export const cached = true;\n').toString('base64') } },
    diffs: { 'a.ts': { hunks: [{ header: '@@ -1 +1 @@', oldStart: 1, oldLines: 1, newStart: 1, newLines: 1, lines: [{ kind: 'add', new: 1, text: 'export const cached = true;' }] }] } },
    terminals: [{ id: 'term-1', output: 'cached terminal tail\n', state: 'exited', exitCode: 0 }],
  });
  const reads: { path: string; at: number }[] = [];
  await page.route('**/api/engine/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (/\/(?:files\/text|diff|output)$/.test(path)) reads.push({ path, at: Date.now() });
    await new Promise(resolve => setTimeout(resolve, 300));
    await route.fallback();
  });
  const tabs = [
    { id: 'chat', kind: 'conversation', title: 'Chat', sessionFile: 'mock-session-1.jsonl' },
    { id: 'file', kind: 'file', title: 'a.ts', target: { sessionId: 'mock-1', path: 'a.ts' } },
    { id: 'diff', kind: 'diff', title: 'Changes', target: { sessionId: 'mock-1', path: 'a.ts' } },
    { id: 'terminal', kind: 'terminal', title: 'Terminal', target: { sessionId: 'mock-1', terminalId: 'term-1' } },
  ].map(tab => ({ ...tab, draft: '', pinned: false, titleSource: 'manual' }));
  await page.addInitScript(state => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state)), { tabs, groups: [], closed: [], activeId: 'chat', nextNumber: 5, recentIds: tabs.map(tab => tab.id) });
  await page.goto('/');
  await expect(page.locator('.conversation-view')).toContainText('Here is the short version.');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await expect(overview.locator('[data-card-id="file"] .preview-field')).toContainText('export const cached = true;');
  await expect(overview.locator('[data-card-id="diff"] .preview-field')).toContainText('+ export const cached = true;');
  await expect(overview.locator('[data-card-id="terminal"] .preview-field')).toContainText('cached terminal tail');
  expect(reads).toHaveLength(3);
  expect(Math.max(...reads.map(read => read.at)) - Math.min(...reads.map(read => read.at))).toBeLessThan(100);
  await overview.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  await expect(overview.locator('[data-card-id="file"] .preview-field')).toContainText('export const cached = true;');
  await overview.getByRole('radio', { name: 'Filmstrip' }).click();
  await page.keyboard.press('ArrowRight');
  await expect(overview.locator('[data-cursor="true"] .preview-field')).toContainText('export const cached = true;');
  await page.keyboard.press('ArrowRight');
  await expect(overview.locator('[data-cursor="true"] .preview-field')).toContainText('+ export const cached = true;');
  await page.keyboard.press('ArrowRight');
  await expect(overview.locator('[data-cursor="true"] .preview-field')).toContainText('cached terminal tail');
  expect(reads).toHaveLength(3);
});

test('an already loaded diff supplies filmstrip content without another bridge read', async ({ page }) => {
  const scenario = plainReply();
  const text = 'const alreadyLoaded = true;';
  await installMockEngine(page, { ...scenario, diffs: { 'warm.ts': { hunks: [{ header: '@@ -1 +1 @@', oldStart: 1, oldLines: 1, newStart: 1, newLines: 1, lines: [{ kind: 'add', new: 1, text }] }] } } });
  let reads = 0;
  await page.route('**/api/engine/**', async route => {
    if (new URL(route.request().url()).pathname.endsWith('/diff')) reads++;
    await new Promise(resolve => setTimeout(resolve, 300));
    await route.fallback();
  });
  await page.addInitScript(() => {
    const tab = { id: 'warm', title: 'warm.ts', kind: 'diff', draft: '', pinned: false, sessionFile: 'mock-session-1.jsonl', file: { path: 'warm.ts', view: 'changes' } };
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [tab], groups: [], closed: [], activeId: tab.id, nextNumber: 2, recentIds: [tab.id] }));
    localStorage.setItem('codeaf.desktop.overview.view', 'film');
  });
  await page.goto('/');
  await expect(page.locator('.file-live')).toContainText(text);
  const before = reads;
  const timing = await openVisible(page, '.overview-film-pane .preview-field', text);
  expect(timing.visibleMs).toBeLessThan(100);
  expect(reads).toBe(before);
});
