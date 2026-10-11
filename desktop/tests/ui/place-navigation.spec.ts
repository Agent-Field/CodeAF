import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';

// OWNER DECISION 2026-10-11 (docs/DECISIONS.md "Places are libraries"): a rail click opens the place's Home as a tab in the
// strip that is already showing; Focus filters the strip and tints the frame; nothing is ever replaced, closed or moved.

const READING = 'pl_0000000000000a01';
const WRITING = 'pl_0000000000000a02';
const conversation = (id: string, memberOf?: string[]) => ({ id, kind: 'conversation' as const, title: id, titleSource: 'manual' as const, draft: '', pinned: false, ...(memberOf ? { memberOf } : {}) });
const seedTabs = {
  tabs: [conversation('Alpha', [READING]), conversation('Beta', [WRITING]), conversation('Gamma', [READING, WRITING]), conversation('Delta')],
  groups: [], closed: [], activeId: 'Delta', recentIds: ['Delta'], nextNumber: 9,
};

const seed = (): PlacesSeed => ({
  places: [{ id: READING, name: 'Reading', tint: 'iris', pinned: true }, { id: WRITING, name: 'Writing', tint: 'rose', pinned: true }],
  chats: [{ id: 'read-chat', title: 'Notes on Borges', places: ['Reading'] }],
});

const rail = (page: Page) => page.locator('.app-shell .place-rail');
const strip = (page: Page) => page.locator('.workspace-tabstrip');
const names = (page: Page) => strip(page).getByRole('tab').evaluateAll(els => els.map(el => el.getAttribute('aria-label') ?? el.textContent?.trim() ?? ''));
const selected = (page: Page) => strip(page).locator('[role="tab"][aria-selected="true"]');
const row = (page: Page, name: string) => rail(page).locator('.rail-row', { has: page.getByRole('button', { name: new RegExp(`^${name}`) }) }).first();
const open = (page: Page, name: string) => row(page, name).getByRole('button', { name: new RegExp(`^${name}`) }).first();
const focusButton = (page: Page, name: string) => row(page, name).getByRole('button', { name: new RegExp(`(Focus|focusing) on ${name}$`) });
const now = (page: Page) => rail(page).getByRole('button', { name: 'Now', exact: true });

async function boot(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.addInitScript(state => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state)); }, seedTabs);
  await installMockEngine(page, { initial: { entries: [], title: '', sessionFile: sessionFileFor('mock-1') }, turns: [] });
  await installMockPlaces(page, seed());
  await page.goto('/');
  await expect(strip(page).getByRole('tab', { name: 'Alpha' })).toBeVisible();
}

/** The shared pointer drag: press on the source, pass the threshold, move over the target, release. */
async function dragTo(page: Page, source: Locator, target: Locator) {
  const from = (await source.boundingBox())!;
  const to = (await target.boundingBox())!;
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2 + 12, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.move(to.x + to.width / 2 + 1, to.y + to.height / 2, { steps: 2 });
  await page.mouse.up();
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: (1)(5) a rail click opens the Home as a tab or focuses the open one, and Now shows every tab`, async ({ page }) => {
    await boot(page, theme);
    const before = await names(page);
    await open(page, 'Reading').click();
    await expect(page.getByRole('heading', { level: 1, name: 'Reading' })).toBeVisible();
    await expect(selected(page)).toHaveAccessibleName(/Reading/);
    const afterOne = await names(page);
    expect(afterOne.length).toBe(before.length + 1);
    for (const name of before) expect(afterOne).toContain(name);

    await open(page, 'Writing').click();
    expect((await names(page)).length).toBe(before.length + 2);
    // Clicking a place that is already open focuses its tab; it never opens a second one.
    await open(page, 'Reading').click();
    expect((await names(page)).length).toBe(before.length + 2);
    await expect(selected(page)).toHaveAccessibleName(/Reading/);

    // Now shows all open tabs, unfiltered (and ends a Focus that was on).
    await focusButton(page, 'Reading').click({ force: true });
    await expect(strip(page).getByRole('tab', { name: 'Delta' })).toHaveCount(0);
    await now(page).click();
    expect((await names(page)).length).toBe(before.length + 2);
    await expect(page.locator('.focus-chip')).toHaveCount(0);

    // Reload keeps the same tab set.
    await page.reload();
    await expect.poll(async () => (await names(page)).length).toBe(before.length + 2);
  });

  test(`${theme}: (6) the rail highlights the place of the active tab`, async ({ page }) => {
    await boot(page, theme);
    await expect(now(page)).toHaveAttribute('aria-current', 'page');
    await strip(page).getByRole('tab', { name: 'Alpha' }).click();
    await expect(open(page, 'Reading')).toHaveAttribute('aria-current', 'page');
    await strip(page).getByRole('tab', { name: 'Beta' }).click();
    await expect(open(page, 'Writing')).toHaveAttribute('aria-current', 'page');
    await expect(open(page, 'Reading')).not.toHaveAttribute('aria-current', 'page');
    await strip(page).getByRole('tab', { name: 'Delta' }).click();
    await expect(now(page)).toHaveAttribute('aria-current', 'page');
  });

  test(`${theme}: (2) Focus filters the strip, tints the frame, closes nothing, and every exit clears it`, async ({ page }) => {
    await page.addInitScript(() => { Object.defineProperty(navigator, 'platform', { value: 'MacIntel' }); });
    await boot(page, theme);
    const shell = page.locator('.app-shell');
    const before = await names(page);
    await expect(shell).toHaveAttribute('data-tint', 'graphite');

    // The hover button filters to the place's tabs and tints the frame with the place's tint.
    await row(page, 'Reading').hover();
    await focusButton(page, 'Reading').click();
    await expect(page.locator('.focus-chip')).toContainText('Reading');
    await expect(shell).toHaveAttribute('data-tint', 'iris');
    await expect.poll(() => names(page)).toEqual(['Alpha', 'Gamma']);
    await expect(selected(page)).toHaveAccessibleName(/Alpha|Gamma/);

    // New tabs opened while focused belong to the place, so they stay in the filtered strip.
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    await expect(strip(page).getByRole('tab')).toHaveCount(3);

    // The chip clears the filter and every tab is back, in the order it had.
    await page.locator('.focus-chip').click();
    await expect(page.locator('.focus-chip')).toHaveCount(0);
    await expect(shell).toHaveAttribute('data-tint', 'graphite');
    expect(await names(page)).toEqual([...before, expect.any(String)]);

    // ⌘-click on the row filters; Focus again, and Now, clear it.
    await open(page, 'Writing').click({ modifiers: ['Meta'] });
    await expect(shell).toHaveAttribute('data-tint', 'rose');
    await expect.poll(() => names(page)).toEqual(['Beta', 'Gamma']);
    await row(page, 'Writing').hover();
    await focusButton(page, 'Writing').click();
    await expect(page.locator('.focus-chip')).toHaveCount(0);
    await open(page, 'Writing').click({ modifiers: ['Meta'] });
    await now(page).click();
    await expect(page.locator('.focus-chip')).toHaveCount(0);
    expect((await names(page)).length).toBe(before.length + 1);
  });

  test(`${theme}: (3) a chat opened from a place Home is a new tab beside the current one and keeps its place`, async ({ page }) => {
    await boot(page, theme);
    await open(page, 'Reading').click();
    await expect(page.getByRole('heading', { level: 1, name: 'Reading' })).toBeVisible();
    const before = await names(page);
    await page.getByRole('region', { name: 'Chats', exact: true }).locator('[data-chat-id="read-chat"] button').click();
    const after = await names(page);
    expect(after.length).toBe(before.length + 1);
    const at = before.findIndex(name => /Reading/.test(name));
    expect(after[at + 1]).toMatch(/Notes on Borges/);
    await expect(selected(page)).toHaveAccessibleName(/Notes on Borges/);
    await expect(open(page, 'Reading')).toHaveAttribute('aria-current', 'page');
    // It belongs to Reading: a Focus on Reading keeps it, a Focus on Writing does not.
    await row(page, 'Reading').hover();
    await focusButton(page, 'Reading').click();
    await expect(strip(page).getByRole('tab', { name: /Notes on Borges/ })).toBeVisible();
    await page.locator('.focus-chip').click();
    await row(page, 'Writing').hover();
    await focusButton(page, 'Writing').click();
    await expect(strip(page).getByRole('tab', { name: /Notes on Borges/ })).toHaveCount(0);
  });

  test(`${theme}: (4) dragging a tab onto a rail place adds membership and moves nothing`, async ({ page }) => {
    await boot(page, theme);
    const before = await names(page);
    await dragTo(page, strip(page).getByRole('tab', { name: 'Alpha' }), row(page, 'Writing'));
    // Nothing moved or closed.
    expect(await names(page)).toEqual(before);
    // Alpha now belongs to Writing as well as Reading (multi-membership).
    await row(page, 'Writing').hover();
    await focusButton(page, 'Writing').click();
    await expect.poll(() => names(page)).toEqual(['Alpha', 'Beta', 'Gamma']);
    await page.locator('.focus-chip').click();
    await row(page, 'Reading').hover();
    await focusButton(page, 'Reading').click();
    await expect.poll(() => names(page)).toEqual(['Alpha', 'Gamma']);
  });
}
