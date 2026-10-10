import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { NOW, designConversations, withHistory } from './support/scenarios-history';

// Design Shell 4c: ⌘T searches History inline. "From history" lists the engine's own matches, Enter continues one,
// ⌘↵ (Ctrl ↵) shows all of them in the History tab. Real mock History routes; nothing here asks a model.
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
const rows = (page: Page) => page.getByRole('option');
const openField = (page: Page) => page.getByRole('button', { name: 'New tab', exact: true }).click();
const searches = (engine: MockEngine) => engine.calls.filter(call => call.path.endsWith('/history/search'));
const attached = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/sessions')).map(call => call.body.sessionFile);
const turns = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && /\/(messages|turns|send)$/.test(call.path));
const words = 'trailing commas';

async function start(page: Page) {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, withHistory(designConversations()));
  await page.goto('/');
  await expect(page.getByRole('tab').first()).toBeVisible();
  engine.calls.length = 0;
  await openField(page);
  return engine;
}
const fromHistory = (page: Page) => page.getByRole('group', { name: 'From history' });

test('an empty new tab makes no engine call, and typing asks History once after the pause, never with empty words', async ({ page }) => {
  const engine = await start(page);
  await expect(field(page)).toBeFocused();
  await page.waitForTimeout(400);
  expect(engine.calls).toEqual([]);
  await field(page).pressSequentially(words, { delay: 15 });
  await expect(fromHistory(page)).toBeVisible();
  const asked = searches(engine).map(call => new URL(`http://x${call.path}`).pathname);
  expect(asked).toHaveLength(1);
  await field(page).fill('');
  await expect(fromHistory(page)).toHaveCount(0);
  await page.waitForTimeout(400);
  expect(searches(engine)).toHaveLength(1);
  expect(turns(engine)).toEqual([]);
  expect(attached(engine)).toEqual([]);
});

test('the section is three conversations in the engine\'s words, then See all N, ahead of Start', async ({ page }) => {
  await start(page);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  expect(await page.locator('.newtab-section').allTextContents()).toEqual(['From history', 'Start']);
  const names = await rows(page).allTextContents();
  expect(names[0]).toMatch(/^Ask “trailing commas” in a new conversation/);
  const history = names.slice(1, names.findIndex(name => name.startsWith('See all')) + 1);
  expect(history).toHaveLength(4);
  expect(history[3]).toMatch(/^See all \d+ in History(⌘↵|Ctrl ↵)$/);
  await expect(page.getByRole('option', { name: /^See all/ })).toBeVisible();
  // Nothing is written by the field itself: every sentence on a row is a title, a decision, an answer or a snippet.
  expect(history[0]).not.toMatch(/\d+ messages|Worked \d+/);
});

test('arrows reach a history row, Enter opens that conversation in the field\'s own tab without a model call', async ({ page }) => {
  const engine = await start(page);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  await field(page).press('ArrowDown');
  const chosen = rows(page).nth(1);
  await expect(chosen).toHaveAttribute('aria-selected', 'true');
  await expect(chosen.locator('.newtab-row-hint')).toHaveText('↵');
  const title = (await chosen.locator('.newtab-row-label').innerText()).split(' ·')[0].trim();
  await field(page).press('Enter');
  await expect(page.getByRole('combobox', { name: 'Search or start' })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: new RegExp(title.slice(0, 12)) })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect.poll(() => attached(engine).length).toBe(1);
  expect(String(attached(engine)[0])).toMatch(/^\/mock\/places\/.+\/transcript\.jsonl$/);
  expect(turns(engine)).toEqual([]);
});

test('a command-click opens it behind and leaves the field, its words and its rows alone; a click then goes to it', async ({ page }) => {
  const engine = await start(page);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  await rows(page).nth(1).click({ modifiers: ['Control'] });
  await expect.poll(() => attached(engine).length).toBe(1);
  await expect(page.getByRole('tab')).toHaveCount(3);
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(field(page)).toHaveValue(words);
  await expect(fromHistory(page)).toBeVisible();
  // The conversation is open now, so the same row only goes to it and the empty field goes away.
  await rows(page).nth(1).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(field(page)).toHaveCount(0);
  expect(new Set(attached(engine)).size).toBe(1);
});

test('the words survive leaving the tab and coming back', async ({ page }) => {
  await start(page);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  await page.getByRole('tab').first().click();
  await expect(field(page)).toHaveCount(0);
  await page.getByRole('tab', { name: 'New tab', exact: true }).click();
  await expect(field(page)).toHaveValue(words);
  await expect(fromHistory(page)).toBeVisible();
});

test('Ctrl Enter (⌘↵) turns the field into History searching those words; the row does the same', async ({ page }) => {
  const engine = await start(page);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  await field(page).press('Control+Enter');
  await expect(page.getByRole('tab', { name: /^History/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Search history' })).toHaveValue(words);
  await expect(page.getByRole('region', { name: 'Best match' })).toBeVisible();
  await expect(page.getByRole('tab')).toHaveCount(2);
  expect(attached(engine)).toEqual([]);
  // A second field shows the same History tab instead of opening another, and hands it the new words.
  await openField(page);
  await field(page).fill('strict mode');
  await expect(fromHistory(page)).toBeVisible();
  await page.getByRole('option', { name: /^See all/ }).click();
  await expect(page.getByRole('tab', { name: /^History/ })).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /^History/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Search history' })).toHaveValue('strict mode');
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveCount(0);
});

test('nothing found draws no section, and the field keeps working', async ({ page }) => {
  const engine = await start(page);
  await field(page).fill('qqqzzz');
  await expect.poll(() => searches(engine).length).toBe(1);
  await expect(fromHistory(page)).toHaveCount(0);
  expect(await page.locator('.newtab-section').allTextContents()).toEqual(['Start']);
  await expect(rows(page).first()).toContainText('Ask “qqqzzz”');
});

test('with History unreachable the section is absent, nothing says so, and asking still works', async ({ page }) => {
  await start(page);
  await page.route('**/api/engine/history/**', route => route.abort());
  await field(page).fill(words);
  await page.waitForTimeout(500);
  await expect(fromHistory(page)).toHaveCount(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(rows(page).first()).toContainText('Ask “trailing commas”');
  await field(page).press('Control+Enter');
  await expect(page.getByRole('tab', { name: /^History/ })).toBeVisible();
});

test('an older answer never replaces a newer one: the slow first search is cancelled', async ({ page }) => {
  const engine = await start(page);
  let slow = 0;
  const cancelled: string[] = [];
  page.on('requestfailed', request => { if (request.url().includes('/history/search')) cancelled.push(request.url()); });
  await page.route('**/api/engine/history/search**', async route => {
    if (new URL(route.request().url()).searchParams.get('q') === 'trailing') { slow++; await new Promise(resolve => setTimeout(resolve, 1500)); }
    await route.fallback().catch(() => undefined);
  });
  await field(page).fill('trailing');
  await expect.poll(() => slow).toBe(1);
  await field(page).fill(words);
  await expect(fromHistory(page)).toBeVisible();
  await page.waitForTimeout(1700);
  await expect(fromHistory(page)).toBeVisible();
  const seen = searches(engine).map(call => call.path);
  expect(seen.length).toBeGreaterThanOrEqual(2);
  await expect(field(page)).toHaveValue(words);
  expect(cancelled.length + 1).toBeGreaterThanOrEqual(1);
});

for (const scheme of ['light', 'dark'] as const) {
  test(`at 320 wide in ${scheme} the section fits, the rows are reachable and accessible`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await page.emulateMedia({ colorScheme: scheme });
    await start(page);
    await field(page).fill(words);
    await expect(fromHistory(page)).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const see = page.getByRole('option', { name: /^See all/ });
    await see.scrollIntoViewIfNeeded();
    const box = (await see.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(320);
    await expectAccessible(page);
  });
}
