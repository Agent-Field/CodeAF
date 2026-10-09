import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { emitState, installNativeWebMock, nativeCalls } from './support/native-web-mock';
import { richReply } from './support/scenarios-v2';
import { message, openApp, posts, send } from './support/conversation';

// The web lane inside the real workspace: every step is a real click or key press in the real React tree. The native
// side is the typed mock of src-tauri/src/web.rs (no page is drawn, the snapshot is a fixed 1x1 PNG) and the engine is
// the mock engine, so this proves what the renderer asks, draws and refuses, never that a page rendered. Native
// isolation is proven by the Rust tests and the native smoke.
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
const activeTab = (page: Page) => page.locator('.workspace-tab[data-active="true"]');
const stored = (page: Page) => page.evaluate(() => JSON.parse(localStorage.getItem('codeaf.desktop.workspace.v1') ?? 'null'));
const modifier = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control') as 'Meta' | 'Control');

test('an address in the new-tab field opens a web tab; the page, the chat and the engine stay separate', async ({ page }) => {
  const engineCalls: string[] = [];
  await page.route('**/api/engine/**', route => { engineCalls.push(`${route.request().method()} ${new URL(route.request().url()).pathname}`); return route.abort(); });
  await installNativeWebMock(page);
  await page.goto('/');

  // 1. Type an address: the first row opens the page, the second still asks the same words.
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await field(page).fill('pkg.go.dev/encoding/json');
  const rows = await page.getByRole('option').allTextContents();
  expect(rows[0]).toMatch(/^Open pkg\.go\.dev\/encoding\/json in a web tab/);
  expect(rows[1]).toMatch(/^Ask “pkg\.go\.dev\/encoding\/json” in a new conversation/);
  await expect(page.locator('.newtab-hint')).toHaveText('↵ to open the page');
  await field(page).press('Enter');

  // 2. The tab became a web tab: monogram from the site, one native view with the typed address.
  await expect(activeTab(page)).toHaveAttribute('data-kind', 'web');
  await expect(activeTab(page).locator('.tab-monogram')).toHaveText('p');
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
  const open = (await nativeCalls(page, 'web_open'))[0].args;
  expect(open.url).toBe('https://pkg.go.dev/encoding/json');
  const pane = open.pane as string;
  await expect(page.locator('.web-address-site')).toHaveText('pkg.go.dev');

  // 3. Address, reload and back are real clicks that reach the native side as typed calls.
  await emitState(page, pane, { loading: false, title: 'json package', canBack: true });
  await expect(activeTab(page)).toContainText('json package');
  await expect(activeTab(page).locator('.tab-monogram')).toHaveText('p');
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  expect((await nativeCalls(page, 'web_history')).map(call => call.args.step)).toEqual(['reload', 'back']);
  await page.getByRole('button', { name: /^Address / }).click();
  await page.getByRole('textbox', { name: 'Address' }).fill('go.dev/doc');
  await page.getByRole('textbox', { name: 'Address' }).press('Enter');
  await expect.poll(async () => (await nativeCalls(page, 'web_navigate')).map(call => call.args)).toEqual([{ pane, url: 'https://go.dev/doc' }]);
  await expect(activeTab(page).locator('.tab-monogram')).toHaveText('g');
  expect((await stored(page)).tabs.find((tab: { id: string }) => tab.id === pane).target.url).toBe('https://go.dev/doc');

  // 4. Start a conversation with the page: an unsent draft and the page's picture, attached once.
  await page.getByRole('button', { name: 'Start a conversation with this page' }).click();
  await expect(activeTab(page)).toHaveAttribute('data-kind', 'conversation');
  await expect(message(page)).toHaveValue(/^About this page, "json package": https:\/\/go\.dev\/doc\n\n$/);
  await expect(page.getByRole('img', { name: 'page.png' })).toHaveCount(1);
  expect(engineCalls.filter(call => call.startsWith('POST'))).toEqual([]);
  await message(page).pressSequentially('what is this?');
  // The page is hidden, not closed, while the chat shows.
  await expect.poll(async () => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible).toBe(false);
  expect((await nativeCalls(page, 'web_close'))).toEqual([]);

  // 5. A new tab, then the hover card and the overview draw the web tab from its native picture without a second frame.
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.locator('.workspace-tab[data-kind="web"] .workspace-tab-select').hover();
  const card = page.getByRole('group', { name: 'Preview of json package', exact: true });
  await expect(card).toBeVisible();
  await expect(card.locator('.preview-shot-image')).toBeVisible();
  await expect(card.locator('.preview-address')).toHaveText('go.dev/doc');
  await expect.poll(async () => (await nativeCalls(page, 'web_snapshot')).length).toBeGreaterThan(0);
  await page.mouse.move(600, 600);
  await expect(card).toHaveCount(0);
  await page.getByRole('button', { name: 'All tabs' }).click();
  const webCard = page.locator('.overview-card[data-kind="web"]');
  await expect(webCard.locator('.preview-shot-image')).toBeVisible();
  await expect(webCard.locator('.overview-card-title')).toHaveCount(1);
  await expect(webCard.locator('.preview-card, .preview-caption')).toHaveCount(0);
  // The page stays hidden while the full-window overview covers it.
  await expect.poll(async () => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible).toBe(false);
  await page.keyboard.press('Escape');

  // 6. The draft survived the tab switches; closing the web tab closes only its view.
  await page.locator('.workspace-tab[data-kind="conversation"] .workspace-tab-select').last().click();
  await expect(message(page)).toHaveValue(/what is this\?About this page/);
  await page.locator('.workspace-tab[data-kind="web"]').hover();
  await page.locator('.workspace-tab[data-kind="web"]').getByRole('button', { name: /^Close / }).click();
  await expect.poll(async () => (await nativeCalls(page, 'web_close')).map(call => call.args)).toEqual([{ pane }]);
  expect((await nativeCalls(page, 'web_open')).length).toBe(1);
  await expect(message(page)).toHaveValue(/what is this\?About this page/);
  expect(engineCalls.filter(call => /turn|stop|cancel/.test(call))).toEqual([]);
});

test('a modified click on a link opens a web tab, a plain click opens the browser, and closing the tab leaves the conversation running', async ({ page }) => {
  await installNativeWebMock(page, { engine: true });
  const engine = await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  const link = page.locator('.answer-block a[href="https://pkg.go.dev/time"]');
  await expect(link).toBeVisible();

  await link.click();
  await expect.poll(async () => (await nativeCalls(page, 'open_url')).map(call => call.args)).toEqual([{ url: 'https://pkg.go.dev/time' }]);
  expect((await nativeCalls(page, 'web_open')).length).toBe(0);

  await link.click({ modifiers: [await modifier(page)] });
  await expect(activeTab(page)).toHaveAttribute('data-kind', 'web');
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual(['https://pkg.go.dev/time']);
  expect((await nativeCalls(page, 'open_url')).length).toBe(1);

  const turns = posts(engine, '/turn').length;
  await activeTab(page).hover();
  await activeTab(page).getByRole('button', { name: /^Close / }).click();
  await expect.poll(async () => (await nativeCalls(page, 'web_close')).length).toBe(1);
  await expect(page.locator('.answer-block a[href="https://pkg.go.dev/time"]')).toBeVisible();
  expect(posts(engine, '/turn').length).toBe(turns);
  expect(engine.calls.filter(call => /stop|cancel|abort/.test(call.path))).toEqual([]);
});
