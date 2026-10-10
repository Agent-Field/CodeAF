import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { installPlacesEngine, type PlacesEngine } from './support/places-engine';
import { sessionFileFor, type PlacesSeed } from './support/mock-places';
import type { UsingView } from '../../src/features/places/using-client';

// The Using chip, its popover, the membership note and ⌘T in a place (Places 6d–6f; PL-005, 111, 113, 118, 121, 122, 262).
// Every journey drives real clicks and keys against the stateful Places mock; the chat's Using answer is the mock's own
// resolver reading the same graph the rail shows, so a filing in one is visible in the other.

const CHAT = 'mock-1';
const WIDTHS = [320, 600, 850, 1200];

/**
 * Release inherits Software and Marketing, and Software inherits codeaf: four places, two sources (the codeaf folder, the
 * brand-voice file). Ids are left to the mock: the app refuses a place id that is not pl_ and sixteen hex digits.
 */
function seed(chat: { places?: string[] } = { places: ['Release'] }): PlacesSeed {
  return {
    places: [
      { name: 'codeaf', tint: 'tide', pinned: true, instructions: 'Keep changes focused.', sources: [{ kind: 'folder', ref: '/work/codeaf' }] },
      { name: 'Software', parents: ['codeaf'], lastOpenedAt: 'now' },
      { name: 'Marketing', parents: ['codeaf'], tint: 'rose', lastOpenedAt: 'now', sources: [{ kind: 'file', ref: '/work/brand-voice.md' }] },
      { name: 'Release', parents: ['Software', 'Marketing'], lastOpenedAt: 'now' },
    ],
    chats: [{ id: CHAT, title: 'Release notes', ...chat }],
    now: '2026-10-10T12:00:00.000Z',
    live: [CHAT],
    disk: ['/work/codeaf', '/work/brand-voice.md'],
  };
}

type Rig = { engine: MockEngine; places: PlacesEngine; mac: boolean };
async function boot(page: Page, theme: string, fixture: PlacesSeed = seed()): Promise<Rig> {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ colorScheme: theme as 'light' | 'dark' });
  const engine = await installMockEngine(page, { initial: { entries: [], title: 'Release notes', sessionFile: sessionFileFor(CHAT) }, turns: [{ entries: [{ Role: 'assistant', Text: 'On it.', Answer: true } as never] }] });
  const places = await installPlacesEngine(page, fixture);
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  return { engine, places, mac };
}

const rail = (page: Page) => page.locator('.app-shell .place-rail').first();
const railRow = (page: Page, name: string) => rail(page).locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }) });
const railPlace = (page: Page, name: string) => railRow(page, name).locator('.rail-place');
const chip = (page: Page) => page.locator('.using-chip');
const sheet = (page: Page) => page.getByRole('dialog', { name: 'What this conversation is using' });
const primary = (rig: Rig) => (rig.mac ? 'Meta' : 'Control');

/** Opens the seeded chat from its place's Home, the way a person does. */
async function openChat(page: Page, placeName = 'Release') {
  await railPlace(page, placeName).click();
  await page.locator('.places-chat-row').first().locator('button').click();
}

for (const theme of ['light', 'dark'] as const) {
  test(`PL-111: the closed chip names the places and sources, 24px tall, and opens by keyboard · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme);
    await openChat(page);
    const button = page.getByRole('button', { name: 'Using 4 places · 2 sources', exact: true });
    await expect(button).toBeVisible();
    await expect(button).toHaveAttribute('aria-expanded', 'false');
    expect((await button.boundingBox())!.height).toBe(24);
    // Colour is only for the 6px glyphs: the closed chip text is not a state colour.
    await button.focus();
    await page.keyboard.press('Enter');
    await expect(button).toHaveAttribute('aria-expanded', 'true');
    await expect(sheet(page)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(sheet(page)).toHaveCount(0);
    await expect(button).toBeFocused();
    await expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(rig.places.calls.some(call => /\/using$/.test(call.path))).toBe(true);
  });

  test(`PL-113: a chat with no place and no sources draws no chip · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme);
    await openChat(page);
    await expect(chip(page)).toHaveCount(1);
    // Taking the chat out of its only place leaves nothing to use (Software and Marketing were inherited, not its own).
    const release = rig.places.id('Release');
    const status = await page.evaluate(async id => (await fetch(`/api/engine/places/${id}/members/remove`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ chats: ['mock-1'] }) })).status, release);
    expect(status).toBe(200);
    expect(rig.places.state().members.filter(m => m.chatId === CHAT)).toHaveLength(0);
    // The conversation reads its Using list again when it is reopened.
    await page.reload();
    await expect(page.getByRole('tab', { name: /Release notes/ })).toBeVisible();
    await expect(chip(page)).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^Using / })).toHaveCount(0);
  });

  test(`PL-118: the Policy row names the conflict outcome · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme);
    const view: UsingView = rig.places.using(CHAT);
    view.bundle.policy = [{ field: 'model', value: 'Pro', outcome: 'decided', decidedBy: rig.places.id('codeaf'), wanted: [{ placeId: rig.places.id('Release'), value: 'Flash' }, { placeId: rig.places.id('codeaf'), value: 'Pro' }] }];
    view.settings = [{ field: 'model', state: 'pending', value: 'Pro', decidedBy: 'codeaf', reason: 'Decided by codeaf.' } as UsingView['settings'][number]];
    rig.places.setUsing(CHAT, view);
    await openChat(page);
    await chip(page).click();
    const policy = sheet(page).getByRole('region', { name: 'Policy' });
    await expect(policy).toContainText('Release wanted Flash · codeaf decided');
  });

  test(`PL-005: with no common ancestor the chat asks once, by keyboard, and remembers the pick · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme);
    const view: UsingView = rig.places.using(CHAT);
    const [software, marketing] = [rig.places.id('Software'), rig.places.id('Marketing')];
    view.bundle.policy = [{ field: 'model', value: '', outcome: 'needsPick', wanted: [{ placeId: software, value: 'Flash' }, { placeId: marketing, value: 'Pro' }] }];
    view.settings = [{ field: 'model', state: 'needsPick', reason: 'Two places want different models.' }];
    rig.places.setUsing(CHAT, view);
    await openChat(page);
    // The amber mark carries the screen-reader words for a choice that waits on the person.
    await expect(chip(page).getByLabel('Needs your choice')).toHaveCount(1);
    await chip(page).focus();
    await page.keyboard.press('Enter');
    const ask = sheet(page).getByRole('group', { name: 'Which place decides the model' });
    await expect(ask).toContainText('Asked once; the answer is remembered for this conversation.');
    await ask.getByRole('button', { name: /^Marketing/ }).focus();
    await page.keyboard.press('Enter');
    await expect(ask.getByRole('button', { name: /^Marketing/ })).toHaveAttribute('aria-pressed', 'true');
    expect(rig.places.calls.filter(call => call.path.endsWith('/using/choice'))).toHaveLength(1);
    expect(rig.places.calls.find(call => call.path.endsWith('/using/choice'))!.body).toMatchObject({ field: 'model', placeId: marketing });
  });

  test(`PL-121: files dropped on the popover attach to this chat only · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme);
    await openChat(page);
    await chip(page).click();
    const target = sheet(page);
    await expect(target).toBeVisible();
    const before = rig.places.calls.filter(call => call.method === 'POST').length;
    const data = await page.evaluateHandle(() => {
      const transfer = new DataTransfer();
      transfer.items.add(new File(['hello'], 'notes.txt', { type: 'text/plain' }));
      return transfer;
    });
    await target.dispatchEvent('dragover', { dataTransfer: data });
    await expect(target).toHaveAttribute('data-drop', 'true');
    await target.dispatchEvent('drop', { dataTransfer: data });
    await expect(target).not.toHaveAttribute('data-drop', 'true');
    // The file waits in this chat's tray; the place gained no source and no member.
    await expect(page.getByText('notes.txt').first()).toBeVisible();
    expect(rig.places.calls.filter(call => call.method === 'POST').length).toBe(before);
    expect(rig.places.state().places.find(p => p.id === rig.places.id('Release'))!.sources).toHaveLength(0);
  });

  test(`PL-122: the membership note reads "Now also using Release: brand-voice.md · Undo" and Undo reverts · ${theme}`, async ({ page }) => {
    const rig = await boot(page, theme, seed({ places: ['Software'] }));
    await openChat(page, 'Software');
    // A real write gives a real receipt: filing the chat into Release is what the note reports.
    const release = rig.places.id('Release');
    const receipt = await page.evaluate(async id => {
      const response = await fetch(`/api/engine/places/${id}/members`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ chats: ['mock-1'] }) });
      return (await response.json()).undo as string[];
    }, release);
    expect(rig.places.state().members.some(m => m.chatId === CHAT && m.placeId === release)).toBe(true);
    rig.engine.update({ entries: [{ Role: 'aside', AsideKind: 'places', Text: 'Now also using Release: brand-voice.md', UndoReceipts: receipt } as never] });
    const note = page.locator('.membership-note');
    await expect(note).toHaveText('Now also using Release: brand-voice.md · Undo', { timeout: 10000 });
    await expect(note).toHaveAttribute('role', 'note');
    await note.getByRole('button', { name: 'Undo', exact: true }).focus();
    await expect(note.getByRole('button', { name: 'Undo', exact: true })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => rig.places.state().members.some(m => m.chatId === CHAT && m.placeId === release)).toBe(false);
    await expect(note.getByRole('button', { name: 'Undo', exact: true })).toHaveCount(0);
  });

  test(`PL-262: ⌘T in a place starts a chat that belongs to it and uses its context · ${theme}`, async ({ page }) => {
    const fixture: PlacesSeed = seed();
    fixture.chats = [];
    const rig = await boot(page, theme, fixture);
    const id = rig.places.id('Marketing');
    await railPlace(page, 'Marketing').click();
    await page.keyboard.press(`${primary(rig)}+KeyT`);
    const field = page.getByRole('combobox', { name: 'Search or start' });
    await expect(field).toBeFocused();
    await field.fill('Plan the webinar');
    await field.press('Enter');
    await expect.poll(() => rig.places.traffic.some(call => call.method === 'POST' && call.path === `/places/${id}/members`)).toBe(true);
    const create = rig.places.traffic.findIndex(call => call.method === 'POST' && call.path === '/sessions');
    expect(rig.places.traffic[create].body).toEqual({ place: id });
    expect(rig.places.state().members).toContainEqual(expect.objectContaining({ chatId: CHAT, placeId: id }));
    // The new chat uses Marketing's context: its chip names the place and the brand-voice file.
    await expect(page.getByRole('button', { name: 'Using 2 places · 2 sources', exact: true })).toBeVisible({ timeout: 10000 });
  });
}

for (const theme of ['light', 'dark'] as const) {
  test(`the chip rings on keyboard focus and the sheet stays inside the window at every width · ${theme}`, async ({ page }) => {
    await boot(page, theme);
    await openChat(page);
    const button = chip(page);
    // Keyboard focus draws the one shared ring: 2px of accent, then a 4px halo (so 6px out in all).
    await button.focus();
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Tab');
    await expect(button).toBeFocused();
    const ring = await button.evaluate(node => getComputedStyle(node).boxShadow);
    expect(ring).toContain('0px 0px 0px 2px');
    expect(ring).toContain('0px 0px 0px 6px');
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await expect(button).toBeVisible();
      if (await sheet(page).count() === 0) await button.click();
      await expect(sheet(page)).toBeVisible();
      const box = (await sheet(page).boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
      await page.keyboard.press('Escape');
      await expect(sheet(page)).toHaveCount(0);
    }
  });
}
