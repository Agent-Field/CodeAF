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
  await expect(rail(page).getByRole('button', { name: 'All places' })).toBeVisible();
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

test('b. Go to via the chooser lands on the place Home; Now gives back its own tabs and the root palette', async ({ page }) => {
  const rig = await boot(page);
  // A draft typed in Now must survive the trip.
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await expect(composer).toBeVisible();
  await composer.fill('a draft kept in Now');
  await expect(page.locator('body')).not.toHaveAttribute('data-tint', /./); // Now keeps the root palette (DESIGN-QUESTIONS PS12).

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
  await expect.poll(() => rig.places.posts(`/places/${id}/visit`).length).toBe(1);
  await expect(section(page, 'Open').locator('.rail-place', { hasText: 'Config parser' })).toHaveAttribute('aria-current', 'page');

  await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
  await expect(page.locator('body')).not.toHaveAttribute('data-tint', /./); // Now keeps the root palette (DESIGN-QUESTIONS PS12).
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
  await expect(page.locator('body')).not.toHaveAttribute('data-tint', /./); // Now keeps the root palette (DESIGN-QUESTIONS PS12).
  await expect(homeTab(page)).toHaveCount(0);
  await expect(railRow(page, 'Docs')).toContainText('closed · still running');

  // A needs-you roll-up draws the amber dot with words a screen reader can say.
  rig.places.setStatus(software, { needsYou: 1 });
  const dot = railRow(page, 'Software').locator('.rail-dot');
  await expect(dot).toHaveAttribute('data-state', 'waiting');
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
  await expect.poll(() => rig.places.posts(`/places/${parser}/parents`).map(call => call.body)).toEqual([{ add: marketing }]);
  await expect(toast(page).last()).toContainText('Added “Config parser” to “Marketing”');

  await openAllPlaces(page, rig);
  const loose = page.locator('.places-chat-row', { hasText: 'Pricing for teams' });
  await loose.locator('.places-chat-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Add to a place…' }).click();
  await page.getByRole('dialog', { name: 'Add “Pricing for teams” to a place…' }).getByRole('combobox').fill('Software');
  await page.keyboard.press('Enter');
  await expect.poll(() => rig.places.posts(`/places/${software}/members`).map(call => call.body)).toEqual([{ chats: ['sess-loose'] }]);
  await expect(page.locator('.places-chat-row', { hasText: 'Pricing for teams' })).toHaveCount(0);
});

test('g. Delete place… says what will happen first, then offers Undo', async ({ page }) => {
  const rig = await boot(page);
  const software = rig.places.id('Software');
  await openAllPlaces(page, rig);
  await tile(page, 'Software').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete place…' }).click();
  const confirm = page.getByRole('group', { name: 'Delete Software' });
  await expect(confirm).toContainText('Delete “Software”? 1 place inside it moves up a level. No chat is deleted.');
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
  await expect.poll(() => rig.places.posts(`/places/${studio}/sources`).map(call => call.body)).toEqual([{ kind: 'folder', ref: '/work/brand' }]);
  await expect(sheet.getByRole('list', { name: 'Files and links' })).toContainText('brand');
  // The same folder again is refused, in the engine's words, and the sheet stays.
  await field.fill('/work/brand');
  await sheet.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(sheet.getByRole('alert')).toHaveText('That is already in this place.');
  await sheet.getByRole('button', { name: 'Done' }).click();
  await expect(sheet).toHaveCount(0);

  await page.getByRole('button', { name: 'Write instructions' }).click();
  const notes = page.getByRole('dialog', { name: 'Instructions for “Studio”' });
  await notes.getByRole('textbox', { name: 'Instructions for Studio' }).fill('Use the brand voice.');
  await notes.getByRole('button', { name: 'Save' }).click();
  await expect(notes).toHaveCount(0);
  await expect.poll(() => rig.places.posts(`/places/${studio}`).map(call => call.body)).toEqual([{ instructions: 'Use the brand voice.' }]);
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

  await page.keyboard.press(`${primary(rig)}+KeyT`);
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
  places.fail('graph', undefined);
  await rail(page).getByRole('button', { name: 'Retry' }).click();
  await expect(railPlace(page, 'Marketing')).toBeVisible();
  await expect(rail(page).getByText('Can’t reach the engine')).toHaveCount(0);
});

test('inbox: the rail’s Inbox lists the waiting question and opens its conversation', async ({ page }) => {
  await boot(page, {
    places: [{ name: 'Software', tint: 'iris' }],
    chats: [{ id: 'sess-need', title: 'Port fix to v1', places: ['Software'], needsYou: true, reason: 'Allow the v1 branch push?' }],
    live: [NEW_CHAT],
  });
  await rail(page).getByRole('button', { name: /^Inbox/ }).click();
  const row = page.locator('.inbox-pane').getByRole('button', { name: /Port fix to v1/ });
  await expect(row).toBeVisible();
  await expect(page.locator('.inbox-pane')).toContainText('Allow the v1 branch push?');
  await row.click();
  await expect(tabs(page).filter({ hasText: 'Port fix to v1' })).toHaveAttribute('aria-selected', 'true');
});

async function setTheme(page: Page, theme: 'Light' | 'Dark') {
  await page.getByRole('combobox', { name: 'Theme' }).click();
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
