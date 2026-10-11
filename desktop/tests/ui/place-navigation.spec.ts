import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';

// Owner rule (10 Oct): a click inside a page opens a NEW TAB in this window; only the rail and ⌘P switch the place.
// Switching away saves the exact tab set, and Now restores it (docs/DECISIONS.md).

const conversation = (id: string, extra: object = {}) => ({ id, kind: 'conversation' as const, title: id, titleSource: 'manual' as const, draft: '', pinned: false, ...extra });
const seedTabs = {
  tabs: [
    conversation('Alpha'),
    conversation('Beta', { groupId: 'g1' }),
    conversation('Gamma', { groupId: 'g1' }),
    {
      id: 'Split', kind: 'conversation', title: 'Split', titleSource: 'manual', draft: '', pinned: false,
      split: { layout: '1x2', focus: 0, panes: [conversation('Left'), conversation('Right')] },
    },
  ],
  groups: [{ id: 'g1', title: 'Pair', collapsed: false }], closed: [], activeId: 'Gamma', recentIds: ['Gamma'], nextNumber: 9,
};

const seed = (): PlacesSeed => ({
  places: [{ name: 'Reading', tint: 'iris', pinned: true }, { name: 'Writing', tint: 'rose', pinned: true }],
  chats: [{ id: 'loose', title: 'A loose chat' }],
  live: ['mock-1'],
});

const rail = (page: Page) => page.locator('.app-shell .place-rail');
const strip = (page: Page) => page.locator('.workspace-tabstrip');
const names = (page: Page) => strip(page).getByRole('tab').evaluateAll(els => els.map(el => el.getAttribute('aria-label') ?? el.textContent?.trim() ?? ''));
const selected = (page: Page) => strip(page).locator('[role="tab"][aria-selected="true"]');

async function boot(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.addInitScript(state => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state)); }, seedTabs);
  await installMockEngine(page, { initial: { entries: [], title: '', sessionFile: sessionFileFor('mock-1') }, turns: [] });
  await installMockPlaces(page, seed());
  await page.goto('/');
  await expect(strip(page).getByRole('tab', { name: 'Alpha' })).toBeVisible();
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: a tile opens a tab, the rail switches, Now restores the exact setup, reload keeps it`, async ({ page }) => {
    await boot(page, theme);
    const before = await names(page);
    await expect(strip(page).locator('.workspace-split-tab')).toHaveCount(1);
    await expect(selected(page)).toHaveAccessibleName(/Gamma/);

    // 1. All places opens as a tab; a tile in it opens a NEW tab and leaves the rest alone.
    await rail(page).getByRole('button', { name: /^All places/ }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toBeVisible();
    await page.locator('.places-tile[data-mode="place"] [data-places-tile-focusable]').filter({ hasText: 'Reading' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Reading' })).toBeVisible();
    const after = await names(page);
    expect(after.length).toBe(before.length + 2);
    for (const name of before) expect(after).toContain(name);
    await expect(strip(page).locator('.workspace-split-tab')).toHaveCount(1);
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toHaveAttribute('aria-current', 'page');

    // Clicking the same tile again focuses the open tab instead of duplicating it.
    await strip(page).getByRole('tab', { name: 'All places' }).click();
    await page.locator('.places-tile[data-mode="place"] [data-places-tile-focusable]').filter({ hasText: 'Reading' }).click();
    expect((await names(page)).length).toBe(after.length);

    // 2. The rail switches the whole set.
    const leftOn = await selected(page).getAttribute('aria-label');
    await rail(page).getByRole('button', { name: /^Writing/ }).first().click();
    await expect(strip(page).getByRole('tab', { name: 'Alpha' })).toHaveCount(0);
    await expect(page.getByRole('heading', { level: 1, name: 'Writing' })).toBeVisible();

    // 3. Now brings the exact setup back, including the tab added in it.
    await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
    expect(await names(page)).toEqual(after);
    await expect(selected(page)).toHaveAttribute('aria-label', leftOn!);
    await expect(strip(page).locator('.workspace-split-tab')).toHaveCount(1);
    await expect(strip(page).locator('.workspace-tab-group')).toHaveCount(1);

    // 4. Reload keeps it.
    await page.reload();
    await expect.poll(() => names(page)).toEqual(after);
    await expect(selected(page)).toHaveAttribute('aria-label', leftOn!);
  });

  test(`${theme}: ⌘-click opens behind, ⌥-click on a rail row opens its Home as a tab, ⌘[ returns across a switch`, async ({ page }) => {
    await page.addInitScript(() => { Object.defineProperty(navigator, 'platform', { value: 'MacIntel' }); });
    await boot(page, theme);
    const before = await names(page);
    await rail(page).getByRole('button', { name: /^All places/ }).click();
    const tile = (name: string) => page.locator('.places-tile[data-mode="place"] [data-places-tile-focusable]').filter({ hasText: name });
    await tile('Reading').click({ modifiers: ['Meta'] });
    // Behind: All places stays the selected tab and Reading's Home exists beside it.
    await expect(strip(page).getByRole('tab', { name: 'Reading', exact: true })).toHaveAttribute('aria-selected', 'false');
    await expect(selected(page)).toHaveAccessibleName('All places');
    expect((await names(page)).length).toBe(before.length + 2);

    await rail(page).getByRole('button', { name: /^Writing/ }).first().click({ modifiers: ['Alt'] });
    await expect(selected(page)).toHaveAccessibleName('Writing');
    for (const name of before) expect(await names(page)).toContain(name);
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toHaveAttribute('aria-current', 'page');

    // A plain rail click switches; ⌘[ comes back to the setup it left.
    const now = await names(page);
    await rail(page).getByRole('button', { name: /^Reading/ }).first().click();
    await expect(strip(page).getByRole('tab', { name: 'Alpha' })).toHaveCount(0);
    await page.keyboard.press('Meta+BracketLeft');
    await expect.poll(() => names(page)).toEqual(now);
  });
}
