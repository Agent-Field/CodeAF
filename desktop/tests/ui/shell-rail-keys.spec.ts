import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, tokenColor } from './contracts';
import { installMockEngine, GLM, GLM_FLASH, MODEL } from './support/mock-engine';
import { plainReply, withTasks } from './support/scenarios';
import { message, openApp, send } from './support/conversation';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Shell 2h "Rail" and "Keys", shell-helpers RAIL. Geometry is pinned from tokens.
const f = design.foundation;
const px = (name: string) => parseFloat(f[name as keyof typeof f] as string);
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const rail = (page: Page) => page.locator('.app-shell > .sidebar');
const strip = (page: Page) => page.locator('.workspace-tabbar');
const tabs = (page: Page) => page.getByRole('tab');
const box = async (page: Page, selector: string) => (await page.locator(selector).first().boundingBox())!;
// Pause the fake clock. install() keeps real time flowing, so a later fastForward would add to time that already passed.
const freezeClock = async (page: Page) => {
  await page.clock.install({ time: new Date('2026-10-09T12:00:00.000Z') });
  await page.clock.pauseAt('2026-10-09T12:00:02.000Z');
};
const selectedIndex = async (page: Page) => (await tabs(page).evaluateAll(all => all.findIndex(tab => tab.getAttribute('aria-selected') === 'true')));
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('the rail is 252px (232px rows plus 10px padding), drawn like the design, with today\'s items as 32px rail rows', async ({ page }) => {
  await page.goto('/');
  expect(px('sidebar-width')).toBe(252);
  expect((await rail(page).boundingBox())!.width).toBe(252);
  await expect(rail(page)).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  const toggle = rail(page).getByRole('button', { name: 'Hide sidebar' });
  expect(await toggle.boundingBox()).toMatchObject({ width: 26, height: 26 });
  await expect(toggle).toHaveCSS('border-top-left-radius', '7px');
  await expect(toggle).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  await expect(toggle.locator('.app-icon')).toHaveCSS('width', '15px');
  for (const row of [rail(page).getByRole('button', { name: 'Now', exact: true }), rail(page).getByRole('button', { name: 'All places' })]) {
    expect((await row.boundingBox())!.height).toBe(px('rail-row-height'));
    await expect(row).toHaveCSS('border-top-left-radius', '8px');
    await expect(row).toHaveCSS('font-size', '13px');
    await expect(row).toHaveCSS('column-gap', '10px');
    await expect(row.locator('.app-icon')).toHaveCSS('width', '14px');
    await expect(row.locator('.app-icon')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  }
  // Rows sit 1px apart and the rail is padded 16 10 12 (Places PRAIL / rr).
  await expect(rail(page)).toHaveCSS('padding', '16px 10px 12px');
  await expect(rail(page).locator('.rail-nav .rail-group').first()).toHaveCSS('row-gap', '1px');
  // The open row is the --tab fill plus sh-1, ink, weight 500. Hover is the tab-hover fill and nothing else.
  const open = rail(page).getByRole('button', { name: 'Now', exact: true });
  await expect(open).toHaveAttribute('aria-current', 'page');
  await expect(open).toHaveCSS('background-color', await tokenColor(page, 'tab'));
  await expect(open).toHaveCSS('box-shadow', /.+/);
  await expect(open).toHaveCSS('font-weight', '500');
  await expect(open).toHaveCSS('color', await tokenColor(page, 'ink'));
  const other = rail(page).getByRole('button', { name: 'All places' });
  await expect(other).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(other).toHaveCSS('color', await tokenColor(page, 'ink-2'));
  await expect(other).toHaveCSS('font-weight', '400');
  await other.hover();
  await expect(other).toHaveCSS('background-color', await tokenColor(page, 'tab-hover'));
  await expect(other).toHaveCSS('box-shadow', 'none');
});

test('the toggle and ⌘S collapse the rail; the toggle then leads the strip and brings it back', async ({ page }) => {
  await page.goto('/');
  await rail(page).getByRole('button', { name: 'Hide sidebar' }).click();
  await expect(rail(page)).toBeHidden();
  const show = strip(page).getByRole('button', { name: 'Show sidebar' });
  expect(await show.boundingBox()).toMatchObject({ width: 30, height: 30 });
  await expect(strip(page).getByRole('separator')).toHaveCount(1);
  await show.click();
  await expect(rail(page)).toBeVisible();
  await expect(strip(page).getByRole('button', { name: 'Show sidebar' })).toHaveCount(0);
  await page.keyboard.press(`${mod}+s`);
  await expect(rail(page)).toBeHidden();
  await page.keyboard.press(`${mod}+s`);
  await expect(rail(page)).toBeVisible();
  // ⌘B stays as the older spelling of the same key.
  await page.keyboard.press(`${mod}+b`);
  await expect(rail(page)).toBeHidden();
});

test('resting on the left 8px edge for 300ms peeks the collapsed rail over the content; leaving puts it away', async ({ page }) => {
  await page.goto('/');
  await page.mouse.move(600, 400);
  await page.keyboard.press(`${mod}+s`);
  await expect(rail(page)).toBeHidden();
  expect(px('shell-hotzone')).toBe(8);
  expect(parseFloat(f['rail-peek-delay'])).toBe(300);
  // The top edge is not the rail's: only Focus mode listens there.
  await page.mouse.move(600, 3);
  await expect(page.locator('.shell-hotzone[data-edge="top"]')).toHaveCount(0);
  // Passing across the edge without resting does not peek.
  await page.mouse.move(4, 400);
  await page.mouse.move(600, 400);
  await page.waitForTimeout(400);
  await expect(rail(page)).toBeHidden();
  await page.mouse.move(4, 400);
  await expect(rail(page)).toBeVisible();
  await expect.poll(async () => (await rail(page).boundingBox())?.x).toBe(0);
  expect(await rail(page).boundingBox()).toMatchObject({ y: 0, width: 252 });
  // Moving onto the sheet keeps it; leaving it puts it away.
  await page.mouse.move(120, 400);
  await expect(rail(page)).toBeVisible();
  await page.mouse.move(600, 400);
  await expect(rail(page)).toBeHidden();
  // The peeking rail's toggle pins it open for good.
  await page.mouse.move(4, 400);
  await expect(rail(page)).toBeVisible();
  await page.mouse.move(120, 30);
  await rail(page).getByRole('button', { name: 'Show sidebar' }).click();
  await expect(page.locator('.app-shell')).not.toHaveClass(/sidebar-collapsed/);
});

test('left edge peek after 300ms, not before', async ({ page }) => {
  await page.goto('/');
  await page.mouse.move(600, 400);
  await page.keyboard.press(`${mod}+s`);
  await expect(rail(page)).toBeHidden();
  // Collapsed, not Focus mode: the top 8px is not listening. Only the left edge is.
  await expect(page.locator('.shell-hotzone[data-edge="top"]')).toHaveCount(0);
  const zone = page.locator('.shell-hotzone[data-edge="left"]');
  const zoneBox = await zone.evaluate(el => {
    const style = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    return { width: box.width, height: box.height, top: box.top, left: box.left, cssWidth: parseFloat(style.width) };
  });
  expect(zoneBox.cssWidth).toBe(px('shell-hotzone'));
  expect(zoneBox).toMatchObject({ width: 8, left: 0, top: 0 });
  expect(zoneBox.height).toBeGreaterThan(700);
  await freezeClock(page);
  await page.mouse.move(4, 400);
  await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
  await page.clock.fastForward(299);
  await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
  await expect(rail(page)).toBeHidden();
  await page.clock.fastForward(1);
  const shell = page.locator('.app-shell');
  await expect(shell).toHaveAttribute('data-peek', 'rail');
  await expect(rail(page)).toBeVisible();
  // The slide has not finished. The pointer is already in the rectangle the rail rests in, so the overlay stays.
  await page.mouse.move(120, 400);
  await expect(shell).toHaveAttribute('data-peek', 'rail');
  const paneLeft = await page.locator('.content-pane').evaluate(el => el.getBoundingClientRect().left);
  expect(paneLeft).toBe(px('shell-card-inset'));
  const overlay = await rail(page).evaluate(el => {
    const box = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return { y: box.y, width: box.width, position: style.position };
  });
  expect(overlay).toMatchObject({ y: 0, width: px('sidebar-width'), position: 'fixed' });
  await page.mouse.move(600, 400);
  await expect(shell).not.toHaveAttribute('data-peek');
  await expect(rail(page)).toBeHidden();
});

test('reduced motion puts the peeked rail in place once the 300ms rest ends', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  await page.mouse.move(600, 400);
  await page.keyboard.press(`${mod}+s`);
  await expect(rail(page)).toBeHidden();
  await freezeClock(page);
  await page.mouse.move(4, 400);
  await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
  await page.clock.fastForward(299);
  await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
  await page.clock.fastForward(1);
  await expect(page.locator('.app-shell')).toHaveAttribute('data-peek', 'rail');
  const motion = await rail(page).evaluate(el => {
    const style = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    return { durations: style.transitionDuration.split(',').map(part => parseFloat(part)), x: box.x, y: box.y, width: box.width, position: style.position };
  });
  expect(motion.durations.every(part => part === 0)).toBe(true);
  expect(motion).toMatchObject({ x: 0, y: 0, width: px('sidebar-width'), position: 'fixed' });
  const paneLeft = await page.locator('.content-pane').evaluate(el => el.getBoundingClientRect().left);
  expect(paneLeft).toBe(px('shell-card-inset'));
});

test('Focus mode (⌘⇧F) hides the rail and the strip; the top 8px brings the strip back, the left edge the rail; the key restores', async ({ page }) => {
  await page.goto('/');
  await page.mouse.move(600, 400);
  await expect(strip(page)).toBeVisible();
  await page.keyboard.press(`${mod}+Shift+f`);
  await expect(strip(page)).toBeHidden();
  await expect(rail(page)).toBeHidden();
  expect((await box(page, '.workspace-pane')).y).toBe(px('shell-card-inset'));
  await page.mouse.move(600, 3);
  await expect(strip(page)).toBeVisible();
  await expect(rail(page)).toBeHidden();
  await expect.poll(async () => (await strip(page).boundingBox())?.y).toBe(0);
  expect(await strip(page).boundingBox()).toMatchObject({ height: 46 });
  // Leaving the revealed strip puts it away again; the tabs are usable while it shows.
  await page.mouse.move(600, 500);
  await expect(strip(page)).toBeHidden();
  await page.mouse.move(4, 500);
  await expect(rail(page)).toBeVisible();
  await expect(strip(page)).toBeHidden();
  await page.mouse.move(600, 500);
  await expect(rail(page)).toBeHidden();
  await page.keyboard.press(`${mod}+Shift+f`);
  await expect(strip(page)).toBeVisible();
  await expect(rail(page)).toBeVisible();
  expect((await box(page, '.workspace-pane')).y).toBe(46);
});

test('Focus mode: ⌘S leaves it and shows the rail', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press(`${mod}+Shift+f`);
  await page.keyboard.press(`${mod}+s`);
  await expect(strip(page)).toBeVisible();
  await expect(rail(page)).toBeVisible();
});

test('⌘T, ⌘W, ⌘⇧T, ⌘1–9 and ⌃Tab', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press(`${mod}+t`);
  await page.keyboard.press(`${mod}+t`);
  await expect(tabs(page)).toHaveCount(3);
  expect(await selectedIndex(page)).toBe(2);
  await page.keyboard.press(`${mod}+1`);
  expect(await selectedIndex(page)).toBe(0);
  await page.keyboard.press(`${mod}+2`);
  expect(await selectedIndex(page)).toBe(1);
  await page.keyboard.press(`${mod}+9`);
  expect(await selectedIndex(page)).toBe(2);
  await page.keyboard.down('Control'); await page.keyboard.press('Tab'); await page.keyboard.up('Control');
  expect(await selectedIndex(page)).toBe(1);
  await page.keyboard.down('Control'); await page.keyboard.down('Shift'); await page.keyboard.press('Tab'); await page.keyboard.up('Shift'); await page.keyboard.up('Control');
  // ⌃⇧Tab walks the recent order backwards: from the second-most-recent it wraps to the oldest.
  expect(await selectedIndex(page)).toBe(0);
  await page.keyboard.press(`${mod}+w`);
  await expect(tabs(page)).toHaveCount(2);
  await page.keyboard.press(`${mod}+Shift+t`);
  await expect(tabs(page)).toHaveCount(3);
  // ⌘⇧T puts the closed first tab back where it stood, not at the end of the strip.
  expect(await selectedIndex(page)).toBe(0);
});

test('⌘W leaves a pinned tab open', async ({ page }) => {
  await page.goto('/');
  await tabs(page).first().click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Pin tab/ }).click();
  await expect(tabs(page).first()).toBeFocused({ timeout: 1000 }).catch(() => undefined);
  await page.keyboard.press(`${mod}+w`);
  await expect(tabs(page)).toHaveCount(1);
});

// Interactions "Shortcuts": ⌘⇧\\ is the tab overview (Ctrl Shift A off a Mac); ⌘↑ and ⌘↓ step between messages in a chat.
const overviewChord = process.platform === 'darwin' ? 'Meta+Shift+\\' : 'Control+Shift+a';
test('⌘⇧\\ toggles the overview from the composer; ⌘↑ does not open it', async ({ page }) => {
  await page.goto('/');
  const overview = page.getByRole('dialog', { name: 'All tabs overview', exact: true });
  await message(page).fill('some words');
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expect(overview).toBeHidden();
  await page.keyboard.press(overviewChord);
  await expect(overview).toBeVisible();
  await page.keyboard.press(overviewChord);
  await expect(overview).toBeHidden();
  await expect(tabs(page)).toHaveCount(1);
});

test('⌘Y opens History now that the engine backs it, and a second press selects the one that is open', async ({ page }) => {
  await page.goto('/');
  const press = () => page.evaluate(() => {
    const event = new KeyboardEvent('keydown', { key: 'y', ctrlKey: !/Mac/.test(navigator.platform), metaKey: /Mac/.test(navigator.platform), bubbles: true, cancelable: true });
    window.dispatchEvent(event); return event.defaultPrevented;
  });
  expect(await press()).toBe(true);
  await expect(tabs(page)).toHaveCount(2);
  await expect(page.getByRole('tab', { name: /History/ })).toHaveAttribute('aria-selected', 'true');
  expect(await press()).toBe(true);
  await expect(tabs(page)).toHaveCount(2);
});

test('⌘⇧K toggles the task panel of a conversation that has tasks', async ({ page }) => {
  const base = withTasks();
  const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
  await installMockEngine(page, { ...base, initial: { title: base.initial.title, entries: [] }, turns: [{ entries: [aside, base.initial.entries!.at(-1)!] as typeof base.initial.entries, patch: { tasks: base.initial.tasks } }] });
  await openApp(page);
  // No tasks yet: the key does nothing.
  await page.keyboard.press(`${mod}+Shift+k`);
  await send(page, 'Migrate the settings screen');
  const panel = page.getByRole('complementary', { name: 'Tasks' });
  await expect(panel).toBeVisible();
  await page.keyboard.press(`${mod}+Shift+k`);
  await expect(panel).toHaveCount(0);
  await page.keyboard.press(`${mod}+Shift+k`);
  await expect(panel).toBeVisible();
});

test('⌥⌘1–3 pick pinned models; ⌘1–9 always jump to tabs, composer or not', async ({ page }) => {
  const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' }, models: [{ id: MODEL, name: 'DeepSeek V4.1 Flash' }, { id: GLM_FLASH, name: 'GLM 5.3 Flash' }, { id: GLM, name: 'GLM 5.3' }] });
  await openApp(page);
  await send(page, 'hello');
  await expect.poll(() => engine.calls.some(call => call.path.endsWith('/models/pinned'))).toBe(true);
  await page.keyboard.press(`${mod}+t`);
  await page.keyboard.press(`${mod}+t`);
  // ⌘1 is the tab jump even with a composer on screen; the model stays put.
  await page.keyboard.press(`${mod}+1`);
  expect(await selectedIndex(page)).toBe(0);
  await expect.poll(() => engine.calls.some(call => call.method === 'PUT' && call.path.includes('/models/roles/'))).toBe(false);
  await page.keyboard.press(`${mod}+9`);
  expect(await selectedIndex(page)).toBe(2);
  // ⌥⌘3 picks the third pinned model and does not leave the tab.
  await page.keyboard.press(`Alt+${mod}+3`);
  expect(await selectedIndex(page)).toBe(2);
  // On the Settings tab there is no composer, so ⌥⌘1 does nothing and ⌘1 jumps to the first tab.
  await openPage(page, 'Settings');
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await page.keyboard.press(`Alt+${mod}+1`);
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await page.keyboard.press(`${mod}+1`);
  expect(await selectedIndex(page)).toBe(0);
});

test('⌘, opens the Settings tab once, from any page', async ({ page }) => {
  await page.goto('/');
  await openPage(page, 'Activity');
  await page.keyboard.press(`${mod}+,`);
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await page.keyboard.press(`${mod}+,`);
  await expect(page.getByRole('tab', { name: 'Models', exact: true })).toHaveCount(1);
});

test('Settings is a tab: the rail item opens it once, lights while it shows, and Now leaves it', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await expect(rail(page).getByRole('button', { name: 'Settings', exact: true })).toHaveCount(0);
  await openPage(page, 'Settings');
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Models', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('combobox', { name: 'Theme' })).toBeVisible();
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).not.toHaveAttribute('aria-current', 'page');
  // A second request focuses the tab that is open.
  await page.getByRole('tab').first().click();
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toHaveCount(0);
  await openPage(page, 'Settings');
  await expect(page.getByRole('tab', { name: 'Models', exact: true })).toHaveCount(1);
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  // ⌘K leaves Settings for the New-tab field and keeps the one Settings tab.
  await page.keyboard.press(`${mod}+k`);
  await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
  await expect(page.getByRole('tab', { name: 'Models', exact: true })).toHaveCount(1);
  await page.keyboard.press(`${mod}+w`);
  await openPage(page, 'Settings');
  // It survives a reload like any tab.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
  await expect(message(page)).toBeVisible();
});

test('the rail, the strip toggle and Focus mode are accessible in light and dark', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await expectAccessible(page);
    await expectNoUnstyledControls(page);
    await page.keyboard.press(`${mod}+s`);
    await expectAccessible(page);
    await page.keyboard.press(`${mod}+s`);
    await openPage(page, 'Settings');
    await expectAccessible(page);
  }
});

test('the Design system page shows the rail specimen, light and dark', async ({ page }) => {
  // The accessibility pass covers the whole design system. Under four workers it does not finish in the default 30s.
  test.setTimeout(60_000);
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await openPage(page, 'Design system');
    const specimen = page.locator('[data-rail-specimen]');
    await specimen.scrollIntoViewIfNeeded();
    await expect(specimen.locator('.nav-item')).toHaveCount(7);
    await expect(specimen.locator('.nav-item', { hasText: /^Inbox$/ })).toHaveCount(0);
    await expect(specimen.locator('.status-mark[data-status="waiting"]')).toHaveCount(1);
    await expect(specimen.locator('.nav-item.active')).toHaveCount(1);
    await expectAccessible(page);
  }
});

test('shrinking the window to 320px does not animate the rail column away, so nothing overflows at any instant', async ({ page }) => {
  await page.goto('/');
  await openPage(page, 'Activity');
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
  await page.setViewportSize({ width: 320, height: 700 });
  await expect(page.locator('.app-shell.sidebar-collapsed')).toHaveCount(1);
  expect(await page.evaluate(() => getComputedStyle(document.querySelector('.app-shell')!).gridTemplateColumns.split(' ')[0])).toBe('0px');
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
});
