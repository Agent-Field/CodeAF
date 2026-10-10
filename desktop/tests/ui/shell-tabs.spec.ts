import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };

// The shell design (2h, 3j): geometry measured from the design files and pinned here from tokens.
const f = design.foundation;
const px = (name: string) => parseFloat(f[name as keyof typeof f] as string);
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

const pane = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', kind: 'conversation', ...over });
async function seedSplit(page: Page) {
  const state = {
    tabs: [
      { id: 'solo', title: 'Solo', draft: '', pinned: false },
      { id: 'sp', title: 'Config · Fixtures', draft: '', pinned: false, kind: 'conversation', split: { layout: '1x2', focus: 0, panes: [pane('p1', 'Config', { draft: 'left draft' }), pane('p2', 'Fixtures', { kind: 'task', draft: 'right draft' })] } },
    ],
    groups: [], closed: [], activeId: 'sp', nextNumber: 3, recentIds: ['sp', 'solo'],
  };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

test('the strip, a tab and the content card match the shell design', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  const bar = page.locator('.workspace-tabbar');
  expect((await bar.boundingBox())!.height).toBe(px('tab-strip-height'));
  expect(px('tab-strip-height')).toBe(46);
  const active = page.locator('.workspace-tab[data-active="true"]');
  const rest = page.locator('.workspace-tab[data-active="false"]');
  await expect(active).toHaveCSS('height', '30px');
  await expect(active).toHaveCSS('border-top-left-radius', '8px');
  await expect(active).toHaveCSS('box-shadow', /.+/);
  await expect(rest).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(rest).toHaveCSS('box-shadow', 'none');
  expect((await rest.boundingBox())!.width).toBeLessThanOrEqual(190);
  // Equal-width tabs, 12px title, 13px glyph, no ellipsis: the title fades under a mask.
  const title = rest.locator('.workspace-tab-title');
  await expect(title).toHaveCSS('font-size', '12px');
  await expect(title).not.toHaveCSS('text-overflow', 'ellipsis');
  await expect(title).not.toHaveCSS('mask-image', 'none');
  expect((await rest.locator('.workspace-tab-select .app-icon').boundingBox())!.width).toBe(13);
  const card = page.locator('.workspace-pane');
  await expect(card).toHaveCSS('border-top-left-radius', '10px');
  await expect(card).toHaveCSS('box-shadow', /.+/);
  const rail = await page.locator('.sidebar').boundingBox();
  expect(rail!.width).toBe(px('sidebar-width'));
  expect(px('sidebar-width')).toBe(252);
  // The close sits in a fixed 20px slot and never changes the tab width.
  const slot = await active.locator('.workspace-tab-close-slot').boundingBox();
  expect(slot!.width).toBe(20);
  expect((await active.getByRole('button', { name: /^Close / }).boundingBox())!.width).toBe(18);
});

test('tabs compress from 190px to 112px, then the strip scrolls under a mask with a +N menu', async ({ page }) => {
  await page.goto('/');
  const widths: number[] = [];
  for (let index = 0; index < 14; index++) {
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    widths.push((await page.locator('.workspace-tab').first().boundingBox())!.width);
  }
  expect(widths[0]).toBe(190);
  expect(Math.min(...widths)).toBeGreaterThanOrEqual(112);
  expect(widths[widths.length - 1]).toBe(112);
  const strip = page.locator('.workspace-tabstrip');
  await expect(strip).toHaveAttribute('data-fade-end', 'true');
  await expect(strip).not.toHaveCSS('mask-image', 'none');
  await expect(page.getByRole('button', { name: 'Tab actions', exact: true })).toContainText('+');
});

test('double-clicking a tab renames it', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('tab').first().dblclick();
  const dialog = page.getByRole('dialog', { name: 'Rename tab', exact: true });
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill('Renamed');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Renamed', exact: true })).toBeVisible();
});

test('a split is one merged tab with a segment per pane, a 40px pane title and a focus ring', async ({ page }) => {
  await seedSplit(page);
  await page.goto('/');
  // One merged tab beside the plain one: two role=tab segments inside a single split element.
  await expect(page.locator('.workspace-split-tab')).toHaveCount(1);
  const segments = page.locator('.workspace-split-segment');
  await expect(segments).toHaveCount(2);
  await expect(segments.first()).toHaveAttribute('data-focused', 'true');
  await expect(segments.first()).toHaveCSS('height', '24px');
  await expect(page.locator('.workspace-split-tab')).toHaveCSS('height', '30px');
  const panes = page.locator('.workspace-pane');
  await expect(panes).toHaveCount(2);
  expect((await panes.first().locator('.pane-header').boundingBox())!.height).toBe(40);
  await expect(panes.first()).toHaveAttribute('data-focused', 'true');
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'false');
  await expect(page.getByRole('tabpanel')).toHaveAttribute('data-layout', '1x2');
  // Clicking the other segment, or the other pane, moves focus.
  await segments.nth(1).click();
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'true');
  await panes.first().locator('.pane-header').click();
  await expect(panes.first()).toHaveAttribute('data-focused', 'true');
  // A single tab has no pane title line.
  await page.getByRole('tab', { name: 'Solo', exact: true }).click();
  await expect(page.locator('.pane-header')).toHaveCount(0);
  await expectAccessible(page);
});

test('the Design system page shows every tab kind and state, light and dark', async ({ page }) => {
  // Two accessibility passes over the whole design system. Webkit under the suite's
  // four workers does not finish that inside the default 30s, and the checks themselves do not change.
  test.setTimeout(60_000);
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await openPage(page, 'Design system');
    const sheet = page.locator('[data-tab-specimen]');
    await sheet.scrollIntoViewIfNeeded();
    // 8 kinds x 5 columns minus the four n/a cells.
    await expect(sheet.locator('.tabs-specimen-cell .workspace-tab')).toHaveCount(8 * 5 - 4);
    await expect(sheet.locator('.tabs-specimen-na')).toHaveCount(4);
    const waiting = sheet.locator('.tab-dot[data-state="waiting"]').first();
    const failed = sheet.locator('.tab-dot[data-state="failed"]').first();
    await expect(waiting).toHaveCSS('width', '6px');
    expect(await waiting.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(await failed.evaluate(el => getComputedStyle(el).backgroundColor));
    await expect(sheet.locator('.workspace-tab-group').first()).toHaveCSS('border-top-left-radius', '10px');
    await expect(sheet.locator('.workspace-tab-group[data-collapsed="true"] .workspace-group-count')).toHaveText('3');
    await expect(sheet.locator('.workspace-tab.is-pinned').first()).toHaveCSS('width', '30px');
    await expect(sheet.locator('.tab-badge')).toHaveCount(1);
    await expect(sheet.locator('.load-line')).toHaveCSS('height', '2px');
    await expectAccessible(page); await expectNoUnstyledControls(page);
  }
});
