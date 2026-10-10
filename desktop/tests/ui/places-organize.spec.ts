import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';
import { NOW as HISTORY_NOW, designConversations, withHistory } from './support/scenarios-history';

// PL-142 PL-220 PL-223 PL-227 PL-229 PL-231 PL-232: organizing places end to end. The rail row menu, tint swatches, rename, dragging a chat
// onto a rail row, a non-chat drop being refused, delete with Undo, ⌘Z over structural place actions, and History's Add to place.
// Every path here is reachable from the keyboard; the pointer variants are in d5-pl-test-org.spec.ts (tiles) and are not repeated.

const WIDTHS = [320, 600, 850, 1200] as const;
const THEMES = ['light', 'dark'] as const;
const NOW = new Date('2026-10-10T12:00:00Z');
const fresh = (): Scenario => ({ initial: { entries: [], title: '', sessionFile: sessionFileFor('mock-1') }, turns: [] });

/** Marketing is pinned, Software and Garden are open, and Launch copy is filed in Marketing. */
const seed = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true, lastOpenedAt: 'now' },
    { name: 'Software', tint: 'iris', lastOpenedAt: 'now' },
    { name: 'Garden', tint: 'sage', lastOpenedAt: 'now' },
  ],
  chats: [
    { id: 'c-launch', title: 'Launch copy', places: ['Marketing'] },
    { id: 'c-loose', title: 'Pricing for teams' },
  ],
  live: ['mock-1'],
});

async function boot(page: Page, width: number, theme: (typeof THEMES)[number]) {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.setViewportSize({ width, height: 800 });
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, seed());
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  return { engine, places, primary: mac ? 'Meta' : 'Control' };
}

const rail = (page: Page) => page.locator('.app-shell .place-rail').first();
const railRow = (page: Page, name: string) => rail(page).locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }) });
const railPlace = (page: Page, name: string) => railRow(page, name).locator('.rail-place');
const toast = (page: Page) => page.locator('.toast-region .toast');
const tile = (page: Page, name: string) => page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: new RegExp(`^${name}$`) }) });

/** Below the wide layout the rail is put away: Show sidebar brings it back (a drawer at the smallest breakpoint). */
async function showRail(page: Page) {
  if (await railPlace(page, 'Software').isVisible()) return;
  await page.getByRole('button', { name: 'Show sidebar' }).first().click();
  await expect(railPlace(page, 'Software')).toBeVisible();
}

/** One DataTransfer for the whole gesture, so what dragstart wrote is still there at the drop. */
async function drag(source: Locator, target: Locator, page: Page, commit = true) {
  const data = await page.evaluateHandle(() => new DataTransfer());
  await source.dispatchEvent('dragstart', { dataTransfer: data, bubbles: true });
  await target.dispatchEvent('dragenter', { dataTransfer: data, bubbles: true });
  await target.dispatchEvent('dragover', { dataTransfer: data, bubbles: true });
  if (commit) await target.dispatchEvent('drop', { dataTransfer: data, bubbles: true });
  await source.dispatchEvent('dragend', { dataTransfer: data, bubbles: true });
}

for (const theme of THEMES) for (const width of WIDTHS) {
  test(`PL-142 PL-227 menu: a rail row opens its menu from the keyboard, names its rows and returns focus (${theme} ${width}px)`, async ({ page }) => {
    test.setTimeout(60_000);
    await boot(page, width, theme);
    await showRail(page);
    const place = railPlace(page, 'Software');
    await place.focus();
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: 'Software actions' });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem')).toHaveText([/^Go to/, /^Quick Look/, /^Open in new window/, 'Pin', 'Rename', 'Close', 'Close all others']);
    // The tint row is five named squares, the place's own tint is the selected one, and Graphite is never offered.
    const tint = menu.getByRole('radiogroup', { name: 'Tint' });
    await expect(tint.getByRole('radio')).toHaveText(['', '', '', '', '']);
    for (const name of ['Tide', 'Rose', 'Sage', 'Sand', 'Iris']) await expect(tint.getByRole('radio', { name })).toHaveCount(1);
    await expect(tint.getByRole('radio', { name: 'Iris' })).toHaveAttribute('aria-checked', 'true');
    await expect(tint.getByRole('radio', { name: 'Graphite' })).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(place).toBeFocused();
    // Focus is drawn for the keyboard: the 2px accent ring comes back on the row after the menu closes.
    expect(await place.evaluate(el => getComputedStyle(el).outlineStyle + getComputedStyle(el).boxShadow)).not.toMatch(/^none(none)?$/);

    await railPlace(page, 'Marketing').focus();
    await page.keyboard.press('Shift+F10');
    const pinned = page.getByRole('menu', { name: 'Marketing actions' });
    await expect(pinned.getByRole('menuitem', { name: 'Unpin' })).toBeVisible();
    await expect(pinned.getByRole('menuitem', { name: 'Pin', exact: true })).toHaveCount(0);
    await expect(pinned.getByRole('radio', { name: 'Rose' })).toHaveAttribute('aria-checked', 'true');
  });
}

for (const theme of THEMES) {
  test(`PL-227 PL-231 rename and tint from the keyboard write once, announce, and ⌘Z puts the old words back (${theme})`, async ({ page }) => {
    test.setTimeout(60_000);
    const { places, primary } = await boot(page, 1200, theme);
    // The id is read before the rename: the mock looks places up by their current name.
    const softwareId = places.id('Software');
    const softwareRow = () => places.state().places.find(row => row.id === softwareId);
    await railPlace(page, 'Software').focus();
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: 'Software actions' });
    await menu.getByRole('menuitem', { name: 'Rename' }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename “Software”' });
    await expect(dialog).toBeVisible();
    const field = dialog.getByRole('textbox', { name: 'Name' });
    await expect(field).toBeFocused();
    await expect(field).toHaveValue('Software');
    await field.fill('   ');
    await page.keyboard.press('Enter');
    // An empty name is refused before anything is written.
    expect(softwareRow()?.name).toBe('Software');
    expect(places.calls.filter(call => call.method === 'POST' && call.body.name !== undefined)).toEqual([]);
    await field.fill('Code');
    await page.keyboard.press('Enter');
    await expect(dialog).toHaveCount(0);
    await expect(railPlace(page, 'Code')).toBeVisible();
    await expect(toast(page).filter({ hasText: 'Renamed “Software” to “Code”' })).toBeVisible();
    await expect.poll(() => softwareRow()?.name).toBe('Code');
    // Closing the dialog hands focus back to the page, not to a vanished element.
    await expect(page.locator('dialog[open]')).toHaveCount(0);

    await railPlace(page, 'Code').focus();
    await page.keyboard.press('Shift+F10');
    await page.getByRole('menu', { name: 'Code actions' }).getByRole('radio', { name: 'Sand' }).click();
    await expect.poll(() => softwareRow()?.tint).toBe('sand');

    await page.locator('body').click({ position: { x: 1, y: 1 } });
    await page.keyboard.press(`${primary}+z`);
    await expect.poll(() => softwareRow()?.tint).toBe('iris');
    await page.keyboard.press(`${primary}+z`);
    await expect.poll(() => softwareRow()?.name).toBe('Software');
    await expect(railPlace(page, 'Software')).toBeVisible();
  });
}

for (const theme of THEMES) for (const width of [850, 1200] as const) {
  test(`PL-220 PL-223 PL-231 a chat dropped on a rail row is added there; any other drop is not taken; ⌘Z takes it back (${theme} ${width}px)`, async ({ page }) => {
    test.setTimeout(60_000);
    const { places, primary } = await boot(page, width, theme);
    await showRail(page);
    await railPlace(page, 'Marketing').click();
    const chat = page.locator('[data-chat-id="c-launch"]');
    await expect(chat).toBeVisible();
    const software = railRow(page, 'Software');
    const members = (name: string) => places.state().members.filter(row => row.placeId === places.id(name)).map(row => row.chatId);

    // A hover alone writes nothing, and says where it would land with the soft fill.
    await drag(chat, software, page, false);
    expect(places.posts('/members')).toEqual([]);

    await drag(chat, software, page);
    await expect.poll(() => members('Software')).toEqual(['c-launch']);
    expect(members('Marketing')).toEqual(['c-launch']);
    expect(places.posts('/members').at(-1)?.body).toMatchObject({ chats: ['c-launch'] });
    expect(places.posts('/members').at(-1)?.body.moveFrom).toBeUndefined();
    await expect(toast(page).filter({ hasText: 'Added a chat to “Software”' })).toBeVisible();

    // A link or a file on a rail row is not taken: the row never claims the drop, so the browser keeps its own behaviour.
    const claimed = await software.evaluate(el => {
      const data = new DataTransfer();
      data.setData('text/uri-list', 'https://example.com/spec');
      const over = new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true });
      el.dispatchEvent(over);
      return over.defaultPrevented;
    });
    expect(claimed).toBe(false);
    expect(places.posts('/sources')).toEqual([]);

    await page.locator('body').click({ position: { x: 1, y: 1 } });
    await page.keyboard.press(`${primary}+z`);
    await expect.poll(() => members('Software')).toEqual([]);
    expect(members('Marketing')).toEqual(['c-launch']);
  });
}

// PL-223 asks that a file, folder, URL or tab dropped on a rail row or Home becomes a source (or membership). performFiling exists and is
// unit-tested, but no surface calls it yet: PlaceRail takes chat and place payloads only. Kept as a fixme so the gap is a red line on the day
// someone wires it, not a silent omission.
test.fixme('PL-223 a URL dropped on a rail row becomes a source of that place', async ({ page }) => {
  const { places } = await boot(page, 1200, 'light');
  const row = railRow(page, 'Software');
  await row.evaluate(el => {
    const data = new DataTransfer();
    data.setData('text/uri-list', 'https://example.com/spec');
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }));
  });
  await expect.poll(() => places.posts('/sources').length).toBe(1);
});

for (const theme of THEMES) {
  test(`PL-229 PL-231 Delete place… from the keyboard asks first, then ⌘Z restores it after the toast is gone (${theme})`, async ({ page }) => {
    test.setTimeout(60_000);
    const { places, primary } = await boot(page, 1200, theme);
    await page.keyboard.press(`${primary}+Shift+KeyP`);
    await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
    await tile(page, 'Garden').locator('.places-tile-main').focus();
    await page.keyboard.press('Shift+F10');
    await page.getByRole('menuitem', { name: 'Delete place…' }).click();
    const confirm = page.getByRole('group', { name: /^Delete “Garden”\?/ });
    await expect(confirm).toBeVisible();
    await expect(confirm.getByRole('button', { name: 'Delete place', exact: true })).toBeVisible();
    expect(places.posts('/delete')).toEqual([]);
    await confirm.getByRole('button', { name: 'Delete place', exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect.poll(() => places.posts('/delete').length).toBe(1);
    await expect(tile(page, 'Garden')).toHaveCount(0);
    const gone = toast(page).filter({ hasText: /^Deleted “Garden”/ });
    await expect(gone).toBeVisible();
    // Ten seconds on the toast; the window's step outlives it.
    await page.clock.fastForward(11_000);
    await expect(gone).toHaveCount(0);
    await page.locator('body').click({ position: { x: 1, y: 1 } });
    await page.keyboard.press(`${primary}+z`);
    await expect.poll(() => places.posts('/places/undo').length).toBe(1);
    await expect(tile(page, 'Garden')).toBeVisible();
  });
}

for (const theme of THEMES) for (const width of WIDTHS) {
  test(`PL-232 History: Add to place from the keyboard files the chat in the chosen place (${theme} ${width}px)`, async ({ page }) => {
    test.setTimeout(60_000);
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.setViewportSize({ width, height: 800 });
    await page.clock.setFixedTime(HISTORY_NOW);
    await installMockEngine(page, { ...withHistory(designConversations()), places: 'typical-day' });
    const filed: { path: string; chats?: string[] }[] = [];
    await page.route('**/api/engine/places/*/members', async route => {
      if (route.request().method() === 'POST') filed.push({ path: new URL(route.request().url()).pathname, chats: route.request().postDataJSON().chats });
      await route.fallback();
    });
    await page.goto('/');
    await page.keyboard.press('Control+y');
    await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
    // The list holds focus and the selected row owns the menu, so a row is chosen first. At the narrow widths choosing opens its recap over
    // the list, and the recap's History button goes back with the choice kept.
    const row = page.getByRole('option', { name: /Does JSON5 handle this/ });
    await row.click();
    const back = page.getByRole('complementary', { name: 'Recap' }).getByRole('button', { name: 'History', exact: true });
    await expect.poll(async () => (await back.isVisible()) || (await row.isVisible())).toBe(true);
    if (await back.isVisible()) await back.click();
    await expect(row).toBeVisible();
    await page.getByRole('listbox').first().focus();
    await expect(row).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: /Does JSON5 handle this.* actions/ });
    await expect(menu.getByRole('menuitem')).toHaveText(['Continue', 'Read', 'Add to place', 'Archive']);
    await menu.getByRole('menuitem', { name: 'Add to place' }).focus();
    await page.keyboard.press('ArrowRight');
    const sub = page.locator('.app-menu-submenu');
    await expect(sub.getByRole('menuitem', { name: 'All places…' })).toBeVisible();
    await sub.getByRole('menuitem', { name: 'codeaf', exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect.poll(() => filed.length).toBe(1);
    expect(filed[0].path).toMatch(/\/places\/codeaf\/members$/);
    expect(filed[0].chats).toEqual(['json5']);
  });
}
