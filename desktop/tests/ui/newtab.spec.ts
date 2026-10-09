import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { message, posts, send } from './support/conversation';
import { newConversation } from './support/new-tab';

// Design 3f / 4c / 2h and Components "Command field": a new tab is one field, not a page.
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
const rowNames = (page: Page) => page.getByRole('option').allTextContents();
const openField = (page: Page) => page.getByRole('button', { name: 'New tab', exact: true }).click();
const mod = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? '⌘' : 'Ctrl '));
const saved = (page: Page) => page.evaluate(() => JSON.parse(localStorage.getItem('codeaf.desktop.workspace.v1') ?? 'null'));

test.describe('with the engine away', () => {
 test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

 test('the New tab button opens an empty card with one centred field and no engine call', async ({ page }) => {
  const calls: string[] = [];
  page.on('request', request => { if (request.url().includes('/api/engine/')) calls.push(request.url()); });
  await page.goto('/');
  calls.length = 0;
  await openField(page);
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(field(page)).toBeFocused();
  await expect(page.getByText('↵ to start a conversation')).toBeVisible();
  await expect(page.getByText('Type a question, a file, a URL, or a command.')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(0);
  expect(await rowNames(page)).toEqual([`Open file…${await mod(page)}O`]);
  expect(calls).toEqual([]);
 });

 test('the field, its rows and the caption have the design geometry', async ({ page }) => {
  await page.goto('/'); await openField(page);
  await field(page).fill('fix');
  const card = await page.locator('.workspace-pane').boundingBox();
  const box = await page.locator('.newtab-field').boundingBox();
  expect(Math.round(box!.width)).toBe(612);
  expect(Math.round(box!.y - card!.y)).toBe(120);
  expect(Math.abs((box!.x + box!.width / 2) - (card!.x + card!.width / 2))).toBeLessThanOrEqual(1);
  const shell = page.locator('.newtab-field');
  await expect(shell).toHaveCSS('border-top-left-radius', '14px');
  await expect(shell).toHaveCSS('padding-top', '6px');
  await expect(shell).toHaveCSS('background-color', await tokenColor(page, 'surface'));
  await expect(shell).toHaveCSS('box-shadow', await page.evaluate(() => { const p = document.createElement('div'); p.style.boxShadow = 'var(--sh-2)'; document.body.append(p); const v = getComputedStyle(p).boxShadow; p.remove(); return v; }));
  expect((await page.locator('.newtab-input-row').boundingBox())!.height).toBe(48);
  await expect(page.locator('.newtab-input-row')).toHaveCSS('font-size', '15px');
  await expect(page.locator('.newtab-hint')).toHaveCSS('font-size', '11px');
  const rule = await page.locator('.newtab-rule').boundingBox();
  expect(rule!.height).toBeCloseTo(0.5, 1);
  const rows = page.locator('.newtab-row');
  for (const row of await rows.all()) expect((await row.boundingBox())!.height).toBe(36);
  await expect(rows.first()).toHaveCSS('border-top-left-radius', '8px');
  await expect(rows.first()).toHaveCSS('font-size', '13px');
  await expect(rows.first()).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await expect(rows.nth(1)).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(page.locator('.newtab-section').first()).toHaveCSS('font-size', '11px');
  await expect(page.locator('.newtab-section').first()).toHaveCSS('font-weight', '500');
  const caption = page.locator('.newtab-caption');
  await expect(caption).toHaveCSS('font-size', '12px');
  expect(Math.round((await caption.boundingBox())!.y - (box!.y + box!.height))).toBe(16);
  await expect(field(page)).toHaveCSS('caret-color', await tokenColor(page, 'accent'));
 });

 test('typing puts the conversation row first, then Start, then matching open and closed tabs', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('tab').first().click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename tab', exact: true }).click();
  await page.getByRole('dialog', { name: 'Rename tab' }).getByRole('textbox', { name: 'Name' }).fill('Port fix to v1 branch');
  await page.getByRole('dialog', { name: 'Rename tab' }).getByRole('button', { name: 'Save' }).click();
  await newConversation(page, 'Fix it in the lexer');
  await page.getByRole('tab', { name: 'Fix it in the lexer', exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Close tab/ }).click();
  await openField(page);
  await field(page).fill('fix');
  const key = await mod(page);
  expect(await rowNames(page)).toEqual(['Ask “fix” in a new conversation↵', `Open file…${key}O`, `Port fix to v1 branch open tab${key}1`, 'Fix it in the lexer closed']);
  await expect(page.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.newtab-row b').first()).toHaveText('fix');
  expect(await page.locator('.newtab-section').allTextContents()).toEqual(['Start', 'Matching']);
 });

 test('arrows move, Enter on an open tab row jumps to it and the empty field goes away', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('tab').first().click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename tab', exact: true }).click();
  await page.getByRole('dialog', { name: 'Rename tab' }).getByRole('textbox', { name: 'Name' }).fill('Port fix');
  await page.getByRole('dialog', { name: 'Rename tab' }).getByRole('button', { name: 'Save' }).click();
  await openField(page);
  await field(page).fill('fix');
  await field(page).press('ArrowDown'); await field(page).press('ArrowDown');
  await expect(page.getByRole('option', { name: /Port fix/ })).toHaveAttribute('aria-selected', 'true');
  await field(page).press('ArrowDown');
  await expect(page.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
  await field(page).press('ArrowUp');
  await expect(page.getByRole('option', { name: /Port fix/ })).toHaveAttribute('aria-selected', 'true');
  await field(page).press('Enter');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toHaveAttribute('aria-selected', 'true');
 });

 test('Enter on the first row turns the tab into a conversation and keeps the words when the engine is away', async ({ page }) => {
  await page.goto('/'); await openField(page);
  await field(page).fill('Why is the lexer slow?');
  await field(page).press('Enter');
  await expect(message(page)).toHaveValue('Why is the lexer slow?');
  await expect(page.getByRole('tab', { name: 'Why is the lexer slow?', exact: true })).toHaveAttribute('aria-selected', 'true');
  expect((await saved(page)).tabs.at(-1).kind).toBe('conversation');
 });

 test('Escape clears the text, then closes the empty field; a URL is a question for now', async ({ page }) => {
  await page.goto('/'); await openField(page);
  await field(page).fill('https://pkg.go.dev/encoding/json');
  expect((await rowNames(page))[0]).toMatch(/^Ask “https:\/\/pkg\.go\.dev\/encoding\/json” in a new conversation/);
  await field(page).press('Escape');
  await expect(field(page)).toHaveValue('');
  await expect(page.getByRole('tab')).toHaveCount(2);
  await field(page).press('Escape');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(message(page)).toBeVisible();
 });

 test('a closed tab row brings the tab back into the field and leaves the closed list', async ({ page }) => {
  await page.goto('/');
  await newConversation(page, 'Fix it in the lexer');
  await page.getByRole('tab', { name: 'Fix it in the lexer', exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Close tab/ }).click();
  await openField(page);
  await field(page).fill('lexer');
  await page.getByRole('option', { name: /Fix it in the lexer/ }).click();
  await expect(page.getByRole('tab', { name: 'Fix it in the lexer', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(message(page)).toHaveValue('Fix it in the lexer');
  expect((await saved(page)).closed.filter((tab: { title: string }) => tab.title === 'Fix it in the lexer')).toHaveLength(0);
 });

 test('Open file… keeps the field and asks for part of a name', async ({ page }) => {
  await page.goto('/'); await openField(page);
  await page.getByRole('option', { name: /Open file/ }).click();
  await expect(page.getByText('Type part of a file name.')).toBeVisible();
  await expect(field(page)).toBeFocused();
 });

 test('is accessible and uses shared controls, light and dark', async ({ page }) => {
  await page.goto('/'); await openField(page); await field(page).fill('fix');
  for (const theme of ['Light', 'Dark']) {
   await page.getByRole('combobox', { name: 'Theme' }).click().catch(() => undefined);
   const option = page.getByRole('option', { name: `${theme} appearance`, exact: true });
   if (await option.count()) await option.click();
   await expectAccessible(page); await expectNoUnstyledControls(page);
  }
 });

 test('stays inside the card at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 560 });
  await page.goto('/'); await openField(page); await field(page).fill('fix');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const box = await page.locator('.newtab-field').boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(320);
 });
});

test.describe('with a conversation engine', () => {
 test('Enter on the first row creates a session, sends the words and opens the conversation', async ({ page }) => {
  const engine = await installMockEngine(page, plainReply());
  await page.goto('/');
  await expect(message(page)).toBeVisible();
  const before = posts(engine, '').filter(call => call.path.endsWith('/sessions')).length;
  await openField(page);
  await field(page).fill('Explain the lexer');
  await field(page).press('Enter');
  await expect(page.getByRole('tab', { name: 'Explain the lexer', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.user-message-text').filter({ hasText: 'Explain the lexer' })).toBeVisible();
  expect(posts(engine, '/turn').length).toBeGreaterThanOrEqual(1);
  expect(posts(engine, '').filter(call => call.path.endsWith('/sessions')).length).toBeGreaterThan(before);
  await expect(message(page)).toHaveValue('');
 });

 test('matching files come from the engine and open a file tab', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), files: { 'internal/parse/lexer.go': { mime: 'text/x-go', dataBase64: Buffer.from('package parse\n// lexer content').toString('base64') } } });
  await page.route('**/api/engine/sessions/*/files/find*', route => route.fulfill({ json: { files: [{ path: 'internal/parse/lexer.go', name: 'lexer.go', dir: 'internal/parse' }] } }));
  await page.goto('/');
  await send(page, 'Where is the lexer?');
  await expect.poll(async () => (await saved(page)).tabs[0].sessionFile).toBeTruthy();
  await openField(page);
  await field(page).fill('lex');
  const file = page.getByRole('option', { name: /lexer\.go/ });
  await expect(file).toContainText('internal/parse');
  await file.click();
  await expect(page.getByRole('tab', { name: 'lexer.go', exact: true })).toHaveAttribute('aria-selected', 'true');
  const tab = (await saved(page)).tabs.at(-1);
  expect([tab.kind, tab.file?.path, typeof tab.sessionFile]).toEqual(['file', 'internal/parse/lexer.go', 'string']);
  await expect(page.locator('.file-text').getByText('// lexer content', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.locator('.file-text').getByText('// lexer content', { exact: true })).toBeVisible();
 });
});

test('the Design system page shows the field typed and empty', async ({ page }) => {
 await page.route('**/api/engine/**', route => route.abort());
 await page.goto('/');
 await page.getByRole('button', { name: 'Design system', exact: true }).click();
 const specimen = page.locator('[data-newtab-specimen]');
 await specimen.scrollIntoViewIfNeeded();
 await expect(specimen.getByRole('option', { name: /Ask “fix” in a new conversation/ })).toHaveAttribute('aria-selected', 'true');
 await expect(specimen.getByText('Matching')).toBeVisible();
});
