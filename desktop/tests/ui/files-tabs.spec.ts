import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine, type Scenario } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { openApp, send } from './support/conversation';

// File and diff tabs (Shell 3e): opened from file chips, changes first, a toggle to the whole file, a handoff to the editor.
const FILE = 'internal/auth/auth_test.go';
const b64 = (body: string) => Buffer.from(body).toString('base64');
const body = Array.from({ length: 60 }, (_, i) => (i === 29 ? '\tnow := fixedClock()' : `// line ${i + 1}`)).join('\n');

const hunk = {
  header: '@@ -29,3 +29,3 @@ func TestLogin(t *testing.T)', oldStart: 29, oldLines: 3, newStart: 29, newLines: 3, section: 'func TestLogin(t *testing.T)',
  lines: [
    { kind: 'context' as const, old: 29, new: 29, text: '// line 29' },
    { kind: 'del' as const, old: 30, text: '\tnow := time.Now()' },
    { kind: 'add' as const, new: 30, text: '\tnow := fixedClock()' },
    { kind: 'context' as const, old: 31, new: 31, text: '// line 31' },
  ],
};

async function openWithDiff(page: Page, extra: Partial<Scenario> = {}): Promise<MockEngine> {
  const base = richReply();
  const engine = await installMockEngine(page, {
    ...base, ...extra,
    files: { ...base.files, [FILE]: { mime: 'text/x-go', dataBase64: b64(body) }, 'bin/tool.bin': { mime: 'application/octet-stream', dataBase64: b64('ab\0cd') }, ...extra.files },
    diffs: { [FILE]: { lines: 60, hunks: [hunk] }, ...extra.diffs },
  });
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  return engine;
}

const strip = (page: Page) => page;
const chip = (page: Page) => page.locator('.changes').getByRole('button', { name: /^auth_test\.go/ });

async function openDiffTab(page: Page) {
  await page.getByRole('button', { name: /^Changed 1 file/ }).click();
  await chip(page).click({ modifiers: ['ControlOrMeta'] });
  await expect(strip(page).getByRole('tab', { name: /auth_test\.go/ })).toHaveAttribute('aria-selected', 'true');
}

test('a changed-file chip opens a diff tab: header, two line-number columns, red and green fills, folds that open', async ({ page }) => {
  await openWithDiff(page);
  await openDiffTab(page);
  const head = page.locator('.file-head');
  await expect(head.locator('.file-name')).toHaveText('auth_test.go');
  await expect(head.locator('.file-dir')).toHaveText('internal/auth');
  await expect(head.locator('.file-count[data-sign="add"]')).toHaveText('+1');
  await expect(head.locator('.file-count[data-sign="del"]')).toHaveText('−1');
  await expect(head.getByRole('radio', { name: 'Changes' })).toBeChecked();
  await expect(page.locator('.file-hunk')).toHaveText('@@ -29,3 +29,3 @@ func TestLogin(t *testing.T)');
  const del = page.locator('.file-diff-row[data-kind="del"]');
  await expect(del.locator('.file-num').nth(0)).toHaveText('30');
  await expect(del.locator('.file-num').nth(1)).toHaveText('');
  await expect(del.locator('.file-sign')).toHaveText('-');
  const add = page.locator('.file-diff-row[data-kind="add"]');
  await expect(add.locator('.file-num').nth(1)).toHaveText('30');
  const fill = (locator: ReturnType<Page['locator']>) => locator.evaluate(el => getComputedStyle(el).backgroundColor);
  expect(await fill(del)).not.toBe('rgba(0, 0, 0, 0)');
  expect(await fill(add)).not.toBe('rgba(0, 0, 0, 0)');
  const fold = page.getByRole('button', { name: 'Show 28 unchanged lines' });
  await expect(fold).toHaveText('⋯ 28 unchanged lines');
  await fold.click();
  await expect(page.locator('.file-diff-row[data-kind="context"] .file-text').getByText('// line 1', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Show 28 unchanged lines' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Show 29 unchanged lines' })).toBeVisible();
});

test('a plain click on a file chip opens the preview sheet; Command-click or a middle click opens the tab (Interactions)', async ({ page }) => {
  await openWithDiff(page);
  await page.getByRole('button', { name: /^Changed 1 file/ }).click();
  const tabs = await strip(page).getByRole('tab').count();
  await chip(page).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  expect(await strip(page).getByRole('tab').count()).toBe(tabs);
  await chip(page).click({ button: 'middle' });
  await expect(strip(page).getByRole('tab', { name: /auth_test\.go/ })).toHaveAttribute('aria-selected', 'true');
  expect(await strip(page).getByRole('tab').count()).toBe(tabs + 1);
});

test('the Open in menu shows the copy-path key and the key copies the path of the focused file', async ({ page, context, browserName }) => {
  if (browserName === 'chromium') await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await openWithDiff(page);
  await openDiffTab(page);
  await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
  await expect(page.getByRole('menuitem', { name: /^Copy path/ })).toContainText(/(⌘ ?⇧ ?C|Ctrl ?Shift ?C)/);
  await page.keyboard.press('Escape');
  if (browserName === 'chromium') {
    await page.keyboard.press('Control+Shift+C');
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('/mock-workspace/internal/auth/auth_test.go');
  }
});

test('the toggle shows the whole file, the choice survives a reload and a second click selects the same tab', async ({ page }) => {
  await openWithDiff(page);
  await openDiffTab(page);
  await page.locator('.file-head').getByRole('radio', { name: 'File' }).click();
  await expect(page.locator('.file-plain-row').nth(29)).toContainText('now := fixedClock()');
  await expect(page.locator('.file-plain-row').nth(29).locator('.file-num')).toHaveText('30');
  await expect(page.locator('.file-body')).toHaveAttribute('aria-label', 'Contents of auth_test.go');
  const tabs = await strip(page).getByRole('tab').count();
  await strip(page).getByRole('tab', { name: 'Login test' }).click();
  await chip(page).click({ modifiers: ['ControlOrMeta'] });
  await expect(strip(page).getByRole('tab', { name: 'auth_test.go' })).toHaveAttribute('aria-selected', 'true');
  expect(await strip(page).getByRole('tab').count()).toBe(tabs);
  await page.reload();
  await expect(page.locator('.file-head').getByRole('radio', { name: 'File' })).toBeChecked();
  await expect(page.locator('.file-plain-row').nth(29)).toContainText('now := fixedClock()');
  expect(await strip(page).getByRole('tab').count()).toBe(tabs);
});

test('a file the engine cannot show is one muted line and an Open in menu with Copy path', async ({ page }) => {
  await openWithDiff(page);
  await openDiffTab(page);
  const current = await page.evaluate(() => JSON.parse(localStorage.getItem('codeaf.desktop.workspace.v1')!));
  const source = current.tabs.find((tab: { kind: string }) => tab.kind === 'diff');
  current.tabs.push({ ...source, id: 'binary-tab', title: 'tool.bin', kind: 'file', file: { path: 'bin/tool.bin', view: 'file' } });
  current.activeId = 'binary-tab';
  await page.evaluate(state => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state)), current);
  await page.reload();
  await expect(page.locator('.file-body')).toHaveText('Binary file');
  await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
  await expect(page.getByRole('menuitem', { name: 'Copy path' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Open in editor' })).toHaveCount(0);
});

test('Open in editor appears only when the engine is on this machine; otherwise the Open in menu', async ({ page }) => {
  await page.addInitScript(() => {
    const calls: unknown[][] = [];
    Object.assign(window, { isTauri: true, __calls: calls, __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener: () => undefined }, __TAURI_INTERNALS__: {
      metadata: { currentWindow: { label: 'main' }, currentWebview: { label: 'main', windowLabel: 'main' } },
      transformCallback: () => 0,
      invoke: async (command: string, args: unknown) => { calls.push([command, args]); if (command === 'host_name') return 'mock-host'; if (command === 'engine_connection') return { url: location.origin, token: 't', model: 'deepseek/deepseek-v4.1-flash' }; return null; },
    } });
  });
  await openWithDiff(page);
  await openDiffTab(page);
  const button = page.locator('.file-head').getByRole('button', { name: /^Open in editor/ });
  await expect(button).toBeVisible();
  await button.click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { __calls: unknown[][] }).__calls.find(call => call[0] === 'open_path'))).toEqual(['open_path', { path: '/mock-workspace/internal/auth/auth_test.go', workspace: '/mock-workspace' }]);
});

test('without the desktop shell the handoff is the Open in menu', async ({ page }) => {
  await openWithDiff(page);
  await openDiffTab(page);
  await expect(page.locator('.file-head').getByRole('button', { name: /^Open in editor/ })).toHaveCount(0);
  await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
  await expect(page.getByRole('menuitem', { name: 'Copy path' })).toBeVisible();
});

test('a file outside git has the file view only: no toggle, no counts', async ({ page }) => {
  await openWithDiff(page);
  await page.route('**/api/engine/sessions/*/diff?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ path: FILE, name: 'auth_test.go', dir: 'internal/auth', abs: `/mock-workspace/${FILE}`, git: false, status: 'clean', added: 0, deleted: 0, lines: 60, hunks: [] }) }));
  await openDiffTab(page);
  await expect(page.locator('.file-plain-row').nth(29)).toContainText('now := fixedClock()');
  await expect(page.locator('.file-head').getByRole('radio')).toHaveCount(0);
  await expect(page.locator('.file-count')).toHaveCount(0);
});

test('when the start commit is gone the changes view says so', async ({ page }) => {
  await openWithDiff(page);
  await page.route('**/api/engine/sessions/*/diff?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ path: FILE, name: 'auth_test.go', dir: 'internal/auth', abs: `/mock-workspace/${FILE}`, git: true, base: { kind: 'head' }, status: 'modified', added: 1, deleted: 1, lines: 60, hunks: [hunk] }) }));
  await openDiffTab(page);
  await expect(page.locator('.file-base')).toHaveText('Compared with the latest commit. The commit this conversation started on is no longer in history.');
});

test('the Design system page draws every file tab state', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system' }).first().click();
  const cases = page.locator('[data-files-specimen]');
  await expect(cases).toHaveCount(6);
  await expect(page.locator('[data-files-specimen="Changes"] .file-hunk').first()).toHaveText('@@ 84,10 +84,16 @@ func (l *Lexer) next() Token');
  await expect(page.locator('[data-files-specimen="Too large"]')).toContainText('Too large to show, 3.4 MB');
  await expect(page.locator('[data-files-specimen="Outside git"] .file-head').getByRole('radio')).toHaveCount(0);
});
