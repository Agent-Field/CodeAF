import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { plainReply, pendingQuestion } from './support/scenarios';

// Shell 2g (drag to split / group), 2h (focus keys, resize) and 3b (compact composer), measured against the design.
// Tests that need a live conversation install the mock engine; the rest run with no engine at all.
test.beforeEach(async ({ page }) => { await installMockEngine(page, plainReply()); });

const pane = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', kind: 'conversation', ...over });
const plain = (id: string, title: string) => ({ id, title, draft: '', pinned: false, titleSource: 'manual' });
async function seed(page: Page, tabs: unknown[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(t => (t as { id: string }).id) };
  await page.addInitScript(value => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem('codeaf.desktop.workspace.v1', value); sessionStorage.setItem('seeded', '1'); } }, JSON.stringify(state));
}
const split = (panes: unknown[], over: Record<string, unknown> = {}) => ({ id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', split: { layout: '1x2', focus: 0, panes, ...over } });
const saved = (page: Page) => page.evaluate(async () => (await (await fetch('/api/engine/workspaces/now')).json()).workspace);

/** Starts dragging the tab with this name and leaves the button down at `to`. */
async function dragTab(page: Page, name: string, to: { x: number; y: number }) {
  const box = (await page.getByRole('tab', { name, exact: true }).boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 8, box.y + box.height / 2 + 24, { steps: 3 });
  await page.mouse.move(to.x, to.y, { steps: 8 });
  // Browsers repeat dragover while the pointer rests; a nudge stands in for that.
  await page.mouse.move(to.x + 1, to.y);
}
const card = async (page: Page) => (await page.locator('.workspace-conversation').boundingBox())!;

test('dragging a tab to the content edge shows the split zone and pill, and dropping splits', async ({ page }) => {
  await seed(page, [plain('a', 'Config stack'), plain('b', 'Release v2.4'), plain('c', 'Lexer')], 'a');
  await page.goto('/');
  const area = await card(page);
  await dragTab(page, 'Release v2.4', { x: area.x + area.width - 20, y: area.y + area.height / 2 });
  const preview = page.locator('.split-zone-preview[data-zone="right"]');
  await expect(preview).toBeVisible();
  await expect(preview.locator('.split-zone-pill')).toHaveText('Split right');
  await expect(preview.locator('.split-zone-pill')).toHaveCSS('height', '30px');
  const zone = (await preview.boundingBox())!;
  expect(Math.abs(zone.width - (area.width / 2 - 12))).toBeLessThan(1);
  await expect(preview).toHaveCSS('border-top-left-radius', '8px');
  await page.mouse.up();
  await expect(page.locator('.split-zones')).toHaveCount(0);
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  await expect(page.locator('.workspace-split-tab')).toHaveCount(1);
  const titles = await page.locator('.pane-title').allTextContents();
  expect(titles).toEqual(['Config stack', 'Release v2.4']);
  await expect(page.locator('.workspace-pane').nth(1)).toHaveAttribute('data-focused', 'true');
  await expect.poll(async () => (await saved(page))?.tabs.some((t: { split?: unknown }) => !!t.split)).toBe(true);
  const model = await saved(page);
  expect(model.tabs.find((t: { split?: unknown }) => t.split).split.panes.map((p: { id: string }) => p.id)).toEqual(['a', 'b']);
});

test('dropping on the left edge puts the tab first; the bottom edge splits down; a 2x2 stops at four', async ({ page }) => {
  await seed(page, [plain('a', 'One'), plain('b', 'Two'), plain('c', 'Three'), plain('d', 'Four'), plain('e', 'Five')], 'a');
  await page.goto('/');
  let area = await card(page);
  await dragTab(page, 'Two', { x: area.x + 20, y: area.y + area.height / 2 });
  await expect(page.locator('.split-zone-pill')).toHaveText('Split left');
  await page.mouse.up();
  expect(await page.locator('.pane-title').allTextContents()).toEqual(['Two', 'One']);
  area = await card(page);
  await dragTab(page, 'Three', { x: area.x + area.width / 2, y: area.y + area.height - 20 });
  await expect(page.locator('.split-zone-pill')).toHaveText('Split down');
  await page.mouse.up();
  await expect(page.getByRole('tabpanel')).toHaveAttribute('data-layout', '2x2');
  area = await card(page);
  await dragTab(page, 'Four', { x: area.x + area.width - 20, y: area.y + area.height / 2 });
  await page.mouse.up();
  await expect(page.locator('.workspace-pane')).toHaveCount(4);
  // A fifth pane is refused: no zones are drawn.
  area = await card(page);
  await dragTab(page, 'Five', { x: area.x + area.width - 20, y: area.y + area.height / 2 });
  await expect(page.locator('.split-zones')).toHaveCount(0);
  await page.mouse.up();
  await expect(page.locator('.workspace-pane')).toHaveCount(4);
});

test('dragging onto another tab shows the Group target and groups both', async ({ page }) => {
  await seed(page, [plain('a', 'Config stack'), plain('b', 'Release v2.4')], 'a');
  await page.goto('/');
  const target = (await page.getByRole('tab', { name: 'Config stack', exact: true }).boundingBox())!;
  await dragTab(page, 'Release v2.4', { x: target.x + target.width / 2, y: target.y + target.height / 2 });
  const tab = page.locator('.workspace-tab', { has: page.getByRole('tab', { name: 'Config stack', exact: true }) });
  await expect(tab).toHaveAttribute('data-drop', 'group');
  expect(await tab.evaluate(el => getComputedStyle(el, '::after').content)).toBe('"Group"');
  await page.mouse.up();
  await expect(page.locator('.workspace-tab-group .workspace-tab')).toHaveCount(2);
  await expect.poll(async () => (await saved(page))?.groups.length).toBe(1);
  const model = await saved(page);
  expect(model.groups).toHaveLength(1);
  expect(model.tabs.every((t: { groupId?: string }) => t.groupId === model.groups[0].id)).toBe(true);
});

test('the outer quarter of a tab reorders instead of grouping', async ({ page }) => {
  await seed(page, [plain('a', 'One'), plain('b', 'Two'), plain('c', 'Three')], 'a');
  await page.goto('/');
  const target = (await page.getByRole('tab', { name: 'One', exact: true }).boundingBox())!;
  await dragTab(page, 'Three', { x: target.x + 6, y: target.y + target.height / 2 });
  await expect(page.locator('.workspace-tab[data-drop="before"]')).toHaveCount(1);
  await page.mouse.up();
  await expect.poll(async () => (await saved(page))?.tabs.map((t: { id: string }) => t.id)).toEqual(['c', 'a', 'b']);
});

test('control+alt arrows move focus between panes and the pane takes the keyboard', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config'), pane('p2', 'Fixtures'), pane('p3', 'Lexer')], { layout: '2x2' })], 'sp');
  await page.goto('/');
  const panes = page.locator('.workspace-pane');
  await expect(panes.nth(0)).toHaveAttribute('data-focused', 'true');
  await page.keyboard.press('Control+Alt+ArrowRight');
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'true');
  await expect(panes.nth(1).getByRole('textbox', { name: 'Message' })).toBeFocused();
  await page.keyboard.press('Control+Alt+ArrowRight');
  await expect(panes.nth(2)).toHaveAttribute('data-focused', 'true');
  await page.keyboard.press('Control+Alt+ArrowRight');
  await expect(panes.nth(2)).toHaveAttribute('data-focused', 'true');
  await page.keyboard.press('Control+Alt+ArrowLeft');
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'true');
});

test('only the focused pane has the full composer; the others keep a 36px compact field that expands on click', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config stack'), pane('p2', 'Release v2.4', { draft: 'half a thought' })])], 'sp');
  await page.goto('/');
  const panes = page.locator('.workspace-pane');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveCount(1);
  const compact = panes.nth(1).getByRole('textbox', { name: 'Reply to Release v2.4' });
  await expect(compact).toHaveAttribute('placeholder', 'Reply to Release v2.4');
  await expect(compact).toHaveValue('half a thought');
  await expect(compact).toHaveCSS('height', '36px');
  await expect(compact).toHaveCSS('border-top-left-radius', '18px');
  await expect(compact).toHaveCSS('padding-left', '16px');
  // The focused pane has no compact field, and the composer is where the field was.
  await expect(panes.nth(0).getByRole('textbox', { name: /Reply to/ })).toHaveCount(0);
  await compact.click();
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'true');
  await expect(panes.nth(1).getByRole('textbox', { name: 'Message', exact: true })).toBeFocused();
  await expect(panes.nth(1).getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('half a thought');
  await expect(panes.nth(0).getByRole('textbox', { name: 'Reply to Config stack' })).toHaveCount(1);
  await page.keyboard.type('!');
  await expect(panes.nth(1).getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('half a thought!');
  await expectAccessible(page);
});

test('typing into the compact field by keyboard focus also takes the pane', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config stack'), pane('p2', 'Release v2.4')])], 'sp');
  await page.goto('/');
  await page.locator('.workspace-pane').nth(1).getByRole('textbox', { name: 'Reply to Release v2.4' }).focus();
  await page.keyboard.type('hello');
  await expect(page.locator('.workspace-pane').nth(1)).toHaveAttribute('data-focused', 'true');
  await expect(page.locator('.workspace-pane').nth(1).getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('hello');
});

test('a pane that needs you shows its one-line tray above the compact field, and Review brings the full tray', async ({ page }) => {
  await installMockEngine(page, pendingQuestion());
  await seed(page, [split([pane('p1', 'Config stack', { sessionFile: 'a.jsonl', titleSource: 'manual' }), pane('p2', 'Release v2.4', { sessionFile: 'b.jsonl', titleSource: 'manual' })])], 'sp');
  await page.goto('/');
  const second = page.locator('.workspace-pane').nth(1);
  const line = second.locator('.split-tray');
  await expect(line).toBeVisible();
  await expect(line).toContainText('Pick a database');
  await expect(line).toHaveCSS('height', '38px');
  await expect(line).toHaveCSS('border-top-left-radius', '12px');
  const compact = second.getByRole('textbox', { name: 'Reply to Release v2.4' });
  const [lineBox, fieldBox] = await Promise.all([line.boundingBox(), compact.boundingBox()]);
  expect(lineBox!.y + lineBox!.height).toBeLessThan(fieldBox!.y);
  // The full tray belongs to the focused pane only.
  await expect(second.getByRole('region', { name: 'Waiting on you' })).toHaveCount(0);
  await expect(page.locator('.workspace-pane').first().getByRole('region', { name: 'Waiting on you' })).toBeVisible();
  await line.getByRole('button', { name: 'Review' }).click();
  await expect(second).toHaveAttribute('data-focused', 'true');
  await expect(second.getByRole('region', { name: 'Waiting on you' })).toBeVisible();
  await expect(second.locator('.split-tray')).toHaveCount(0);
});

test('pane controls show on hover, a pane closes, and the focused ring is 1.5px', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config'), pane('p2', 'Fixtures')])], 'sp');
  await page.goto('/');
  const second = page.locator('.workspace-pane').nth(1);
  const close = second.getByRole('button', { name: 'Close pane Fixtures' });
  await expect(close).toHaveCSS('opacity', '0');
  await second.hover();
  await expect(close).toHaveCSS('opacity', '1');
  await expect(page.locator('.workspace-pane').first()).toHaveCSS('box-shadow', /1\.5px/);
  await close.click();
  await expect(page.locator('.pane-header')).toHaveCount(0);
  await expect(page.locator('.workspace-pane')).toHaveCount(1);
});

test('the pane menu swaps, maximizes without unmounting, restores, and closes', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config', { draft: 'keep me' }), pane('p2', 'Fixtures')])], 'sp');
  await page.goto('/');
  const panes = page.locator('.workspace-pane');
  await panes.nth(1).hover();
  await panes.nth(1).getByRole('button', { name: 'Pane menu Fixtures' }).click();
  await page.getByRole('menuitem', { name: 'Swap' }).click();
  expect(await page.locator('.pane-title').allTextContents()).toEqual(['Fixtures', 'Config']);
  // The pane that had focus keeps it after the swap.
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'false');
  await expect(panes.nth(0)).toHaveAttribute('data-focused', 'true');
  await panes.nth(1).hover();
  await panes.nth(1).getByRole('button', { name: 'Pane menu Config' }).click();
  await page.getByRole('menuitem', { name: 'Maximize' }).click();
  await expect(panes.nth(0)).toBeHidden();
  await expect(panes.nth(1)).toHaveAttribute('data-focused', 'true');
  await expect(page.locator('.pane-resize')).toHaveCount(0);
  const area = await card(page);
  const full = (await panes.nth(1).boundingBox())!;
  expect(Math.abs(full.width - area.width)).toBeLessThan(2);
  await expect(panes.nth(1).getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('keep me');
  await panes.nth(1).hover();
  await panes.nth(1).getByRole('button', { name: 'Pane menu Config' }).click();
  await page.getByRole('menuitem', { name: 'Restore panes' }).click();
  await expect(panes.nth(0)).toBeVisible();
  await expect(page.locator('.pane-resize')).toHaveCount(1);
  await panes.nth(0).hover();
  await panes.nth(0).getByRole('button', { name: 'Pane menu Fixtures' }).click();
  await page.getByRole('menuitem', { name: 'Close pane' }).click();
  await expect(panes).toHaveCount(1);
});

test('double-clicking a divider equalizes the panes', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config'), pane('p2', 'Fixtures')], { ratios: { col: 0.7, row: 0.5 } })], 'sp');
  await page.goto('/');
  const handle = page.getByRole('separator', { name: 'Resize panes side by side' });
  const first = page.locator('.workspace-pane').first();
  const wide = (await first.boundingBox())!.width;
  await handle.dblclick();
  await expect.poll(async () => Math.abs((await first.boundingBox())!.width - (await page.locator('.workspace-pane').nth(1).boundingBox())!.width)).toBeLessThan(2);
  expect((await first.boundingBox())!.width).toBeLessThan(wide - 100);
  await expect.poll(async () => (await saved(page))?.tabs[0].split.ratios.col).toBe(0.5);
});

test('dragging a divider resizes the panes and the ratio persists', async ({ page }) => {
  await seed(page, [split([pane('p1', 'Config'), pane('p2', 'Fixtures')])], 'sp');
  await page.goto('/');
  const handle = page.getByRole('separator', { name: 'Resize panes side by side' });
  const before = (await page.locator('.workspace-pane').first().boundingBox())!;
  const area = await card(page);
  const hit = (await handle.boundingBox())!;
  expect(hit.width).toBe(8);
  await page.mouse.move(hit.x + 4, hit.y + hit.height / 2);
  await expect(handle).toHaveCSS('cursor', 'col-resize');
  await expect.poll(() => handle.evaluate(el => getComputedStyle(el, '::after').opacity)).toBe('1');
  await page.mouse.down();
  await page.mouse.move(area.x + area.width * 0.7, hit.y + hit.height / 2, { steps: 6 });
  await page.mouse.up();
  const after = (await page.locator('.workspace-pane').first().boundingBox())!;
  expect(after.width).toBeGreaterThan(before.width + 100);
  expect(Math.abs(after.width / (area.width - 8) - 0.7)).toBeLessThan(0.02);
  await expect.poll(async () => (await saved(page))?.tabs[0].split.ratios?.col).toBeGreaterThan(0.68);
  const ratios = (await saved(page)).tabs[0].split.ratios;
  expect(ratios.col).toBeGreaterThan(0.68);
  expect(ratios.col).toBeLessThan(0.72);
  await page.reload();
  const reloaded = (await page.locator('.workspace-pane').first().boundingBox())!;
  expect(Math.abs(reloaded.width - after.width)).toBeLessThan(2);
  // The divider also moves with the arrow keys, and never past the 20% floor.
  await handle.focus();
  for (let i = 0; i < 40; i++) await page.keyboard.press('ArrowLeft');
  await expect.poll(async () => (await saved(page))?.tabs[0].split.ratios.col).toBeCloseTo(0.2, 5);
  await expectAccessible(page);
});

test('the Design system page shows the drag targets, the pane header and the compact composer, light and dark', async ({ page }) => {
  // The accessibility pass covers the whole design system. Under four workers it does not finish in the default 30s.
  test.setTimeout(60_000);
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await openPage(page, 'Design system');
    const drag = page.locator('[data-drag-specimen]');
    await drag.scrollIntoViewIfNeeded();
    await expect(drag.locator('.workspace-tab[data-drop="group"]')).toHaveCSS('box-shadow', /1\.5px/);
    await expect(drag.locator('.split-zone-pill')).toHaveText('Split right');
    const panes = page.locator('[data-split-specimen]');
    await expect(panes.locator('.pane-header').first()).toHaveCSS('height', '40px');
    await expect(panes.locator('.workspace-pane[data-focused="true"]')).toHaveCSS('box-shadow', /1\.5px/);
    await expect(panes.getByRole('textbox', { name: 'Reply to Release v2.4' })).toHaveCSS('height', '36px');
    await expect(panes.locator('.split-tray')).toHaveCSS('height', '38px');
    await expectAccessible(page);
  }
});
