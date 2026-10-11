import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type MockPlaces, type PlacesSeed } from './support/mock-places';

// Home composer (Places 8a, 8b, 9b, IX): the prompt names the place, Enter opens a tab right after Home, and
// ⌘↵ leaves focus on Home. The new chat is filed in the place and born in its first folder.

const NEW_CHAT = 'mock-1';
const fresh = (): Scenario => ({ initial: { entries: [], title: '', sessionFile: sessionFileFor(NEW_CHAT) }, turns: [{ entries: [{ Role: 'assistant', Text: 'On it.', Answer: true } as never] }] });

const garden = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true, sources: [{ kind: 'folder', ref: '/work/brand' }, { kind: 'folder', ref: '/work/docs' }] },
    { name: 'Launch site', parents: ['Marketing'] },
  ],
  chats: [{ id: 'sess-launch', title: 'Launch copy', places: ['Marketing'] }],
  live: [NEW_CHAT],
  disk: ['/work/brand', '/work/docs'],
});

type Rig = { engine: MockEngine; places: MockPlaces; mac: boolean };
async function boot(page: Page): Promise<Rig> {
  const engine = await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, garden());
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await expect(page.locator('.app-shell .place-rail').getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  return { engine, places, mac };
}

const tabs = (page: Page) => page.locator('.workspace-tabstrip').getByRole('tab');
// Places are libraries: several Homes can be tabs at once. `home` is the one in front; `homeOf` finds one by its place's name.
const home = (page: Page) => page.locator('.workspace-tab.is-place-home[data-active="true"]');
const homeOf = (page: Page, name: string) => page.locator('.workspace-tab.is-place-home').filter({ hasText: name });
const composer = (page: Page) => page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
const isCreate = (call: { method: string; path: string }) => call.method === 'POST' && call.path === '/sessions';
const isFiling = (placeId: string) => (call: { method: string; path: string }) => call.method === 'POST' && call.path === `/places/${placeId}/members`;
const isTurn = (call: { method: string; path: string }) => call.method === 'POST' && call.path === `/sessions/${NEW_CHAT}/turn`;

for (const theme of ['light', 'dark'] as const) {
  test(`d5-pl-test-home: ${theme} Home composer starts a chat after Home, and ⌘↵ stays on Home`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const rig = await boot(page);
    const marketing = rig.places.id('Marketing');
    const primary = rig.mac ? 'Meta' : 'Control';

    await page.keyboard.press(`${primary}+KeyP`);
    const chooser = page.locator('dialog.goto-chooser[open]');
    await chooser.getByRole('combobox').fill('Launch site');
    await page.keyboard.press('Enter');
    await expect(home(page)).toContainText('Launch site');
    await expect(composer(page)).toHaveAttribute('placeholder', 'Start the first chat in Launch site');

    await page.locator('.app-shell .place-rail').locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: /^Marketing/ }) }).locator('.rail-place').click();
    await expect(home(page)).toContainText('Marketing');
    await expect(composer(page)).toHaveAttribute('placeholder', 'Start something in Marketing');
    await expect(page.locator('.workspace-tab.is-place-home')).toHaveCount(2);

    await composer(page).fill('Draft the launch email');
    await composer(page).press('Enter');
    const opened = tabs(page).filter({ hasText: 'Draft the launch email' });
    await expect(opened).toHaveAttribute('aria-selected', 'true');
    // It opens right after the Marketing Home it was started from, beside the Launch site Home, which stays.
    const labels = await tabs(page).evaluateAll(els => els.map(el => el.getAttribute('aria-label') ?? el.textContent?.trim() ?? ''));
    expect(labels[labels.findIndex(label => /Marketing/.test(label)) + 1]).toBe('Draft the launch email');
    await expect(page.locator('.workspace-tab.is-place-home')).toHaveCount(2);
    await expect(page.locator('.workspace-tab[data-kind="conversation"]')).toHaveCount(2);

    const create = rig.places.traffic.findIndex(isCreate);
    const file = rig.places.traffic.findIndex(isFiling(marketing));
    const turn = rig.places.traffic.findIndex(isTurn);
    expect(create).toBeGreaterThanOrEqual(0);
    expect(rig.places.traffic[create].body).toEqual({ placeId: marketing });
    expect(file).toBeGreaterThan(create);
    expect(turn).toBeGreaterThan(file);
    expect(rig.engine.snapshot().workspace).toBe('/work/brand');
    expect(rig.engine.snapshot().workingFolder).toMatchObject({ from: 'place', path: '/work/brand' });

    await homeOf(page, 'Marketing').getByRole('tab').click();
    await expect(homeOf(page, 'Marketing').getByRole('tab')).toHaveAttribute('aria-selected', 'true');
    await composer(page).fill('Second thought, in the background');
    await composer(page).press(`${primary}+Enter`);
    await expect(tabs(page)).toHaveCount(5);
    await expect.poll(() => rig.engine.calls.filter(call => call.path.endsWith('/turn')).map(call => call.body.text)).toContain('Second thought, in the background');
    await expect(homeOf(page, 'Marketing').getByRole('tab')).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('.workspace-tab[data-kind="conversation"]')).toHaveCount(3);
  });
}
