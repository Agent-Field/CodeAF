import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { copiedText, deliverLinks, installDeepLinkMock, nativeCalls } from './support/deep-link-mock';

// Copy link and codeaf:// links opened from outside the app (Shell 3g "Copy link ⌘⇧C"). The tab menu's Copy link copies
// a link to the tab's DURABLE target; a link that arrives focuses the tab already showing it or opens ONE tab on it.
// The IPC mock stands in for src-tauri/src/links.rs; the mock engine stands in for the engine's history store,
// sessions and terminals. Nothing here calls a model, and every test asserts that no turn was sent.
const CHAT = '9446cc2627f3deae';
const OTHER = '1a2b3c4d5e6f7081';
const SESSION = `/mock/places/${CHAT}/transcript.jsonl`;
const OTHER_SESSION = `/mock/places/${OTHER}/transcript.jsonl`;
const at = new Date(Date.now() - 3_600_000).toISOString();
const history = { conversations: [{ id: CHAT, title: 'Fix the parser', at }, { id: OTHER, title: 'Release notes', at }] };

type SeedTab = { id: string; title: string; kind?: string; sessionFile?: string; target?: Record<string, string>; file?: { path: string; view?: string } };
async function seed(page: Page, tabs: SeedTab[], active: string) {
  const state = {
    tabs: tabs.map(t => ({ draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, ...t })),
    groups: [], closed: [], activeId: active, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id),
  };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const engineFor = (page: Page, over: Parameters<typeof installMockEngine>[1] extends infer S ? Partial<S & object> : never = {}) =>
  installMockEngine(page, { history, initial: { sessionFile: SESSION, title: 'Fix the parser', entries: [] }, ...over });
// The strip's tablist owns its tabs through aria-owns, so tabs are counted on the page (no overview is open here).
const tabCount = (page: Page) => page.getByRole('tab').count();
const tabsNamed = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const turns = (engine: MockEngine) => engine.calls.filter(call => call.path.endsWith('/turn')).length;
const newSessions = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && /\/sessions$/.test(call.path) && !call.body.sessionFile).length;
const terminalStarts = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && /\/terminals$/.test(call.path)).length;

test.describe('Copy link in the tab menu', () => {
  test('a saved conversation copies its codeaf link, says so in the shared toast, and draws the chord', async ({ page }) => {
    await installDeepLinkMock(page, { clipboard: 'record' });
    const engine = await engineFor(page);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'b');
    await page.goto('/');
    await page.getByRole('tab', { name: 'Fix the parser', exact: true }).click({ button: 'right' });
    const item = page.getByRole('menuitem', { name: /^Copy link/ });
    await expect(item).toBeVisible();
    await expect(item).toContainText(/(⌘⇧C|Ctrl Shift C)/);
    await expectAccessible(page);
    await item.click();
    await expect.poll(() => copiedText(page)).toEqual([`codeaf://chat/${CHAT}`]);
    const toast = page.locator('.toast');
    await expect(toast).toContainText('Copied the link to Fix the parser');
    expect(await toast.getByRole('button', { name: 'Undo' }).count()).toBe(0);
    expect(turns(engine)).toBe(0);
  });

  test('a conversation never sent, and Settings, have no Copy link at all', async ({ page }) => {
    await installDeepLinkMock(page, { clipboard: 'record' });
    await engineFor(page);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 's', title: 'Settings', kind: 'settings' }], 'a');
    await page.goto('/');
    for (const name of ['Intro', 'Settings']) {
      await page.getByRole('tab', { name, exact: true }).click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: /^Close tab(?!s)/ })).toBeVisible();
      await expect(page.getByRole('menuitem', { name: /^Copy link/ })).toHaveCount(0);
      await page.keyboard.press('Escape');
    }
  });

  test('a clipboard that refuses is said in a danger toast and nothing claims it was copied', async ({ page }) => {
    await installDeepLinkMock(page, { clipboard: 'refuse' });
    await engineFor(page);
    await seed(page, [{ id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'b');
    await page.goto('/');
    await page.getByRole('tab', { name: 'Fix the parser', exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Copy link/ }).click();
    const toast = page.locator('.toast');
    await expect(toast).toContainText('Could not copy the link to Fix the parser');
    await expect(toast.locator('.toast-dot')).toHaveCSS('background-color', await tokenColor(page, 'danger'));
  });

  test('the chord copies the active tab; a file tab keeps it as Copy path and its menu shows Copy link without it', async ({ page }) => {
    await installDeepLinkMock(page, { clipboard: 'record' });
    await engineFor(page);
    await seed(page, [{ id: 'b', title: 'Fix the parser', sessionFile: SESSION }, { id: 'f', title: 'main.go', kind: 'file', sessionFile: SESSION, file: { path: 'src/main.go', view: 'file' } }], 'b');
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press('ControlOrMeta+Shift+C');
    await expect.poll(() => copiedText(page)).toEqual([`codeaf://chat/${CHAT}`]);
    await page.getByRole('tab', { name: 'main.go', exact: true }).click({ button: 'right' });
    const item = page.getByRole('menuitem', { name: /^Copy link/ });
    await expect(item).not.toContainText(/(⌘⇧C|Ctrl Shift C)/);
    await item.click();
    await expect.poll(() => copiedText(page)).toEqual([`codeaf://chat/${CHAT}`, `codeaf://file/${CHAT}?path=src/main.go`]);
  });

  test('a web tab copies its own address, never a codeaf wrapper', async ({ page }) => {
    await installDeepLinkMock(page, { clipboard: 'record' });
    await engineFor(page);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'w', title: 'Docs', kind: 'web', target: { url: 'https://example.com/docs?q=1' } }], 'a');
    await page.goto('/');
    await page.getByRole('tab', { name: 'Docs', exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: /^Copy link/ }).click();
    await expect.poll(() => copiedText(page)).toEqual(['https://example.com/docs?q=1']);
  });
});

test.describe('opening a codeaf link', () => {
  test('a link that launched the app opens ONE tab on that conversation, attached, with nothing sent', async ({ page }) => {
    await installDeepLinkMock(page, { atBoot: [`codeaf://chat/${OTHER}`] });
    const engine = await engineFor(page, { initial: { sessionFile: OTHER_SESSION, title: 'Release notes', entries: [] } });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    const opened = page.getByRole('tab', { name: 'Release notes', exact: true });
    await expect(opened).toHaveAttribute('aria-selected', 'true');
    expect(await tabCount(page)).toBe(2);
    await expect.poll(() => engine.calls.some(call => call.method === 'POST' && call.body.sessionFile === OTHER_SESSION)).toBe(true);
    expect(engine.calls.some(call => call.path.endsWith(`/history/${OTHER}`))).toBe(true);
    expect(turns(engine)).toBe(0);
    expect(newSessions(engine)).toBe(0);
    expect(await nativeCalls(page)).toContain('link_claim');
  });

  test('a link to a conversation that already has a tab focuses it, asks the engine nothing and opens nothing', async ({ page }) => {
    await installDeepLinkMock(page);
    const engine = await engineFor(page);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'a');
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Intro', exact: true })).toHaveAttribute('aria-selected', 'true');
    const before = await tabCount(page);
    await deliverLinks(page, [`codeaf://chat/${CHAT}`, `codeaf://chat/${CHAT}`]);
    await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
    expect(await tabCount(page)).toBe(before);
    expect(engine.calls.some(call => call.path.includes('/history/'))).toBe(false);
    expect(turns(engine)).toBe(0);
  });

  test('the same link twice while the app runs opens one tab, not two', async ({ page }) => {
    await installDeepLinkMock(page);
    const engine = await engineFor(page, { initial: { sessionFile: OTHER_SESSION, title: 'Release notes', entries: [] } });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect.poll(() => nativeCalls(page)).toContain('link_claim');
    await deliverLinks(page, [`codeaf://chat/${OTHER}`]);
    await deliverLinks(page, [`codeaf://chat/${OTHER}`]);
    await expect(page.getByRole('tab', { name: 'Release notes', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.waitForTimeout(300);
    expect(await tabsNamed(page, 'Release notes').count()).toBe(1);
    expect(turns(engine)).toBe(0);
  });

  test('a damaged, unknown or climbing link opens nothing, asks the engine nothing and says why', async ({ page }) => {
    await installDeepLinkMock(page);
    const engine = await engineFor(page);
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect.poll(() => nativeCalls(page)).toContain('link_claim');
    const toast = page.locator('.toast');
    const cases: [string, string][] = [
      ['codeaf://chat/%E0%A4%A', 'That codeaf link is damaged, so nothing was opened.'],
      ['codeaf://settings', 'codeaf does not know what that link points to, so nothing was opened.'],
      [`codeaf://file/${CHAT}?path=../../etc/passwd`, 'That link points outside what codeaf can open, so nothing was opened.'],
      [`codeaf://file/${CHAT}?path=%2Fetc%2Fpasswd`, 'That link points outside what codeaf can open, so nothing was opened.'],
    ];
    for (const [link, sentence] of cases) {
      await deliverLinks(page, [link]);
      await expect(toast.filter({ hasText: sentence }).last()).toBeVisible();
      await expect(toast.filter({ hasText: sentence }).last().locator('.toast-dot')).toHaveCSS('background-color', await tokenColor(page, 'danger'));
    }
    expect(await tabCount(page)).toBe(1);
    expect(engine.calls.filter(call => !call.path.includes('/world') && !call.path.includes('/events') && !call.path.includes('/workspaces/'))).toEqual([]);
  });

  test('an unknown conversation and an expired terminal are said, and no shell is started', async ({ page }) => {
    await installDeepLinkMock(page);
    const engine = await engineFor(page, { terminals: [{ id: 'live01', title: 'zsh', state: 'running', output: '$ ' }] });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect.poll(() => nativeCalls(page)).toContain('link_claim');
    const toast = page.locator('.toast');
    await deliverLinks(page, ['codeaf://chat/0000000000000000']);
    await expect(toast.filter({ hasText: 'That conversation is no longer in codeaf, so the link did not open.' })).toBeVisible();
    await deliverLinks(page, [`codeaf://terminal/${CHAT}/gone01`]);
    await expect(toast.filter({ hasText: 'That terminal has ended and is no longer kept, so the link did not open.' })).toBeVisible();
    expect(await tabCount(page)).toBe(1);
    await deliverLinks(page, [`codeaf://terminal/${CHAT}/live01`]);
    await expect(page.getByRole('tab', { name: 'zsh', exact: true })).toHaveAttribute('aria-selected', 'true');
    expect(terminalStarts(engine)).toBe(0);
    expect(newSessions(engine)).toBe(0);
    expect(turns(engine)).toBe(0);
  });

  test('a file link opens a file tab read through its conversation, and a second one focuses it', async ({ page }) => {
    await installDeepLinkMock(page);
    await engineFor(page, { files: { 'src/main.go': { mime: 'text/x-go', dataBase64: Buffer.from('package main\n').toString('base64') } } });
    await seed(page, [{ id: 'a', title: 'Intro' }], 'a');
    await page.goto('/');
    await expect.poll(() => nativeCalls(page)).toContain('link_claim');
    await deliverLinks(page, [`codeaf://file/${CHAT}?path=src/main.go`]);
    await expect(page.getByRole('tab', { name: 'main.go', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('tab', { name: 'Intro', exact: true }).click();
    await deliverLinks(page, [`codeaf://file/${CHAT}?path=src/main.go`]);
    await expect(page.getByRole('tab', { name: 'main.go', exact: true })).toHaveAttribute('aria-selected', 'true');
    expect(await tabsNamed(page, 'main.go').count()).toBe(1);
  });
});

test.describe('appearance', () => {
  for (const theme of ['light', 'dark'] as const) {
    test(`the Copy link row and its toast in ${theme}`, async ({ page }, info) => {
      await page.emulateMedia({ colorScheme: theme });
      await installDeepLinkMock(page, { clipboard: 'record' });
      await engineFor(page);
      await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'b');
      await page.goto('/');
      await page.getByRole('tab', { name: 'Fix the parser', exact: true }).click({ button: 'right' });
      const item = page.getByRole('menuitem', { name: /^Copy link/ });
      await item.hover();
      await expectAccessible(page);
      const shots = process.env.CODEAF_SHOTS;
      if (shots) await page.screenshot({ path: `${shots}/copy-link-menu-${theme}-${info.project.name}.png` });
      await item.click();
      await expect(page.locator('.toast')).toContainText('Copied the link to Fix the parser');
      await expectAccessible(page);
      if (shots) await page.screenshot({ path: `${shots}/copy-link-toast-${theme}-${info.project.name}.png` });
    });
  }
});
