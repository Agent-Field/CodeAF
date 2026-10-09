import { openPage } from './support/shell-navigation';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface, menuSurface, tokenColor } from './contracts';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Design 3g (tab menu, group label menu), Components "Context menu" and 3l (closing running work).
// The mock engine holds one session; a tab with that sessionFile is running, a tab without one is idle.
const f = design.foundation;
const px = (name: string) => parseFloat(f[name as keyof typeof f] as string);
const SESSION = 'mock-session-1.jsonl';
const mac = process.platform === 'darwin';

type Seed = { id: string; title: string; running?: boolean; groupId?: string; pinned?: boolean };
async function seed(page: Page, tabs: Seed[], opts: { active?: string; groups?: { id: string; title: string }[]; closed?: Seed[] } = {}) {
  const make = (t: Seed) => ({ id: t.id, title: t.title, draft: '', titleSource: 'manual', kind: 'conversation', pinned: !!t.pinned, groupId: t.groupId, ...(t.running ? { sessionFile: SESSION } : {}) });
  const state = {
    tabs: tabs.map(make), groups: (opts.groups ?? []).map(g => ({ ...g, collapsed: false })), closed: (opts.closed ?? []).map(make),
    activeId: opts.active ?? tabs[0].id, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id),
  };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const running = { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas across the config stack' }] } };
const slot = (page: Page, title: string) => page.locator('.workspace-tab', { has: page.getByRole('tab', { name: title, exact: true }) });
const closeButton = (page: Page, title: string) => slot(page, title).locator('.workspace-tab-close');
const names = async (page: Page) => {
  await expect(page.getByRole('tab').first()).toBeVisible();
  return page.getByRole('tab').evaluateAll(els => els.map(el => el.getAttribute('aria-label')));
};
const stops = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop')).length;
async function openMenu(page: Page, title: string, menuTitle = title) {
  await page.getByRole('tab', { name: title, exact: true }).click({ button: 'right' });
  const menu = page.getByRole('menu', { name: `Actions for ${menuTitle}`, exact: true });
  await expect(menu).toBeVisible();
  return menu;
}
const labels = (menu: Locator) => menu.locator(':scope > [role^="menuitem"] .menu-label').allTextContents();

test.describe('tab and group menus (3g)', () => {
  test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

  test('the tab menu lists the designed items with shortcuts, in the designed surface', async ({ page }) => {
    await seed(page, [{ id: 'a', title: 'Config stack' }, { id: 'b', title: 'lexer.go' }, { id: 'c', title: 'Release v2.4', groupId: 'g' }], { groups: [{ id: 'g', title: 'Trailing commas' }], active: 'a' });
    await page.goto('/');
    const menu = await openMenu(page, 'lexer.go');
    // Copy link and Move to new window are ABSENT here, not disabled: these tabs were never sent, so nothing durable
    // stands behind them to link to (tab-deep-links.spec covers saved tabs), and a tab moves only in the desktop app.
    // A capability that cannot work is left off the menu.
    expect(await labels(menu)).toEqual(['Open in split', 'Add to group', 'Pin tab', 'Duplicate', 'Rename tab', 'Close tab', 'Close other tabs', 'Close tabs to the right']);
    await expect(menu.getByRole('menuitem', { name: /^Close tab\b/ }).locator('kbd')).toHaveText(mac ? '⌘W' : 'Ctrl W');
    // Detailed Shell 3g: 240px content plus 5px padding each side (250px outer), radius 10, 28px rows.
    // Layout width, not the bounding box: the entry animation scales the surface for a few frames.
    expect(await menu.evaluate(el => (el as HTMLElement).offsetWidth)).toBe(px('menu-wide-min-width') + 2 * px('menu-pad'));
    await expect(menu).toHaveCSS('border-top-left-radius', f['radius-menu']);
    await expect(menu).toHaveCSS('padding-top', f['menu-pad']);
    await expect(menu).toHaveCSS('border-top-width', '0px');
    await expect(menu).toHaveCSS('box-shadow', await page.evaluate(() => { const probe = document.createElement('div'); probe.style.boxShadow = 'var(--sh-2)'; document.body.append(probe); const value = getComputedStyle(probe).boxShadow; probe.remove(); return value; }));
    const row = menu.getByRole('menuitem', { name: 'Pin tab' });
    expect(await row.evaluate(el => (el as HTMLElement).offsetHeight)).toBe(px('menu-item-height'));
    await expect(row).toHaveCSS('font-size', '13px');
    await expect(row).toHaveCSS('border-top-left-radius', '6px');
    await expect(row).toHaveCSS('padding-left', '10px');
    await expect(row.locator('.app-icon')).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    await expect(menu.locator('.menu-separator').first()).toHaveCSS('height', '0.5px');
    await expectThemedSurface(page, menu, menuSurface);
    await expectAccessible(page);
  });

  test('the whole menu works from the keyboard, including the Add to group submenu', async ({ page }) => {
    await seed(page, [{ id: 'a', title: 'Config stack' }, { id: 'b', title: 'lexer.go' }, { id: 'c', title: 'Release v2.4', groupId: 'g' }], { groups: [{ id: 'g', title: 'Trailing commas' }], active: 'b' });
    await page.goto('/');
    await page.getByRole('tab', { name: 'lexer.go', exact: true }).focus();
    await page.keyboard.press('Shift+F10');
    const menu = page.getByRole('menu', { name: 'Actions for lexer.go', exact: true });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Open in split' })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(menu.getByRole('menuitem', { name: 'Add to group' })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    const submenu = page.getByRole('menu', { name: /Add to group/ });
    await expect(submenu).toBeVisible();
    expect(await labels(submenu)).toEqual(['Trailing commas', 'New group…']);
    await expect(submenu.getByRole('menuitemcheckbox', { name: 'Trailing commas' })).toBeFocused();
    await page.keyboard.press('ArrowLeft');
    await expect(submenu).not.toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Add to group' })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('Enter');
    await expect(menu).not.toBeVisible();
    await expect(page.locator('.workspace-tab-group').getByRole('tab', { name: 'lexer.go', exact: true })).toBeVisible();
    // Escape closes a menu and puts focus back on the tab that owns it.
    await page.getByRole('tab', { name: 'Config stack', exact: true }).focus();
    await page.keyboard.press('Shift+F10');
    await expect(page.getByRole('menu', { name: 'Actions for Config stack', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toBeFocused();
  });

  test('pin, duplicate, split, close others and close to the right act on the strip', async ({ page }) => {
    await seed(page, [{ id: 'a', title: 'One' }, { id: 'b', title: 'Two' }, { id: 'c', title: 'Three' }, { id: 'd', title: 'Four' }]);
    await page.goto('/');
    await (await openMenu(page, 'Two')).getByRole('menuitem', { name: 'Duplicate' }).click();
    expect(await names(page)).toEqual(['One', 'Two', 'Two', 'Three', 'Four']);
    await (await openMenu(page, 'Four')).getByRole('menuitem', { name: 'Pin tab' }).click();
    expect((await names(page))[0]).toBe('Four');
    await (await openMenu(page, 'Three')).getByRole('menuitem', { name: 'Open in split' }).hover();
    await page.getByRole('menuitem', { name: 'One', exact: true }).click();
    await expect(page.locator('.workspace-split-tab')).toHaveCount(1);
    // A split takes the place of the tab it was opened from (Three), after the two copies of Two; each pane is a tab segment.
    await expect(page.getByRole('tab')).toHaveCount(5);
    await page.getByRole('tab', { name: 'Two', exact: true }).first().click({ button: 'right' });
    await page.getByRole('menu', { name: 'Actions for Two', exact: true }).getByRole('menuitem', { name: 'Close tabs to the right' }).click();
    await expect(page.getByRole('tab')).toHaveCount(2);
    await page.keyboard.press(`${mac ? 'Meta' : 'Control'}+Shift+t`);
    // The split, closed last, comes back whole: two segments.
    await expect(page.getByRole('tab')).toHaveCount(4);
    await (await openMenu(page, 'Four')).getByRole('menuitem', { name: 'Close other tabs' }).click();
    await expect(page.getByRole('tab')).toHaveCount(1);
  });

  test('the group label menu renames, collapses, ungroups and closes its tabs in danger ink', async ({ page }) => {
    await seed(page, [{ id: 'a', title: 'Solo' }, { id: 'b', title: 'Config stack', groupId: 'g' }, { id: 'c', title: 'Fixtures', groupId: 'g' }], { groups: [{ id: 'g', title: 'Trailing commas' }] });
    await page.goto('/');
    const label = page.locator('.workspace-group-label');
    await label.click({ button: 'right' });
    const menu = page.getByRole('menu', { name: 'Actions for group Trailing commas', exact: true });
    expect(await labels(menu)).toEqual(['Rename', 'Open as split', 'Collapse', 'Ungroup', 'Close 2 tabs']);
    const danger = menu.getByRole('menuitem', { name: 'Close 2 tabs' });
    await expect(danger).toHaveCSS('color', await tokenColor(page, 'danger'));
    expect(await menu.evaluate(el => (el as HTMLElement).offsetWidth)).toBe(px('menu-popup-min-width') + 2 * px('menu-pad'));
    await expectThemedSurface(page, menu, menuSurface);
    await menu.getByRole('menuitem', { name: 'Collapse' }).click();
    await expect(label).toHaveAttribute('aria-expanded', 'false');
    await label.click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Expand' }).click();
    await label.click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Ungroup' }).click();
    await expect(page.locator('.workspace-group-label')).toHaveCount(0);
    await page.getByRole('tab', { name: 'Fixtures', exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Add to group' }).hover();
    await page.getByRole('menuitem', { name: 'New group…' }).click();
    await page.locator('.workspace-group-label').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close 1 tab' }).click();
    await expect(page.locator('.workspace-group-label')).toHaveCount(0);
    expect(await names(page)).toEqual(['Solo', 'Config stack']);
  });
});

test.describe('closing running work (3l)', () => {
  test('the close button keeps work running, shows its tooltip, and Option turns it into a stop square', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Config stack', running: true }, { id: 'b', title: 'lexer.go' }], { active: 'a' });
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toBeVisible();
    // The mark is silent for running work, so wait on the engine read instead.
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    const button = closeButton(page, 'Config stack');
    await slot(page, 'Config stack').hover();
    await expect(button).toHaveAttribute('data-close-mode', 'close');
    await button.hover();
    await expect(page.getByRole('tooltip')).toHaveText(`Close · keeps running${mac ? '⌘W' : 'Ctrl W'}`);
    await page.keyboard.down('Alt');
    await expect(button).toHaveAttribute('data-close-mode', 'stop');
    await expect(button).toHaveAccessibleName('Close and stop Config stack');
    await expect(button.locator('[data-icon="stop"]')).toBeVisible();
    await expect(page.getByRole('tooltip')).toHaveText(`Close and stop${mac ? '⌥⌘W' : 'Ctrl Alt W'}`);
    expect((await button.boundingBox())!.width).toBe(px('tab-close-slot'));
    // An idle tab ignores Option: there is nothing to stop.
    const idle = closeButton(page, 'lexer.go');
    await slot(page, 'lexer.go').hover();
    await expect(idle).toHaveAttribute('data-close-mode', 'close');
    await page.keyboard.up('Alt');
    await slot(page, 'Config stack').hover();
    await expect(button).toHaveAttribute('data-close-mode', 'close');
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toBeVisible();
    expect(stops(engine)).toBe(0);
  });

  test('clicking the stop square closes the tab and stops the engine session', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Config stack', running: true }, { id: 'b', title: 'lexer.go' }], { active: 'a' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    const button = closeButton(page, 'Config stack');
    await slot(page, 'Config stack').hover();
    await page.keyboard.down('Alt');
    await expect(button).toHaveAttribute('data-close-mode', 'stop');
    await button.click();
    await page.keyboard.up('Alt');
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveCount(0);
    await expect.poll(() => stops(engine)).toBe(1);
    expect(engine.snapshot().running).toBe(false);
    // Stopping is not leaving work behind: no toast, no Inbox.
    await expect(page.locator('.toast')).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  });

  test('the explicit paths: right-click lists both closes for running work, and the key stops it', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Config stack', running: true }, { id: 'b', title: 'lexer.go' }, { id: 'c', title: 'Extra', running: true }], { active: 'a' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    const idle = await openMenu(page, 'lexer.go');
    await expect(idle.getByRole('menuitem', { name: /^Close and stop/ })).toHaveCount(0);
    await page.keyboard.press('Escape');
    const menu = await openMenu(page, 'Config stack');
    await expect(menu.getByRole('menuitem', { name: /^Close tab\b/ })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: /^Close and stop/ }).locator('kbd')).toHaveText(mac ? '⌥⌘W' : 'Ctrl Alt W');
    await menu.getByRole('menuitem', { name: /^Close and stop/ }).click();
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveCount(0);
    await expect.poll(() => stops(engine)).toBe(1);
    // The shortcut acts on the active tab.
    await engine.update({ running: true });
    await page.getByRole('tab', { name: 'Extra', exact: true }).click();
    await expect.poll(() => engine.snapshot().running).toBe(true);
    await expect(slot(page, 'Extra')).toBeVisible();
    await page.waitForTimeout(300);
    await page.keyboard.press(mac ? 'Meta+Alt+w' : 'Control+Alt+w');
    await expect(page.getByRole('tab', { name: 'Extra', exact: true })).toHaveCount(0);
    await expect.poll(() => stops(engine)).toBe(2);
  });

  test('after closing running work a 6s toast offers Stop it and Undo; Undo puts the tab back where it was', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }, { id: 'c', title: 'lexer.go' }], { active: 'b' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await slot(page, 'Config stack').hover();
    await closeButton(page, 'Config stack').click();
    const toast = page.locator('.toast');
    await expect(toast).toContainText('Config stack closed and still running');
    expect(await toast.getByRole('button').allTextContents()).toEqual(['Stop it', 'Undo']);
    // Design 3l: 40px tall, radius 12, surface and sh-2, placed at the bottom.
    const box = (await toast.boundingBox())!;
    expect(await toast.evaluate(el => (el as HTMLElement).offsetHeight)).toBe(px('toast-height'));
    await expect(toast).toHaveCSS('border-top-left-radius', '12px');
    await expect(toast).toHaveCSS('background-color', await tokenColor(page, 'surface'));
    expect(box.y + box.height).toBeGreaterThan(700);
    await expect(toast.getByRole('button', { name: 'Undo' })).toHaveCSS('background-color', await tokenColor(page, 'field'));
    await expect(toast.getByRole('button', { name: 'Undo' })).toHaveCSS('height', '26px');
    await expectAccessible(page);
    await toast.getByRole('button', { name: 'Undo' }).click();
    await expect(toast).toHaveCount(0);
    // The work outlived its tab, so the pinned Inbox opened first in the strip and stays after Undo.
    expect(await names(page)).toEqual(['Inbox', 'Intro', 'Config stack', 'lexer.go']);
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
    expect(stops(engine)).toBe(0);
  });

  test('Stop it stops the closed work, and the toast leaves after six seconds on its own', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }], { active: 'b' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await slot(page, 'Config stack').hover();
    await closeButton(page, 'Config stack').click();
    const toast = page.locator('.toast');
    await toast.getByRole('button', { name: 'Stop it' }).click();
    await expect(toast).toHaveCount(0);
    await expect.poll(() => stops(engine)).toBe(1);
    expect(engine.snapshot().running).toBe(false);
    // Left alone the toast goes away without touching the work.
    await engine.update({ running: true });
    await page.keyboard.press(`${mac ? 'Meta' : 'Control'}+Shift+t`);
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toBeVisible();
    await slot(page, 'Config stack').hover();
    await closeButton(page, 'Config stack').click();
    await expect(toast).toBeVisible();
    await page.mouse.move(0, 400);
    await expect(toast).toHaveCount(0, { timeout: design.interaction.toastDuration + 3000 });
    expect(stops(engine)).toBe(1);
  });

  test('closed-but-running work lists in the pinned Inbox and a click reopens the tab where it was', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }, { id: 'c', title: 'lexer.go' }], { active: 'b' });
    await page.goto('/');
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await slot(page, 'Config stack').hover();
    await closeButton(page, 'Config stack').click();
    // The Inbox is the first, pinned, icon-only tab; running work alone gives it no dot.
    const inbox = page.getByRole('tab', { name: 'Inbox', exact: true });
    await expect(inbox).toBeVisible();
    expect((await names(page))[0]).toBe('Inbox');
    await expect(slot(page, 'Inbox')).toHaveClass(/is-pinned/);
    await expect(page.locator('.tab-badge')).toHaveCount(0);
    await inbox.click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card.getByRole('heading', { name: 'Running in the background' })).toBeVisible();
    const row = card.getByRole('button', { name: /Config stack/ });
    await expect(row).toContainText('now');
    // Design: 300px card, radius 12, 32px rows, a 6px accent dot.
    expect((await card.boundingBox())!.width).toBe(px('inbox-width'));
    await expect(card).toHaveCSS('border-top-left-radius', '12px');
    expect((await row.boundingBox())!.height).toBe(px('inbox-row-height'));
    await expect(row.locator('.inbox-dot')).toHaveCSS('width', '6px');
    await expectAccessible(page);
    await row.click();
    expect(await names(page)).toEqual(['Inbox', 'Intro', 'Config stack', 'lexer.go']);
    await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
  });

  test('stopping the closed work clears it from the Inbox', async ({ page }) => {
    const engine = await installMockEngine(page, running);
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }], { active: 'b' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await slot(page, 'Config stack').hover();
    await closeButton(page, 'Config stack').click();
    await page.getByRole('tab', { name: 'Inbox', exact: true }).click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card.getByRole('button', { name: /Config stack/ })).toBeVisible();
    await page.locator('.toast').getByRole('button', { name: 'Stop it' }).click();
    await expect(card.getByRole('button', { name: /Config stack/ })).toHaveCount(0, { timeout: 8000 });
    await expect(card).toContainText('lands here');
  });

  test('the Inbox carries a dot only when something needs you', async ({ page }) => {
    const asked = pendingQuestion();
    const engine = await installMockEngine(page, { ...asked, initial: { ...asked.initial, needsPerson: false, questions: [], running: false } });
    await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Release v2.4', running: true }], { active: 'a' });
    await page.goto('/');
    await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
    await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
    await engine.update({ running: true, needsPerson: true, questions: asked.initial.questions });
    const inbox = page.getByRole('tab', { name: 'Inbox', exact: true });
    await expect(inbox).toBeVisible({ timeout: 8000 });
    await expect(page.locator('.tab-badge')).toHaveCount(1);
    await expect(inbox).toHaveAccessibleDescription('Needs you');
    await inbox.click();
    const card = page.getByRole('region', { name: 'Inbox' });
    await expect(card.getByRole('heading', { name: 'Needs you' })).toBeVisible();
    await card.getByRole('button', { name: /Release v2.4/ }).click();
    await expect(page.getByRole('tab', { name: 'Release v2.4', exact: true })).toHaveAttribute('aria-selected', 'true');
    await engine.update({ needsPerson: false, questions: [], running: false });
    await expect(page.locator('.tab-badge')).toHaveCount(0, { timeout: 8000 });
  });
});

test('the Design system page shows the menus, the closing states and the Inbox, light and dark', async ({ page }) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await openPage(page, 'Design system');
    const sheet = page.locator('[data-closing-specimen]');
    await sheet.scrollIntoViewIfNeeded();
    await expect(sheet.locator('.closing-specimen-menu')).toHaveCount(4);
    await expect(sheet.locator('.closing-specimen-menu').first()).toHaveCSS('border-top-left-radius', '10px');
    await expect(sheet.locator('.menu-item-danger')).toHaveCSS('color', await tokenColor(page, 'danger'));
    await expect(sheet.locator('.toast')).toHaveCount(2);
    await expect(sheet.locator('.toast').first()).toHaveCSS('height', '40px');
    await expect(sheet.locator('[data-close-mode="stop"]')).toHaveCount(1);
    await expect(sheet.locator('.inbox-card')).toHaveCount(2);
    await expectAccessible(page); await expectNoUnstyledControls(page);
  }
});
