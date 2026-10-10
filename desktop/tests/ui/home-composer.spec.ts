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
const home = (page: Page) => page.locator('.workspace-tab.is-place-home');
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
    await expect(home(page)).toHaveCount(1);

    await composer(page).fill('Draft the launch email');
    await composer(page).press('Enter');
    const opened = tabs(page).filter({ hasText: 'Draft the launch email' });
    await expect(opened).toHaveAttribute('aria-selected', 'true');
    await expect(tabs(page).nth(1)).toHaveAccessibleName('Draft the launch email');
    await expect(home(page)).toHaveCount(1);
    await expect(page.locator('.workspace-tab[data-kind="conversation"]')).toHaveCount(1);

    const create = rig.places.traffic.findIndex(isCreate);
    const file = rig.places.traffic.findIndex(isFiling(marketing));
    const turn = rig.places.traffic.findIndex(isTurn);
    expect(create).toBeGreaterThanOrEqual(0);
    expect(rig.places.traffic[create].body).toEqual({ placeId: marketing });
    expect(file).toBeGreaterThan(create);
    expect(turn).toBeGreaterThan(file);
    expect(rig.engine.snapshot().workspace).toBe('/work/brand');
    expect(rig.engine.snapshot().workingFolder).toMatchObject({ from: 'place', path: '/work/brand' });

    await tabs(page).first().click();
    await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
    await composer(page).fill('Second thought, in the background');
    await composer(page).press(`${primary}+Enter`);
    await expect(tabs(page)).toHaveCount(3);
    await expect.poll(() => rig.engine.calls.filter(call => call.path.endsWith('/turn')).map(call => call.body.text)).toContain('Second thought, in the background');
    await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
    await expect(home(page)).toHaveCount(1);
    await expect(page.locator('.workspace-tab[data-kind="conversation"]')).toHaveCount(2);
  });
}
