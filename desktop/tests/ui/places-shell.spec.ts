import { writeFile, mkdir } from 'node:fs/promises';
import { openAppearance } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine, type MockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type MockPlaces, type PlacesSeed } from './support/mock-places';

// The Places shell end to end (Places 6a–10a, Interactions): the rail, Go to, per-place tab sets, the Home composer's
// canonical order, organizing from Home, sources and instructions, Quick Look, and the offline state. Every journey
// drives real clicks and keys against the stateful Places mock and the conversation mock; nothing calls into the app.

const NEW_CHAT = 'mock-1';
/** A conversation the engine has not started yet: its journal's folder is the chat id the Home files. */
const fresh = (): Scenario => ({ initial: { entries: [], title: '', sessionFile: sessionFileFor(NEW_CHAT) }, turns: [{ entries: [{ Role: 'assistant', Text: 'On it.', Answer: true } as never] }] });

/** Three places (one pinned, one nested), two chats filed, one loose chat. */
const garden = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true },
    { name: 'Software', tint: 'iris' },
    { name: 'Config parser', parents: ['Software'] },
  ],
  chats: [
    { id: 'sess-launch', title: 'Launch copy', places: ['Marketing'] },
    { id: 'sess-parse', title: 'Parse YAML anchors', places: ['Config parser'] },
    { id: 'sess-loose', title: 'Pricing for teams' },
  ],
  live: [NEW_CHAT],
  disk: ['/work/brand'],
});

type Rig = { engine: MockEngine; places: MockPlaces; mac: boolean };
async function boot(page: Page, seed: PlacesSeed = garden(), scenario: Scenario = fresh()): Promise<Rig> {
  const engine = await installMockEngine(page, scenario);
  const places = await installMockPlaces(page, seed);
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  return { engine, places, mac };
}

const rail = (page: Page) => page.locator('.app-shell .place-rail').first();
const railRow = (page: Page, name: string) => rail(page).locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }) });
const railPlace = (page: Page, name: string) => railRow(page, name).locator('.rail-place');
const section = (page: Page, label: string) => rail(page).getByRole('region', { name: label, exact: true });
const tabs = (page: Page) => page.locator('.workspace-tabstrip').getByRole('tab');
const chooser = (page: Page) => page.locator('dialog.goto-chooser[open]');
const toast = (page: Page) => page.locator('.toast-region .toast');
const primary = (rig: Rig) => (rig.mac ? 'Meta' : 'Control');
const slot = (rig: Rig, n: number) => (rig.mac ? `Control+Digit${n}` : `Alt+Digit${n}`);
const homeTab = (page: Page) => page.locator('.workspace-tab.is-place-home');

async function goVia(page: Page, rig: Rig, name: string) {
  await page.keyboard.press(`${primary(rig)}+KeyP`);
  await expect(chooser(page)).toBeVisible();
  await chooser(page).getByRole('combobox').fill(name);
  await page.keyboard.press('Enter');
  await expect(chooser(page)).toHaveCount(0);
  await expect(homeTab(page)).toContainText(name);
}

test.beforeEach(async ({ page }) => { await page.addInitScript(() => { try { localStorage.removeItem('codeaf-theme'); } catch { /* none */ } }); });

test('a. first launch: Now and All places only; All places names a place inline and Undo takes it back', async ({ page }) => {
  const rig = await boot(page, { live: [NEW_CHAT] });
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  await expect(rail(page).getByRole('button', { name: 'All places' })).toBeVisible();
  await expect(rail(page).locator('.nav-item')).toHaveCount(2);
  await expect(rail(page).getByText('Pinned', { exact: true })).toHaveCount(0);
  await expect(rail(page).getByText('Open', { exact: true })).toHaveCount(0);

  await page.keyboard.press(`${primary(rig)}+Shift+KeyP`);
  const allTab = tabs(page).filter({ hasText: 'All places' });
  await expect(allTab).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByText('Places hold work that belongs together, with what the AI should know about it.')).toBeVisible();

  // The rail row lands on the same tab, not a second one.
  await rail(page).getByRole('button', { name: 'All places' }).click();
  await expect(allTab).toHaveCount(1);

  await page.getByRole('button', { name: 'Name a place' }).click();
  await page.getByRole('textbox', { name: 'Place name' }).fill('Garden');
  await page.keyboard.press('Enter');
  await expect.poll(() => rig.places.posts('/places').length).toBe(1);
  expect(rig.places.posts('/places')[0].body).toMatchObject({ name: 'Garden' });
  const tile = page.locator('.places-tile[data-mode="place"]').filter({ hasText: 'Garden' });
  await expect(tile).toBeVisible();
  await expect(toast(page)).toContainText('Created “Garden”');
  await toast(page).getByRole('button', { name: 'Undo' }).click();
  await expect.poll(() => rig.places.posts('/places/undo').length).toBe(1);
  await expect(tile).toHaveCount(0);
  expect(rig.places.state().places).toEqual([]);
});

test('collapsed rail switcher lists Now, the rail places and All places', async ({ page }) => {
  await boot(page);
  await railPlace(page, 'Marketing').click();
  await expect(homeTab(page)).toBeVisible();
  await rail(page).getByRole('button', { name: 'Hide sidebar' }).click();
  await expect(page.locator('.app-shell.sidebar-collapsed')).toHaveCount(1);
  await homeTab(page).getByRole('tab').click();
  const menu = page.getByRole('menu', { name: 'Place switcher' });
  await expect(menu).toBeVisible();
  // Checked rows are menuitemcheckbox; the switcher contains only place navigation.
  await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toBeVisible();
  await expect(menu.getByRole('menuitemcheckbox', { name: /^Marketing/ })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /^All places/ })).toBeVisible();
  await expect(menu.locator('[role=menuitem], [role=menuitemcheckbox]')).toHaveCount(3);
});

test('b. Go to via the chooser lands on the place Home; Now gives back its own tabs and the root palette', async ({ page }) => {
  const rig = await boot(page);
  // A draft typed in Now must survive the trip.
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await expect(composer).toBeVisible();
  await composer.fill('a draft kept in Now');
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite'); // Now is graphite (Places 9d).

  await page.keyboard.press(`${primary(rig)}+KeyP`);
  await expect(page.getByRole('dialog', { name: 'Go to a place, or create one' })).toBeVisible();
  await chooser(page).getByRole('combobox').fill('conf');
  await expect(chooser(page).getByRole('option')).toHaveCount(1);
  await page.keyboard.press('Enter');
  await expect(chooser(page)).toHaveCount(0);

  const id = rig.places.id('Config parser');
  await expect(tabs(page).first()).toHaveAccessibleName('Config parser');
  await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
  await expect(homeTab(page).locator('.place-swatch')).toHaveAttribute('data-tint-name', 'iris');
  await expect(homeTab(page).getByRole('button', { name: /^Close/ })).toHaveCount(0);
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'iris');
  await expect.poll(() => rig.places.posts('/places/rail').filter(call => call.body?.op === 'visit' && call.body?.place === id).length).toBe(1);
  await expect(section(page, 'Open').locator('.rail-place', { hasText: 'Config parser' })).toHaveAttribute('aria-current', 'page');

  await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite'); // Now is graphite (Places 9d).
  await expect(homeTab(page)).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('a draft kept in Now');
});

/** Indices in the page's own request order (both mocks), so "A, then B, then C" is read off what the browser sent. */
function order(rig: Rig, ...matchers: ((call: { method: string; path: string }) => boolean)[]) {
  return matchers.map(match => rig.places.traffic.findIndex(match));
}
const isCreate = (call: { method: string; path: string }) => call.method === 'POST' && call.path === '/sessions';
const isFiling = (placeId: string) => (call: { method: string; path: string }) => call.method === 'POST' && call.path === `/places/${placeId}/members`;
const isTurn = (call: { method: string; path: string }) => call.method === 'POST' && call.path === `/sessions/${NEW_CHAT}/turn`;

test('c. each place keeps its own tab set, across places and across a reload', async ({ page }) => {
  const rig = await boot(page);
  await railPlace(page, 'Marketing').click();
  await expect(homeTab(page)).toContainText('Marketing');
  await page.keyboard.press(`${primary(rig)}+KeyT`);
  const field = page.getByRole('combobox', { name: 'Search or start' });
  await expect(field).toBeFocused();
  // The quiet New tab is the place's second tab.
  await expect(tabs(page)).toHaveCount(2);
  await expect(tabs(page).nth(1)).toHaveAccessibleName('New tab');

  await goVia(page, rig, 'Software');
  await expect(tabs(page).first()).toHaveAccessibleName('Software');
  await expect(tabs(page).filter({ hasText: 'New tab' })).toHaveCount(0);

  await railPlace(page, 'Marketing').click();
  await expect(tabs(page).first()).toHaveAccessibleName('Marketing');
  await expect(tabs(page).nth(1)).toHaveAccessibleName('New tab');

  await page.reload();
  await expect(tabs(page).first()).toHaveAccessibleName('Marketing');
  await expect(tabs(page).nth(1)).toHaveAccessibleName('New tab');
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
});

test('d. the Home composer creates, files, then sends; ↵ focuses the new tab and ⌘↵ leaves it behind', async ({ page }) => {
  const rig = await boot(page);
  const id = rig.places.id('Marketing');
  await railPlace(page, 'Marketing').click();
  const composer = page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
  await composer.fill('Draft the launch email');
  await composer.press('Enter');
  const opened = tabs(page).filter({ hasText: 'Draft the launch email' });
  await expect(opened).toHaveAttribute('aria-selected', 'true');
  await expect(tabs(page).nth(1)).toHaveAccessibleName('Draft the launch email');
  const [create, file, turn] = order(rig, isCreate, isFiling(id), isTurn);
  expect(create).toBeGreaterThanOrEqual(0);
  expect(rig.places.traffic[create].body).toEqual({ placeId: id });
  expect(file).toBeGreaterThan(create);
  expect(turn).toBeGreaterThan(file);
  expect(rig.places.traffic[file].body).toMatchObject({ chats: [NEW_CHAT] });

  await tabs(page).first().click();
  await composer.fill('Second thought, in the background');
  await composer.press(`${primary(rig)}+Enter`);
  // The conversation mock holds one session, so the background tab takes that session's engine title; what matters is
  // that a third tab opened, its turn went out, and Home kept the focus.
  await expect(tabs(page)).toHaveCount(3);
  await expect.poll(() => rig.engine.calls.filter(call => call.path.endsWith('/turn')).map(call => call.body.text)).toContain('Second thought, in the background');
  await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
});

test('d. a refused filing sends nothing, keeps the words and says why', async ({ page }) => {
  const rig = await boot(page);
  rig.places.fail('members', { status: 409, error: 'Places are busy in another window. Try again in a moment.', code: 'busy' });
  await railPlace(page, 'Marketing').click();
  const composer = page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
  await composer.fill('Draft the launch email');
  await composer.press('Enter');
  await expect(page.locator('.home-composer').getByRole('alert')).toContainText('Places are busy in another window');
  await expect(page.locator('.home-composer').getByRole('alert')).toContainText('“Marketing”');
  await expect(composer).toHaveValue('Draft the launch email');
  expect(rig.engine.calls.filter(call => call.path.endsWith('/turn'))).toEqual([]);
  await expect(tabs(page)).toHaveCount(1);
});

test('e. ⌘T inside a place files the new chat before its first turn', async ({ page }) => {
  const rig = await boot(page);
  const id = rig.places.id('Marketing');
  await railPlace(page, 'Marketing').click();
  await page.keyboard.press(`${primary(rig)}+KeyT`);
  const field = page.getByRole('combobox', { name: 'Search or start' });
  await field.fill('Plan the webinar');
  await field.press('Enter');
  const message = page.getByRole('textbox', { name: 'Message', exact: true });
  // Whether Enter sent straight away or left the words as a draft, the first turn must follow the filing.
  if (await message.inputValue().catch(() => '') === 'Plan the webinar') await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect.poll(() => order(rig, isTurn)[0]).toBeGreaterThanOrEqual(0);
  const [create, file, turn] = order(rig, isCreate, isFiling(id), isTurn);
  expect(create).toBeGreaterThanOrEqual(0);
  expect(rig.places.traffic[create].body).toEqual({ placeId: id });
  expect(file, 'POST /places/{id}/members must come before the first turn').toBeGreaterThan(create);
  expect(turn).toBeGreaterThan(file);
});

/** A rail with something in each section: one pinned place, three open ones, one of them busy. */
const busyRail = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true },
    { name: 'Software', tint: 'iris', lastOpenedAt: 'now' },
    { name: 'Config parser', parents: ['Software'], lastOpenedAt: 'now' },
    { name: 'Docs', tint: 'tide', lastOpenedAt: 'now' },
  ],
  chats: [{ id: 'sess-run', title: 'Update fixtures', places: ['Docs'], live: true, doing: 'working', tasks: { running: 1 } }],
  live: [NEW_CHAT],
});

test('f. rail: pin from the menu, close from × and ⌘⇧W, closed-but-running stays muted, needs-you dot, slot keys', async ({ page }) => {
  const rig = await boot(page, busyRail());
  const software = rig.places.id('Software');
  await expect(section(page, 'Pinned').locator('.rail-place')).toHaveCount(1);
  await expect(section(page, 'Open').locator('.rail-place')).toHaveCount(3);

  // Pin by right-click.
  await railPlace(page, 'Software').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Pin', exact: true }).click();
  await expect.poll(() => rig.places.posts(`/places/${software}/pin`).length).toBe(1);
  await expect(section(page, 'Pinned').locator('.rail-place', { hasText: 'Software' })).toBeVisible();
  await expect(section(page, 'Open').locator('.rail-place', { hasText: /^Software/ })).toHaveCount(0);

  // Pin by keyboard: Shift F10 on the focused row.
  const parser = rig.places.id('Config parser');
  await railPlace(page, 'Config parser').focus();
  await page.keyboard.press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Pin', exact: true }).click();
  await expect.poll(() => rig.places.posts(`/places/${parser}/pin`).length).toBe(1);
  await expect(section(page, 'Pinned').locator('.rail-place', { hasText: 'Config parser' })).toBeVisible();
  // The rail is ready to unpin it again from the same menu.
  await railPlace(page, 'Config parser').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Unpin', exact: true }).click();
  await expect(section(page, 'Pinned').locator('.rail-place', { hasText: 'Config parser' })).toHaveCount(0);

  // × on hover takes a quiet place off Open.
  const parserRow = railRow(page, 'Config parser');
  await parserRow.hover();
  await parserRow.getByRole('button', { name: 'Close Config parser' }).click();
  await expect(railRow(page, 'Config parser')).toHaveCount(0);

  // A closed place with work running stays, muted, and says so.
  const docsRow = railRow(page, 'Docs');
  await docsRow.hover();
  await docsRow.getByRole('button', { name: 'Close Docs' }).click();
  await expect(railRow(page, 'Docs')).toHaveAttribute('data-busy-closed', 'true');
  await expect(railRow(page, 'Docs')).toContainText('closed · still running');

  // ⌘⇧W closes the place the window shows and the window goes to Now.
  await railPlace(page, 'Docs').click();
  await expect(homeTab(page)).toContainText('Docs');
  await expect(railRow(page, 'Docs')).not.toHaveAttribute('data-busy-closed', 'true');
  await page.keyboard.press(`${primary(rig)}+Shift+KeyW`);
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite'); // Now is graphite (Places 9d).
  await expect(homeTab(page)).toHaveCount(0);
  await expect(railRow(page, 'Docs')).toContainText('closed · still running');

  // A needs-you roll-up draws the amber dot with words a screen reader can say.
  rig.places.setStatus(software, { needsYou: 1 });
  const dot = railRow(page, 'Software').locator('.status-mark');
  await expect(dot).toHaveAttribute('data-status', 'waiting');
  await expect(dot).toHaveAccessibleName(/needs? you/);

  // ⌃1 / Alt 1 is the first rail place; ⌃0 / Alt 0 is Now.
  await page.keyboard.press(slot(rig, 1));
  await expect(homeTab(page)).toContainText('Marketing');
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
  await page.keyboard.press(slot(rig, 0));
  await expect(homeTab(page)).toHaveCount(0);
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toHaveAttribute('aria-current', 'page');
});

const tile = (page: Page, name: string) => page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: new RegExp(`^${name}$`) }) });
async function openAllPlaces(page: Page, rig: Rig) {
  await page.keyboard.press(`${primary(rig)}+Shift+KeyP`);
  await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
}

test('g. Merge into… opens the chooser without the place itself and merges', async ({ page }) => {
  const rig = await boot(page);
  const software = rig.places.id('Software'), marketing = rig.places.id('Marketing');
  await openAllPlaces(page, rig);
  await tile(page, 'Marketing').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Merge into…' }).click();
  const sheet = page.getByRole('dialog', { name: 'Merge “Marketing” into…' });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByRole('option', { name: /^Marketing/ })).toHaveCount(0);
  await sheet.getByRole('combobox').fill('Soft');
  await page.keyboard.press('Enter');
  await expect.poll(() => rig.places.posts('/merge').map(call => [call.path, call.body.into])).toEqual([[`/places/${marketing}/merge`, software]]);
  await expect(toast(page)).toContainText('Merged “Marketing” into “Software”');
  await expect(tile(page, 'Marketing')).toHaveCount(0);
});

test('g. Add to another place… and a chat row’s Add to a place… each make one write', async ({ page }) => {
  const rig = await boot(page);
  const parser = rig.places.id('Config parser'), marketing = rig.places.id('Marketing'), software = rig.places.id('Software');
  await goVia(page, rig, 'Software');
  await tile(page, 'Config parser').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Add to another place…' }).click();
  const sheet = page.getByRole('dialog', { name: 'Add “Config parser” to another place…' });
  await expect(sheet.getByRole('option', { name: /^Config parser/ })).toHaveCount(0);
  await sheet.getByRole('combobox').fill('Marketing');
  await page.keyboard.press('Enter');
  await expect.poll(() => rig.places.posts(`/places/${parser}/parents`).map(call => call.body)).toEqual([{ add: marketing, ifGeneration: 0 }]);
  await expect(toast(page).last()).toContainText('Added “Config parser” to “Marketing”');

  await openAllPlaces(page, rig);
  const loose = page.locator('.places-chat-row', { hasText: 'Pricing for teams' });
  await loose.locator('.places-chat-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Add to a place…' }).click();
  await page.getByRole('dialog', { name: 'Add “Pricing for teams” to a place…' }).getByRole('combobox').fill('Software');
  await page.keyboard.press('Enter');
  await expect.poll(() => rig.places.posts(`/places/${software}/members`).map(call => call.body)).toEqual([{ chats: ['sess-loose'], ifGeneration: 1 }]);
  await expect(page.locator('.places-chat-row', { hasText: 'Pricing for teams' })).toHaveCount(0);
});

test('g. Delete place… says what will happen first, then offers Undo', async ({ page }) => {
  const rig = await boot(page);
  const software = rig.places.id('Software');
  await openAllPlaces(page, rig);
  await tile(page, 'Software').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete place…' }).click();
  const confirm = page.getByRole('group', { name: /Delete “Software”/ });
  await expect(confirm).toContainText('Delete “Software”? 1 place moves up. No chat is deleted.');
  expect(rig.places.posts('/delete')).toEqual([]);
  await confirm.getByRole('button', { name: 'Delete place' }).click();
  await expect.poll(() => rig.places.posts(`/places/${software}/delete`).length).toBe(1);
  await expect(toast(page)).toContainText('Deleted “Software”. No chat was deleted.');
  await expect(tile(page, 'Software')).toHaveCount(0);
  // Config parser moved up to the top level.
  await expect(tile(page, 'Config parser')).toBeVisible();
  await toast(page).getByRole('button', { name: 'Undo' }).click();
  await expect.poll(() => rig.places.posts('/places/undo').length).toBe(1);
  await expect(tile(page, 'Software')).toBeVisible();
  await expect(tile(page, 'Config parser')).toHaveCount(0);
});

test('h. an empty place takes a typed folder and instructions; a refused source keeps the engine’s sentence', async ({ page }) => {
  const rig = await boot(page, { places: [{ name: 'Studio', tint: 'sage' }], live: [NEW_CHAT], disk: ['/work/brand'] });
  const studio = rig.places.id('Studio');
  await goVia(page, rig, 'Studio');
  await expect(page.getByText('Nothing here yet.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Add files or links' }).click();
  const sheet = page.getByRole('dialog', { name: 'Files and links for “Studio”' });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByText('Choosing from disk needs the desktop app. Paste an absolute path or a link.')).toBeVisible();
  const field = sheet.getByRole('textbox', { name: 'Add a path or a link' });
  await expect(field).toBeFocused();
  await field.fill('/work/brand');
  await sheet.getByRole('radio', { name: 'Folder' }).click();
  await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect.poll(() => rig.places.posts(`/places/${studio}/sources`).map(call => call.body)).toEqual([{ kind: 'folder', ref: '/work/brand', ifGeneration: 0 }]);
  await expect(sheet.getByRole('list', { name: 'Files and links' })).toContainText('brand');
  // The same folder again is refused, in the engine's words, and the sheet stays.
  await field.fill('/work/brand');
  await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(sheet.getByRole('alert')).toHaveText('That is already in this place.');
  await sheet.getByRole('button', { name: 'Done' }).click();
  await expect(sheet).toHaveCount(0);

  await page.getByRole('button', { name: 'Write instructions' }).click();
  const notes = page.getByRole('region', { name: 'Instructions' });
  const instructions = notes.getByRole('textbox', { name: 'Instructions' });
  await expect(instructions).toBeFocused();
  await instructions.fill('Use the brand voice.');
  await instructions.blur();
  await expect.poll(() => rig.places.posts(`/places/${studio}`).map(call => call.body)).toEqual([{ instructions: 'Use the brand voice.', ifGeneration: 1 }]);
  expect(rig.places.state().places[0].instructions).toBe('Use the brand voice.');
});

test('i. Quick Look: Space opens and closes the sheet on a tile, focus comes back, nothing navigates', async ({ page }) => {
  const rig = await boot(page);
  await openAllPlaces(page, rig);
  const button = tile(page, 'Marketing').locator('.places-tile-main');
  await button.focus();
  const visits = rig.places.posts('/visit').length;
  await page.keyboard.press('Space');
  const sheet = page.getByRole('dialog', { name: 'Quick Look: Marketing' });
  await expect(sheet).toBeVisible();
  await expect(sheet).toContainText('Launch copy');
  await page.keyboard.press('Space');
  await expect(sheet).toHaveCount(0);
  await expect(button).toBeFocused();
  await expect(tabs(page).filter({ hasText: 'All places' })).toHaveAttribute('aria-selected', 'true');
  await expect(homeTab(page)).toHaveCount(0);
  expect(rig.places.posts('/visit').length).toBe(visits);
});

test('j. a terminal tab and a file tab still open inside a place strip', async ({ page }) => {
  const b64 = (body: string) => Buffer.from(body).toString('base64');
  const rig = await boot(page, garden(), { ...fresh(), files: { 'internal/parse/lexer.go': { mime: 'text/x-go', dataBase64: b64('package parse\n// lexer content') } } });
  await page.route('**/api/engine/sessions/*/files/find*', route => route.fulfill({ json: { files: [{ path: 'internal/parse/lexer.go', name: 'lexer.go', dir: 'internal/parse' }] } }));
  await railPlace(page, 'Marketing').click();
  // A chat in this place gives the strip a session to read files through.
  const composer = page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
  await composer.fill('Where is the lexer?');
  await composer.press('Enter');
  await expect(tabs(page)).toHaveCount(2);

  await page.keyboard.press('Control+Backquote');
  await expect(page.locator('.workspace-tab[data-kind="terminal"]')).toHaveCount(1);
  await expect(tabs(page).first()).toHaveAccessibleName('Marketing');

  // Focus is in the terminal: off a Mac plain Ctrl+T belongs to the shell, so the new tab is Ctrl+Shift+T there.
  await page.keyboard.press(rig.mac ? 'Meta+KeyT' : 'Control+Shift+KeyT');
  await page.getByRole('combobox', { name: 'Search or start' }).fill('lex');
  await page.getByRole('option', { name: /lexer\.go/ }).click();
  await expect(page.getByRole('tab', { name: 'lexer.go', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('.file-text').getByText('// lexer content', { exact: true })).toBeVisible();
  await expect(tabs(page).first()).toHaveAccessibleName('Marketing');
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
});

test('k. offline: the rail and All places say the engine is out of reach, invent nothing, and Retry recovers', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, garden());
  places.fail('graph', 'abort');
  places.fail('home', 'abort');
  // Graph reads that started while the engine was unreachable must finish failing before it
  // is allowed to answer. A read still queued in the route handler otherwise succeeds after
  // the flag drops and takes the rail's Retry away before the click.
  const pendingGraphs = new Set<import('@playwright/test').Request>();
  const isGraph = (url: string, method: string) => method === 'GET' && /\/api\/engine\/places(\?|$)/.test(url);
  page.on('request', request => { if (isGraph(request.url(), request.method())) pendingGraphs.add(request); });
  page.on('requestfailed', request => pendingGraphs.delete(request));
  page.on('requestfinished', request => pendingGraphs.delete(request));
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  const rig: Rig = { engine, places, mac };
  await expect(rail(page).getByRole('status')).toContainText('Can’t reach the engine');
  await expect(rail(page).getByRole('button', { name: 'Retry' })).toBeVisible();
  await expect(rail(page).locator('.rail-place')).toHaveCount(0);
  await expect(rail(page).getByText('Pinned', { exact: true })).toHaveCount(0);

  await page.keyboard.press(`${primary(rig)}+Shift+KeyP`);
  const notice = page.locator('.home-notice');
  await expect(notice).toContainText('Can’t reach the engine.');
  await expect(page.locator('.places-tile[data-mode="place"]')).toHaveCount(0);
  await expect(page.locator('.places-chat-row')).toHaveCount(0);

  // The Home's own Retry reads again once the engine answers; the rail's Retry does the same for the rail.
  places.fail('home', undefined);
  await notice.getByRole('button', { name: 'Retry' }).click();
  await expect(tile(page, 'Marketing')).toBeVisible();
  await expect.poll(() => pendingGraphs.size).toBe(0);
  await expect(rail(page).getByRole('button', { name: 'Retry' })).toBeVisible();
  places.fail('graph', undefined);
  await rail(page).getByRole('button', { name: 'Retry' }).evaluate(button => (button as HTMLButtonElement).click());
  await expect(railPlace(page, 'Marketing')).toBeVisible();
  await expect(rail(page).getByText('Can’t reach the engine')).toHaveCount(0);
});

test('a waiting question appears once on the Next up frame pill', async ({ page }) => {
  await boot(page, {
    places: [{ name: 'Software', tint: 'iris' }],
    chats: [{ id: 'sess-need', title: 'Port fix to v1', places: ['Software'], needsYou: true, reason: 'Allow the v1 branch push?' }],
    live: [NEW_CHAT],
  });
  const pill = page.getByRole('button', { name: /^1 need you elsewhere/ });
  await expect(pill).toHaveCount(1);
  await expect(pill).toBeVisible();
  await expect(rail(page).locator('.frame-pill')).toHaveCount(0);
});

async function setTheme(page: Page, theme: 'Light' | 'Dark') {
  await openAppearance(page);
  await page.getByRole('option', { name: `${theme} appearance`, exact: true }).click();
}

for (const theme of ['Light', 'Dark'] as const) {
  test(`l. accessible in ${theme}: place Home, All places, the chooser and a dialog`, async ({ page }) => {
    const rig = await boot(page);
    await setTheme(page, theme);
    await railPlace(page, 'Marketing').click();
    await expect(page.locator('.home-page')).toContainText('Launch copy');
    await expectAccessible(page);
    await openAllPlaces(page, rig);
    await expect(tile(page, 'Marketing')).toBeVisible();
    await expectAccessible(page);
    await page.keyboard.press(`${primary(rig)}+KeyP`);
    await expect(chooser(page)).toBeVisible();
    await expectAccessible(page);
    await page.keyboard.press('Escape');
    await expect(chooser(page)).toHaveCount(0);
    // A place dialog: Rename… from the rail row's menu.
    await railPlace(page, 'Marketing').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Rename…' }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename “Marketing”' });
    await expect(dialog).toBeVisible();
    await expectAccessible(page);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
  });
}

for (const width of [320, 600]) {
  test(`l. no horizontal overflow at ${width}px on a place Home and All places${width === 320 ? ' (rail in the drawer)' : ''}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 640 });
    const engine = await installMockEngine(page, fresh());
    const places = await installMockPlaces(page, garden());
    await page.goto('/');
    const rig: Rig = { engine, places, mac: await page.evaluate(() => /Mac/.test(navigator.platform)) };
    const overflow = () => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    // Below the wide layout the rail is put away: Show sidebar brings it back (the drawer at the small breakpoint).
    await page.getByRole('button', { name: 'Show sidebar' }).first().click();
    const drawer = page.getByRole('dialog', { name: 'Navigation', exact: true });
    if (width <= 320) await expect(drawer).toBeVisible();
    await page.locator('.place-rail .rail-place:visible', { hasText: 'Marketing' }).first().click();
    if (width <= 320) await expect(drawer).not.toBeVisible();
    await expect(homeTab(page)).toContainText('Marketing');
    await expect(page.locator('.home-page')).toContainText('Launch copy');
    expect(await overflow()).toBe(true);
    await expectAccessible(page);
    await openAllPlaces(page, rig);
    await expect(tile(page, 'Software')).toBeVisible();
    expect(await overflow()).toBe(true);
    await page.keyboard.press(`${primary(rig)}+KeyP`);
    await expect(chooser(page)).toBeVisible();
    expect(await overflow()).toBe(true);
  });
}

for (const theme of ['light', 'dark'] as const) {
 test(`Home passes its saved folder place before the host opens · ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  const rig = await boot(page, { places: [{ name: 'Studio', tint: 'sage' }], live: [NEW_CHAT], disk: ['/work/brand'] });
  const studio = rig.places.id('Studio');
  await goVia(page, rig, 'Studio');
  await page.getByRole('button', { name: 'Add files or links' }).click();
  const sheet = page.getByRole('dialog', { name: 'Files and links for “Studio”' });
  await sheet.getByRole('textbox', { name: 'Add a path or a link' }).fill('/work/brand');
  await sheet.getByRole('radio', { name: 'Folder' }).click();
  await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect.poll(() => rig.places.posts(`/places/${studio}/sources`).length).toBe(1);
  await sheet.getByRole('button', { name: 'Done', exact: true }).click();
  const starts: Record<string, unknown>[] = [];
  await page.route('**/api/engine/sessions', async route => {
    const body = route.request().postDataJSON();
    starts.push(body);
    if (!body.sessionFile) {
      expect(body).toEqual({ placeId: studio });
      rig.engine.update({ workspace: '/work/brand', workingFolder: { from: 'place', path: '/work/brand', label: 'brand' } });
    } else expect(body).toEqual({ sessionFile: sessionFileFor(NEW_CHAT) });
    await route.fulfill({ json: rig.engine.snapshot() });
  });
  const composer = page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
  await composer.fill('Show the actual folder');
  await composer.press('Enter');
  await expect(tabs(page).filter({ hasText: 'Show the actual folder' })).toHaveAttribute('aria-selected', 'true');
  expect(starts[0]).toEqual({ placeId: studio });
  await expect.poll(() => rig.engine.calls.some(call => call.path.endsWith('/turn'))).toBe(true);
  expect(rig.engine.snapshot().workspace).toBe('/work/brand');
  expect(rig.engine.snapshot().workingFolder?.from).toBe('place');
 });
 test(`populated Home can add its first folder, then start using it · ${theme}`, async ({ page }, info) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  const rig = await boot(page, { places: [{ name: 'Studio', tint: 'sage' }], chats: [{ id: 'existing', title: 'Earlier work', places: ['Studio'] }], live: [NEW_CHAT], disk: ['/work/brand'] });
  const studio = rig.places.id('Studio');
  await goVia(page, rig, 'Studio');
  await expect(page.getByText('Earlier work', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add files or links', exact: true })).toHaveCount(1);
  await page.getByRole('button', { name: 'Add files or links', exact: true }).click();
  const sheet = page.getByRole('dialog', { name: 'Files and links for “Studio”' });
  const path = sheet.getByRole('textbox', { name: 'Add a path or a link' });
  await path.fill('/not-present'); await sheet.getByRole('radio', { name: 'Folder' }).click();
  await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(sheet.getByRole('alert')).toContainText("That path doesn't exist.");
  expect(rig.places.state().places.find(place => place.id === studio)?.sources).toHaveLength(0);
  await path.fill('/work/brand'); await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(sheet.getByRole('list', { name: 'Files and links' })).toContainText('brand');
  await sheet.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(page.locator('.home-source-list')).toContainText('brand');
  await expect(page.getByRole('button', { name: 'Add files or links', exact: true })).toHaveCount(1);
  if (process.env.HOME_SOURCE_SHOTS) await page.screenshot({ path: `${process.env.HOME_SOURCE_SHOTS}/populated-source-${theme}-${info.project.name}.png` });
  await page.route('**/api/engine/sessions', async route => {
    if (route.request().method() !== 'POST') { await route.fallback(); return; }
    const body = route.request().postDataJSON();
    if (!body.sessionFile) {
      expect(body).toEqual({ placeId: studio });
      rig.engine.update({ workspace: '/work/brand', workingFolder: { from: 'place', path: '/work/brand', label: 'brand' } });
    }
    await route.fulfill({ json: rig.engine.snapshot() });
  });
  await page.route('**/api/engine/sessions/*/using', async route => {
    const state = rig.places.state();
    const place = state.places.find(place => place.id === studio)!;
    const sources = place.sources.map(source => ({ key: source.id, kind: source.kind, ref: source.ref, label: source.label, status: 'ok', from: [{ placeId: studio, sourceId: source.id, addedBy: 'you', level: 0 }] }));
    await route.fulfill({ json: { chatId: NEW_CHAT, engine: { places: true }, revision: state.revision, readAt: '2026-10-09T12:00:00Z', settings: [], bundle: { chatId: NEW_CHAT, revision: state.revision, places: [{ id: studio, name: 'Studio', tint: 'sage', level: 0, inherited: false }], instructions: [], sources, trimmed: [], refused: [], policy: [], counts: { places: 1, sources: sources.length } } } });
  });
  const composer = page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
  await composer.fill('Use the folder added after earlier work'); await composer.press('Enter');
  await expect(tabs(page).filter({ hasText: 'Use the folder added after earlier work' })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'Using 1 place · 1 source', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'What this conversation is using' })).toContainText('brand');
  expect(rig.engine.snapshot().workspace).toBe('/work/brand');
  if (process.env.HOME_SOURCE_SHOTS) await page.screenshot({ path: `${process.env.HOME_SOURCE_SHOTS}/using-source-${theme}-${info.project.name}.png` });
 });

}


for (const theme of ['light', 'dark'] as const) {
  test(`Home composer exact inline geometry and retained draft controls · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await boot(page);
    await railPlace(page, 'Marketing').click();
    const surface = page.locator('.home-composer .composer');
    const field = surface.getByRole('textbox', { name: 'Message' });
    const send = surface.getByRole('button', { name: 'Send', exact: true });
    await expect(surface).toHaveCSS('border-radius', '20px');
    await expect(surface).toHaveCSS('padding', '10px 8px 10px 16px');
    await expect(surface).toHaveCSS('column-gap', '8px');
    expect((await surface.boundingBox())!.width).toBe(616);
    expect((await surface.boundingBox())!.height).toBe(48);
    await expect(field).toHaveCSS('font-size', '13px');
    await expect(field).toHaveCSS('line-height', 'normal');
    await expect(surface.locator('.model-picker')).toHaveText('DS Flash');
    await expect(surface.locator('.model-picker')).toHaveCSS('font-size', '12px');
    await expect(surface.getByRole('button', { name: 'Attach files' })).toHaveCount(0);
    await expect(send).toBeDisabled();
    if (process.env.HOME_COMPOSER_SHOTS) await surface.screenshot({ path: `${process.env.HOME_COMPOSER_SHOTS}/actual-${theme}-${test.info().project.name}.png` });
    const fieldBox = (await field.boundingBox())!;
    const sendBox = (await send.boundingBox())!;
    expect(sendBox.width).toBe(28); expect(sendBox.height).toBe(28);
    expect(Math.abs(fieldBox.y + fieldBox.height / 2 - sendBox.y - sendBox.height / 2)).toBeLessThan(1);
    await field.click();
    await expect(field).not.toHaveAttribute('data-keyboard', 'true');
    await page.keyboard.press('Tab');
    await page.keyboard.press('Shift+Tab');
    await expect(field).toHaveAttribute('data-keyboard', 'true');
    await expect(field).not.toHaveCSS('box-shadow', 'none');
    if (process.env.HOME_COMPOSER_SHOTS) await surface.screenshot({ path: `${process.env.HOME_COMPOSER_SHOTS}/focus-${theme}-${test.info().project.name}.png` });
    await field.fill('First line\nSecond line\nThird line');
    expect((await field.boundingBox())!.height).toBeGreaterThan(fieldBox.height);
    await expect(send).toBeEnabled();
    await field.fill('');
    await field.evaluate(el => {
      const paste = new DataTransfer(); paste.setData('text/plain', Array.from({ length: 40 }, (_, i) => `Line ${i}`).join('\n'));
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: paste, bubbles: true, cancelable: true }));
    });
    await expect(surface.locator('.paste-card')).toBeVisible();
    if (process.env.HOME_COMPOSER_SHOTS) await surface.screenshot({ path: `${process.env.HOME_COMPOSER_SHOTS}/paste-${theme}-${test.info().project.name}.png` });
    await expect(send).toBeEnabled();
    await surface.getByRole('button', { name: 'Remove pasted text' }).click();
    await expect(send).toBeDisabled();
    await surface.getByTestId('composer-file-input').setInputFiles({ name: 'context.txt', mimeType: 'text/plain', buffer: Buffer.from('A source for this draft') });
    await expect(surface.getByRole('list', { name: 'Attachments' })).toBeVisible();
    if (process.env.HOME_COMPOSER_SHOTS) await surface.screenshot({ path: `${process.env.HOME_COMPOSER_SHOTS}/attachment-${theme}-${test.info().project.name}.png` });
    await surface.getByRole('button', { name: 'Remove context.txt' }).click();
    await page.setViewportSize({ width: 320, height: 800 });
    await expect(send).toBeVisible();
    await field.fill('A long unbroken draft ' + 'word'.repeat(100));
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    expect((await surface.boundingBox())!.width).toBeLessThanOrEqual(320);
    if (process.env.HOME_COMPOSER_SHOTS) await surface.screenshot({ path: `${process.env.HOME_COMPOSER_SHOTS}/narrow-long-${theme}-${test.info().project.name}.png` });
  });
}


test('Home model label follows the saved Conversation role before a host exists', async ({ page }) => {
  const sample = fresh();
  sample.models = [{ id: 'deepseek/deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash' }, { id: 'fixture/alternate', name: 'Fixture Alternate' }];
  const rig = await boot(page, garden(), sample);
  await page.route('**/models/roles', route => route.fulfill({ json: { default: 'deepseek/deepseek-v4.1-flash', roles: [{ id: 'conversation', model: 'fixture/alternate' }] } }));
  await railPlace(page, 'Marketing').click();
  await page.evaluate(() => window.dispatchEvent(new Event('codeaf:models-changed')));
  await expect(page.locator('.home-composer .model-picker')).toHaveText('Alternate');
  await expect(page.locator('.home-composer .model-picker')).toHaveAccessibleName('Model: Fixture Alternate');
  expect(rig.engine.calls.filter(isCreate)).toHaveLength(0);
});


// The Home chip names the model the first message will really run on. Two things can decide it — the saved Conversation
// role and the place's own default (Policy.Model) — so all four pairings are driven: a place that decides wins over any
// role, and a place that decides nothing leaves the role in charge. Every model here is a fixture id; none is called.
const ROLE_DEFAULT = 'deepseek/deepseek-v4.1-flash';
const ROLE_CHANGED = 'fixture/alternate';
const PLACE_MODEL = 'fixture/placed';
const CATALOG = [{ id: ROLE_DEFAULT, name: 'DeepSeek V4.1 Flash' }, { id: ROLE_CHANGED, name: 'Fixture Alternate' }, { id: PLACE_MODEL, name: 'Fixture Placed' }];
const roleCases = [{ role: 'default', model: ROLE_DEFAULT, word: 'DS Flash', name: 'DeepSeek v4.1 Flash' }, { role: 'changed', model: ROLE_CHANGED, word: 'Alternate', name: 'Fixture Alternate' }];

for (const { role, model, word, name } of roleCases) {
  for (const placed of [false, true]) {
    test(`Home model caption · role ${role} · place ${placed ? 'sets a model' : 'sets none'}`, async ({ page }) => {
      const seed = garden();
      if (placed) seed.places![0].model = PLACE_MODEL;
      const sample = fresh();
      sample.models = CATALOG;
      const rig = await boot(page, seed, sample);
      await page.route('**/models/roles', route => route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model }] } }));
      await railPlace(page, 'Marketing').click();
      await page.evaluate(() => window.dispatchEvent(new Event('codeaf:models-changed')));
      const chip = page.locator('.home-composer .model-picker');
      if (placed) {
        // The place decides: its model, whatever the role says, with no swap that would pretend to change it.
        await expect(chip).toHaveAccessibleName('Model: Fixture Placed');
        await expect(chip).toHaveText('Placed');
        await chip.hover();
        await expect(page.getByRole('tooltip')).toHaveText('Model set by Marketing.');
        await expect(page.locator('.home-composer-note')).toHaveCount(0);
        await chip.click();
        const rows = page.getByRole('dialog').getByRole('option');
        await expect(page.getByRole('dialog').getByText('Fixture Alternate')).toHaveCount(0);
        expect(await rows.count()).toBeLessThanOrEqual(1);
        await page.keyboard.press('Escape');
      } else {
        await expect(chip).toHaveAccessibleName(`Model: ${name}`);
        await expect(chip).toHaveText(word);
        await expect(page.getByRole('tooltip')).toHaveCount(0);
      }
      expect(rig.engine.calls.filter(isCreate)).toHaveLength(0);
      // A typed first send is unchanged by any of it: the session is made first, the chat filed, then the turn.
      const composer = page.getByRole('textbox', { name: 'Message', exact: true });
      await composer.fill('Plan the launch');
      await composer.press('Enter');
      await expect(tabs(page).filter({ hasText: 'Plan the launch' })).toHaveAttribute('aria-selected', 'true');
      const [create, file, turn] = order(rig, isCreate, isFiling(rig.places.id('Marketing')), isTurn);
      expect(create).toBeGreaterThanOrEqual(0);
      expect(file).toBeGreaterThan(create);
      expect(turn).toBeGreaterThan(file);
    });
  }
}

test('Home model caption follows a role change only while no place decides, and never names a guess', async ({ page }) => {
  const seed = garden();
  seed.places![1].model = PLACE_MODEL; // Software decides; its child Config parser inherits.
  seed.places![2].pinned = true; // so the rail lists it
  const sample = fresh();
  sample.models = CATALOG;
  let role = ROLE_DEFAULT;
  const rig = await boot(page, seed, sample);
  await page.route('**/models/roles', route => route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model: role }] } }));
  const chip = page.locator('.home-composer .model-picker');
  await railPlace(page, 'Marketing').click();
  await expect(chip).toHaveText('DS Flash');
  role = ROLE_CHANGED;
  await page.evaluate(() => window.dispatchEvent(new Event('codeaf:models-changed')));
  await expect(chip).toHaveText('Alternate');
  await railPlace(page, 'Config parser').click();
  await expect(chip).toHaveAccessibleName('Model: Fixture Placed');
  role = ROLE_DEFAULT;
  await page.evaluate(() => window.dispatchEvent(new Event('codeaf:models-changed')));
  await expect(chip).toHaveAccessibleName('Model: Fixture Placed');
  // The engine cannot say: no model is named at all.
  rig.places.fail('effective-model', { status: 503, error: 'The engine is busy.' });
  await railPlace(page, 'Marketing').click();
  await railPlace(page, 'Config parser').click();
  await expect(page.locator('.home-composer')).toBeVisible();
  await expect(chip).toHaveCount(0);
});

// A place that decides the default model is shown with the same word the role path gives it, and the unknown is never filled in
// with the default: an unreadable role or list names nothing, or the role's own id, never "DS Flash" by guess.
test('Home names a place-decided default model exactly, and keeps its caption inside the composer row', async ({ page }) => {
  const seed = garden();
  seed.places![0].model = ROLE_DEFAULT;
  const sample = fresh();
  sample.models = CATALOG;
  await boot(page, seed, sample);
  await page.route('**/models/roles', route => route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model: ROLE_CHANGED }] } }));
  await railPlace(page, 'Marketing').click();
  const chip = page.locator('.home-composer .model-picker');
  await expect(chip).toHaveAccessibleName('Model: DeepSeek v4.1 Flash');
  await expect(chip).toHaveText('DS Flash');
  const box = await chip.boundingBox();
  const composer = await page.locator('.home-composer').boundingBox();
  expect(box && composer && box.y >= composer.y && box.y + box.height <= composer.y + composer.height).toBe(true);
});

for (const failing of ['models/roles', 'models']) {
  test(`Home never names the default when ${failing} cannot be read`, async ({ page }) => {
    const sample = fresh();
    sample.models = CATALOG;
    await boot(page, garden(), sample);
    await page.route('**/models/roles', route => failing === 'models/roles'
      ? route.fulfill({ status: 503, json: { error: 'busy' } })
      : route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model: ROLE_CHANGED }] } }));
    if (failing === 'models') await page.route(/\/models(\?.*)?$/, route => route.fulfill({ status: 503, json: { error: 'busy' } }));
    await railPlace(page, 'Marketing').click();
    await page.evaluate(() => window.dispatchEvent(new Event('codeaf:models-changed')));
    await expect(page.locator('.home-composer')).toBeVisible();
    const chip = page.locator('.home-composer .model-picker');
    if (failing === 'models/roles') await expect(chip).toHaveCount(0);
    else await expect(chip).toHaveAccessibleName('Model: fixture/alternate'); // the saved role's own id, no swap
    await expect(page.getByText('DS Flash')).toHaveCount(0);
  });
}

for (const arrival of ['focus', 'visibilitychange']) {
  test(`Home refreshes the saved role after another window changes it · ${arrival}`, async ({ page, context }) => {
    const sample = fresh(); sample.models = CATALOG;
    let role = ROLE_DEFAULT;
    const rig = await boot(page, garden(), sample);
    await page.route('**/models/roles', route => route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model: role }] } }));
    await railPlace(page, 'Marketing').click();
    const chip = page.locator('.home-composer .model-picker');
    await expect(chip).toHaveText('DS Flash');
    const other = await context.newPage();
    try {
      await boot(other, garden(), sample);
      await other.route('**/models/roles', route => route.fulfill({ json: { default: ROLE_DEFAULT, roles: [{ id: 'conversation', model: role }] } }));
      await other.route('**/models/roles/conversation', async route => {
        role = route.request().postDataJSON().model;
        rig.engine.update({ model: role });
        await route.fulfill({ json: { id: 'conversation', model: role } });
      });
      await railPlace(other, 'Marketing').click();
      await other.locator('.home-composer .model-picker').click();
      await other.getByRole('dialog').getByRole('button', { name: /All models/ }).click();
      await other.getByRole('dialog').getByRole('radio').filter({ hasText: 'Fixture Alternate' }).click();
      await expect.poll(() => role).toBe(ROLE_CHANGED);
      await page.bringToFront();
      await page.evaluate(event => event === 'focus' ? window.dispatchEvent(new Event('focus')) : document.dispatchEvent(new Event('visibilitychange')), arrival);
      await expect(chip).toHaveText('Alternate');
      const composer = page.getByRole('textbox', { name: 'Message', exact: true });
      await composer.fill('Use the updated role'); await composer.press('Enter');
      await expect.poll(() => rig.engine.turnModels).toEqual([ROLE_CHANGED]);
    } finally { await other.close(); }
  });
}

for (const theme of ['light', 'dark']) {
  test(`Home effective model keeps the exact composer geometry · ${theme}`, async ({ page }, info) => {
    const seed = garden(); seed.places![0].model = ROLE_DEFAULT;
    const sample = fresh(); sample.models = CATALOG;
    await boot(page, seed, sample);
    await page.evaluate(theme => { localStorage.setItem('codeaf-theme', theme); window.dispatchEvent(new StorageEvent('storage', { key: 'codeaf-theme', newValue: theme })); }, theme);
    await railPlace(page, 'Marketing').click();
    await expect(page.locator('.home-composer .model-picker')).toHaveText('DS Flash');
    const box = await page.locator('.home-composer .composer').boundingBox();
    expect(box?.width).toBe(616); expect(box?.height).toBe(48);
    const typography = await page.locator('.home-composer textarea').evaluate(field => {
      const font = getComputedStyle(field), placeholder = getComputedStyle(field, '::placeholder');
      return { font: font.fontFamily, placeholder: placeholder.fontFamily, size: font.fontSize, leading: font.lineHeight, sans: font.getPropertyValue('--sans'), alias: font.getPropertyValue('--font-sans') };
    });
    await info.attach('home-composer-typography', { body: JSON.stringify(typography), contentType: 'application/json' });
    if (process.env.CODEAF_UI_RESULTS) {
      await mkdir(`${process.env.CODEAF_UI_RESULTS}/shots`, { recursive: true });
      await writeFile(`${process.env.CODEAF_UI_RESULTS}/shots/home-effective-${theme}-${info.project.name}.json`, JSON.stringify({ ...typography, width: box?.width, height: box?.height }, null, 2));
    }
    await expect(page.locator('.home-composer-note')).toHaveCount(0);
    if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/shots/home-effective-${theme}-${info.project.name}.png` });
  });
}
