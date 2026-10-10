import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';
import { openPage } from './support/shell-navigation';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Tab chrome: a cut title's 500ms tooltip (Shell 3l) and the press fill (field-2 for dur-press).
const delay = design.interaction.previewOpenDelay;
const LONG = 'A long conversation title that the strip can only show part of';

async function seed(page: Page, titles: { id: string; title: string; pinned?: boolean }[], activeId: string) {
  const tabs = titles.map(tab => ({ id: tab.id, title: tab.title, draft: '', titleSource: 'manual', kind: 'conversation', pinned: !!tab.pinned }));
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(tab => tab.id) };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const chip = (page: Page, title: string) => page.locator('.workspace-tab', { has: page.getByRole('tab', { name: title, exact: true }) });

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('a cut title shows after the preview delay, and a title that fits does not', async ({ page }) => {
  expect(delay).toBe(500);
  expect(design.interaction.tooltipOpenDelay).toBe(delay);
  await seed(page, [{ id: 'a', title: LONG }, { id: 'b', title: 'Notes' }], 'a');
  await page.goto('/');
  const cut = chip(page, LONG);
  const fit = chip(page, 'Notes');
  await expect(cut).toHaveAttribute('data-title-overflow', 'true');
  await expect(fit).not.toHaveAttribute('data-title-overflow');
  await cut.getByRole('tab').hover();
  await page.waitForTimeout(delay * 0.4);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  await expect(page.getByRole('tooltip')).toHaveText(LONG);
  await page.mouse.move(0, 0);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  // Notes is the inactive tab: its preview carries the name, so the tooltip stays shut.
  await fit.getByRole('tab').hover();
  await page.waitForTimeout(delay * 1.2);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  await page.mouse.move(0, 0);
  // Make the short name the open tab. It still fits, so it still has no tooltip.
  await fit.getByRole('tab').click();
  await page.mouse.move(0, 0);
  await expect(fit).toHaveAttribute('data-active', 'true');
  await expect(fit).not.toHaveAttribute('data-title-overflow');
  await fit.getByRole('tab').hover();
  await page.waitForTimeout(delay * 1.2);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
});

for (const scheme of ['light', 'dark'] as const) {
  test(`holding a tab fills it with field-2 for the press duration (${scheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await seed(page, [{ id: 'a', title: 'Notes' }, { id: 'b', title: 'Other' }], 'a');
    await page.goto('/');
    const active = chip(page, 'Notes');
    const box = (await active.boundingBox())!;
    await page.mouse.move(box.x + 16, box.y + box.height / 2);
    await page.mouse.down();
    await expect(active).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
    await expect(active).toHaveCSS('color', await tokenColor(page, 'ink'));
    await expect(active.locator('.workspace-tab-select')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    // Both transitions (fill and colour) take dur-press. Equal values serialize as one duration.
    const duration = await active.evaluate(element => getComputedStyle(element).transitionDuration);
    expect(duration.split(',').map(part => part.trim()).every(part => part === '0.08s')).toBe(true);
    await page.mouse.up();
    await expect(active).toHaveCSS('background-color', await tokenColor(page, 'canvas'));
    // The close mark's own press does not fill the chip.
    const close = active.locator('.workspace-tab-close');
    const closeBox = (await close.boundingBox())!;
    await page.mouse.move(closeBox.x + closeBox.width / 2, closeBox.y + closeBox.height / 2);
    await page.mouse.down();
    await expect(active).toHaveCSS('background-color', await tokenColor(page, 'canvas'));
    await page.mouse.up();
  });
}

test('a compressed tab shows its full title in the tooltip and opens no preview', async ({ page }) => {
  await page.goto('/');
  await openPage(page, 'Design system');
  const compressed = page.locator('.tabs-specimen-item', { hasText: 'Compressed' }).locator('.workspace-tab[data-compressed]');
  await expect(compressed).toHaveAttribute('data-title-overflow', 'true');
  await expect(compressed.locator('.workspace-tab-title')).toHaveCount(0);
  await compressed.locator('.workspace-tab-select').hover();
  await expect(page.getByRole('tooltip')).toHaveText('nightly-bench');
  await expect(page.locator('.tab-preview')).toHaveCount(0);
});
