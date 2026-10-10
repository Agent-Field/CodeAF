import { test, expect, type Locator, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';

// Strip acceptance for SH-056, SH-066, SH-071, SH-072, SH-073, SH-089 and SH-210.
// Widths, fills and the tooltip delay are read from tokens and from getComputedStyle / getBoundingClientRect.
const foundation = design.foundation as Record<string, string>;
const durBase = parseFloat(foundation['dur-base']) / 1000;
const pressSeconds = parseFloat(foundation['dur-press']) / 1000;
const tipDelay = design.interaction.tooltipOpenDelay;
const small = design.breakpoints.small;
const px = (name: string) => parseFloat(foundation[name]);
const KEY = 'codeaf.desktop.workspace.v1';
const LONG = 'A long conversation title that the strip can only show part of';
const themes = ['light', 'dark'] as const;

const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

async function seed(page: Page, state: unknown) {
  await page.addInitScript(([key, json]) => localStorage.setItem(key, json), [KEY, JSON.stringify(state)] as const);
}

async function theme(page: Page, value: 'light' | 'dark') {
  await page.addInitScript(chosen => localStorage.setItem('codeaf-theme', chosen), value);
}

async function open(page: Page, value: 'light' | 'dark' = 'light') {
  await theme(page, value);
  await installMockEngine(page, { initial: { title: 'Alpha' } });
  await seed(page, {
    tabs: [tab('a', 'Alpha'), tab('b', 'Beta'), tab('c', 'Gamma')],
    groups: [],
    closed: [],
    activeId: 'a',
    nextNumber: 4,
    recentIds: ['a'],
  });
  await page.setViewportSize({ width: 1200, height: 800 });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', value);
  await expect(page.getByRole('tab', { name: 'Gamma', exact: true })).toBeVisible();
}

async function modifier(page: Page) {
  return (await page.evaluate(() => /Mac/.test(navigator.platform))) ? 'Meta' : 'Control';
}

const chip = (page: Page, name: string) => page.locator('.workspace-tab').filter({ has: page.getByRole('tab', { name, exact: true }) });

async function chipBox(strip: Locator, title: string) {
  return strip.evaluate((root, name) => {
    const tab = [...root.querySelectorAll<HTMLElement>('[role="tab"]')].find(el => el.getAttribute('aria-label') === name && !el.closest('[inert]'));
    const node = tab?.closest<HTMLElement>('.workspace-tab');
    if (!node) return null;
    const view = root.getBoundingClientRect();
    const box = node.getBoundingClientRect();
    const style = getComputedStyle(node);
    return {
      width: box.width,
      cssWidth: style.width,
      minWidth: style.minWidth,
      compressed: node.getAttribute('data-compressed'),
      titles: node.querySelectorAll('.workspace-tab-title').length,
      // Positive means the chip sits inside the strip's scrollport. A negative edge is clipped.
      left: box.left - view.left,
      right: view.right - box.right,
    };
  }, title);
}

async function boot(page: Page, state: unknown, scheme: 'light' | 'dark', width: number) {
  await theme(page, scheme);
  await installMockEngine(page, { initial: { title: 'Alpha' } });
  await seed(page, state);
  await page.setViewportSize({ width, height: 800 });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', scheme);
}

const stripOf = (page: Page) => page.locator('.workspace-tabstrip');

function inView(box: { left: number; right: number } | null) {
  expect(box, 'the tab is in the strip').not.toBeNull();
  expect(box!.left).toBeGreaterThanOrEqual(-1);
  expect(box!.right).toBeGreaterThanOrEqual(-1);
}

function widthTransition(property: string, duration: string) {
  const props = property.split(',').map(part => part.trim());
  return { hasWidth: props.includes('width'), seconds: parseFloat(duration) };
}

for (const scheme of themes) {
test(`SH-073 closing a tab collapses its width so neighbours slide, and focus moves at once (${scheme})`, async ({ page }) => {
  expect(durBase).toBeCloseTo(0.2, 2);
  expect(parseFloat(foundation['dur-base'])).toBe(200);
  await open(page, scheme);
  const gamma = page.getByRole('tab', { name: 'Gamma', exact: true });
  const before = (await gamma.boundingBox())!;
  // The slot is gone after dur-base, so the sample has to be taken in the page as the node appears.
  await page.evaluate(() => {
    const bag: { property: string; duration: string; timing: string; width: number; hidden: string | null; inert: boolean; running: boolean }[] = [];
    (window as unknown as { __exit: typeof bag }).__exit = bag;
    const take = () => {
      const el = document.querySelector<HTMLElement>('.workspace-tab-exit');
      if (!el) return;
      const style = getComputedStyle(el);
      bag.push({
        property: style.transitionProperty,
        duration: style.transitionDuration,
        timing: style.transitionTimingFunction,
        width: el.getBoundingClientRect().width,
        hidden: el.getAttribute('aria-hidden'),
        inert: el.inert,
        running: el.getAnimations().some(animation => animation.playState === 'running'),
      });
    };
    const watch = () => {
      const el = document.querySelector<HTMLElement>('.workspace-tab-exit');
      if (!el || el.dataset.watching) return;
      el.dataset.watching = 'true';
      let frames = 0;
      const step = () => {
        take();
        if (++frames < 8 && document.querySelector('.workspace-tab-exit')) requestAnimationFrame(step);
      };
      take();
      requestAnimationFrame(step);
    };
    new MutationObserver(watch).observe(document.querySelector('.workspace-tabstrip')!, { childList: true, subtree: true });
  });
  await page.getByRole('button', { name: 'Close Alpha', exact: true }).click();
  // The neighbour is focused on the close, while the slot is still collapsing.
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeFocused();
  const samples = await page.evaluate(() => (window as unknown as { __exit: { property: string; duration: string; timing: string; width: number; hidden: string | null; inert: boolean; running: boolean }[] }).__exit);
  expect(samples.length).toBeGreaterThan(0);
  const motion = samples.find(sample => widthTransition(sample.property, sample.duration).hasWidth) ?? samples[0];
  const transition = widthTransition(motion.property, motion.duration);
  expect(transition.hasWidth).toBe(true);
  expect(transition.seconds).toBeCloseTo(durBase, 2);
  expect(motion.hidden).toBe('true');
  expect(motion.inert).toBe(true);
  const ease = await page.evaluate(() => {
    const probe = document.createElement('div');
    probe.style.transitionTimingFunction = 'var(--ease)';
    document.body.append(probe);
    const value = getComputedStyle(probe).transitionTimingFunction;
    probe.remove();
    return value;
  });
  const firstCurve = motion.timing.match(/^cubic-bezier\([^)]+\)/)?.[0] ?? motion.timing;
  expect(firstCurve.replaceAll(' ', '')).toBe(ease.replaceAll(' ', ''));
  const widths = samples.map(sample => sample.width);
  expect(samples.some(sample => sample.running) || Math.max(...widths) > Math.min(...widths)).toBe(true);

  await expect(page.locator('.workspace-tab-exit')).toHaveCount(0);
  const after = (await gamma.boundingBox())!;
  expect(before.x - after.x).toBeGreaterThan(parseFloat(foundation['tab-min-width']) / 2);

  const entry = await page.locator('.workspace-tab').first().evaluate(el => {
    const style = getComputedStyle(el);
    let scales = false;
    for (const sheet of document.styleSheets) {
      let rules: CSSRuleList;
      try { rules = sheet.cssRules; } catch { continue; }
      for (const rule of rules) {
        if (!(rule instanceof CSSKeyframesRule) || rule.name !== 'tab-enter') continue;
        scales = [...rule.cssRules].some(frame => frame instanceof CSSKeyframeRule && frame.style.transform !== '');
      }
    }
    return { name: style.animationName, transform: style.transform, scales };
  });
  expect(entry.name).toBe('tab-enter');
  expect(entry.transform).toBe('none');
  expect(entry.scales).toBe(false);
  await expectAccessible(page);
});
}

for (const scheme of themes) {
test(`SH-073 reduced motion removes the close slide and the entry fade (${scheme})`, async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await open(page, scheme);
  const ruled = await page.evaluate(() => {
    const host = document.querySelector('.workspace-tabstrip');
    const probe = document.createElement('div');
    probe.className = 'workspace-tab-exit';
    probe.setAttribute('data-collapsed', '');
    host?.append(probe);
    const style = getComputedStyle(probe);
    const tab = getComputedStyle(document.querySelector('.workspace-tab')!);
    const result = { property: style.transitionProperty, duration: style.transitionDuration, animation: tab.animationName };
    probe.remove();
    return result;
  });
  expect(ruled.property).toBe('none');
  expect(ruled.animation).toBe('none');

  const gamma = page.getByRole('tab', { name: 'Gamma', exact: true });
  const before = (await gamma.boundingBox())!;
  await page.getByRole('button', { name: 'Close Alpha', exact: true }).click();
  await expect(page.locator('.workspace-tab-exit')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Beta', exact: true })).toBeFocused();
  const after = (await gamma.boundingBox())!;
  expect(before.x - after.x).toBeGreaterThan(parseFloat(foundation['tab-min-width']) / 2);
  await expectAccessible(page);
});
}

const pair = {
  tabs: [tab('a', LONG), tab('b', 'Notes')],
  groups: [],
  closed: [],
  activeId: 'a',
  nextNumber: 3,
  recentIds: ['a'],
};

for (const scheme of themes) {
  test(`SH-066 a truncated active tab shows its full title after 500ms and a title that fits does not (${scheme})`, async ({ page }) => {
    expect(tipDelay).toBe(500);
    expect(design.interaction.previewOpenDelay).toBe(tipDelay);
    await boot(page, pair, scheme, 1200);
    const cut = chip(page, LONG);
    const fit = chip(page, 'Notes');
    await expect(cut).toHaveAttribute('data-title-overflow', 'true');
    await expect(fit).not.toHaveAttribute('data-title-overflow');
    // Sampled before the delay: a tooltip that appears at once means the 500ms wait was removed.
    await cut.getByRole('tab').hover();
    await page.waitForTimeout(tipDelay * 0.4);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await expect(page.locator('.tab-preview')).toHaveCount(0);
    await expect(page.getByRole('tooltip')).toHaveText(LONG);
    await page.mouse.move(0, 0);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    // An inactive tab that still has a title opens the preview, not this tooltip.
    await fit.getByRole('tab').hover();
    await page.waitForTimeout(tipDelay * 1.2);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await fit.getByRole('tab').click();
    await expect(fit).toHaveAttribute('data-active', 'true');
    await expect(fit).not.toHaveAttribute('data-title-overflow');
    await fit.getByRole('tab').hover();
    await page.waitForTimeout(tipDelay * 1.2);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await expectAccessible(page);
  });
}

for (const scheme of themes) {
  test(`SH-056 holding a tab fills it with field-2 for the press duration (${scheme})`, async ({ page }) => {
    expect(parseFloat(foundation['dur-press'])).toBe(80);
    expect(pressSeconds).toBeCloseTo(0.08, 2);
    await boot(page, { tabs: [tab('a', 'Notes'), tab('b', 'Other')], groups: [], closed: [], activeId: 'a', nextNumber: 3, recentIds: ['a'] }, scheme, 1200);
    const pressed = await tokenColor(page, 'field-2');
    const canvas = await tokenColor(page, 'canvas');
    expect(pressed).not.toBe(canvas);
    const active = chip(page, 'Notes');
    const box = (await active.boundingBox())!;
    await page.mouse.move(box.x + 16, box.y + box.height / 2);
    await page.mouse.down();
    await expect(active).toHaveCSS('background-color', pressed);
    await expect(active).toHaveCSS('color', await tokenColor(page, 'ink'));
    await expect(active.locator('.workspace-tab-select')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    const duration = await active.evaluate(element => getComputedStyle(element).transitionDuration);
    expect(duration.split(',').map(part => part.trim()).every(part => Math.abs(parseFloat(part) - pressSeconds) < 0.011)).toBe(true);
    await page.mouse.up();
    await expect(active).toHaveCSS('background-color', canvas);
    // The close mark's own press does not fill the chip. Release off the mark so the press is not a click.
    const close = active.locator('.workspace-tab-close');
    const closeBox = (await close.boundingBox())!;
    await page.mouse.move(closeBox.x + closeBox.width / 2, closeBox.y + closeBox.height / 2);
    await page.mouse.down();
    await expect(active).toHaveCSS('background-color', canvas);
    await page.mouse.move(0, 0);
    await page.mouse.up();

    await page.setViewportSize({ width: small, height: 800 });
    const quiet = chip(page, 'Other');
    await expect(quiet).toHaveAttribute('data-compressed', 'true');
    const quietBox = (await quiet.boundingBox())!;
    await page.mouse.move(quietBox.x + quietBox.width / 2, quietBox.y + quietBox.height / 2);
    await page.mouse.down();
    await expect(quiet).toHaveCSS('background-color', pressed);
    await expect(quiet.locator('.workspace-tab-select')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await page.mouse.up();
    await page.mouse.move(0, 0);
    await expectAccessible(page);
  });
}

function crowded() {
  return {
    tabs: [tab('a', LONG), ...Array.from({ length: 14 }, (_, index) => tab(`q${index}`, `Quiet ${index + 1}`)), tab('z', 'Omega')],
    groups: [],
    closed: [],
    activeId: 'a',
    nextNumber: 20,
    recentIds: ['a'],
  };
}

for (const scheme of themes) {
  test(`SH-071 SH-072 the active tab stays at least 112px and in view, and at 600px the others are 44px chips (${scheme})`, async ({ page }) => {
    test.setTimeout(90_000);
    expect(small).toBe(600);
    expect(px('tab-compressed-width')).toBe(44);
    expect(px('tab-min-width')).toBe(112);
    await boot(page, crowded(), scheme, 1200);
    const strip = stripOf(page);
    const wide = (await chipBox(strip, 'Quiet 1'))!;
    expect(wide.compressed).toBeNull();
    expect(wide.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    const first = (await chipBox(strip, LONG))!;
    inView(first);
    expect(first.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect((await chipBox(strip, 'Omega'))!.right).toBeLessThan(-8);

    const mod = await modifier(page);
    await page.getByRole('tab', { name: LONG, exact: true }).click();
    await page.keyboard.press(`${mod}+9`);
    await expect(page.getByRole('tab', { name: 'Omega', exact: true })).toHaveAttribute('aria-selected', 'true');
    const jumped = (await chipBox(strip, 'Omega'))!;
    inView(jumped);
    expect(jumped.compressed).toBeNull();
    expect(jumped.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect(jumped.titles).toBeGreaterThan(0);

    await page.setViewportSize({ width: small, height: 800 });
    expect(await page.evaluate(query => matchMedia(query).matches, `(max-width: ${small}px)`)).toBe(true);
    await expect(chip(page, 'Quiet 1')).toHaveAttribute('data-compressed', 'true');
    const activeNarrow = (await chipBox(strip, 'Omega'))!;
    inView(activeNarrow);
    expect(activeNarrow.compressed).toBeNull();
    expect(activeNarrow.minWidth).toBe(`${px('tab-min-width')}px`);
    expect(activeNarrow.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect(activeNarrow.titles).toBeGreaterThan(0);

    const compressedTitle = await strip.evaluate(root => {
      const view = root.getBoundingClientRect();
      const node = [...root.querySelectorAll<HTMLElement>('.workspace-tab[data-compressed]')].find(el => {
        if (el.closest('[inert]')) return false;
        const box = el.getBoundingClientRect();
        return box.left >= view.left - 1 && box.right <= view.right - 1 && box.width > 0;
      });
      return node?.querySelector('[role="tab"]')?.getAttribute('aria-label') ?? '';
    });
    expect(compressedTitle).not.toBe('');
    const icon = (await chipBox(strip, compressedTitle))!;
    expect(icon.compressed).toBe('true');
    expect(icon.cssWidth).toBe(`${px('tab-compressed-width')}px`);
    expect(icon.width).toBeCloseTo(px('tab-compressed-width'), 0);
    expect(icon.titles).toBe(0);
    await expect(page.getByRole('tab', { name: compressedTitle, exact: true })).toHaveAttribute('aria-label', compressedTitle);
    await page.getByRole('tab', { name: compressedTitle, exact: true }).hover();
    await page.waitForTimeout(tipDelay * 0.4);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await expect(page.getByRole('tooltip')).toHaveText(compressedTitle);
    await expect(page.locator('.tab-preview')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await expect(page.getByRole('tooltip')).toHaveCount(0);

    await page.keyboard.press(`${mod}+1`);
    await expect(page.getByRole('tab', { name: LONG, exact: true })).toHaveAttribute('aria-selected', 'true');
    const back = (await chipBox(strip, LONG))!;
    inView(back);
    expect(back.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect(back.compressed).toBeNull();
    expect((await chipBox(strip, 'Omega'))!.right).toBeLessThan(-8);

    await page.keyboard.press(`${mod}+9`);
    await expect(page.getByRole('tab', { name: 'Omega', exact: true })).toHaveAttribute('aria-selected', 'true');
    const returned = (await chipBox(strip, 'Omega'))!;
    inView(returned);
    expect(returned.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect(returned.compressed).toBeNull();
    expect((await chipBox(strip, 'Quiet 1'))!.compressed).toBe('true');
    expect((await chipBox(strip, 'Quiet 1'))!.width).toBeCloseTo(px('tab-compressed-width'), 0);

    await page.setViewportSize({ width: 320, height: 800 });
    await expect(chip(page, 'Quiet 1')).toHaveAttribute('data-compressed', 'true');
    // Resize does not re-select, so the jump has to happen at this width for the scroll-into-view effect to run.
    await page.keyboard.press(`${mod}+1`);
    await expect(page.getByRole('tab', { name: LONG, exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press(`${mod}+9`);
    await expect(page.getByRole('tab', { name: 'Omega', exact: true })).toHaveAttribute('aria-selected', 'true');
    const phone = (await chipBox(strip, 'Omega'))!;
    inView(phone);
    expect(phone.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    expect(phone.compressed).toBeNull();
    const phoneIcon = (await chipBox(strip, 'Quiet 1'))!;
    expect(phoneIcon.compressed).toBe('true');
    expect(phoneIcon.width).toBeCloseTo(px('tab-compressed-width'), 0);
    await page.mouse.move(0, 0);
    await expectAccessible(page);
  });
}

for (const scheme of themes) {
  test(`SH-089 the empty strip menu lists New tab, Reopen closed tab and Show all tabs, and each acts (${scheme})`, async ({ page }) => {
    await boot(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], groups: [], closed: [], activeId: 'a', nextNumber: 3, recentIds: ['a'] }, scheme, 1200);
    const spacer = page.locator('.workspace-tab-spacer');
    const menu = page.getByRole('menu', { name: 'Tab strip actions' });
    const tabs = page.locator('.workspace-tabstrip [role="tab"]');
    await spacer.click({ button: 'right' });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem')).toHaveCount(3);
    await expect(menu.getByRole('menuitem', { name: /^Reopen closed tab/ })).toBeDisabled();
    const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
    const chord = async (name: RegExp) => (await menu.getByRole('menuitem', { name }).locator('.keyboard-shortcut').textContent())?.replace(/\s+/g, '') ?? '';
    // Mac draws the chord tight. Other platforms join with pluses; WebKit's user agent can take the tight join and still spell Ctrl.
    expect(await chord(/^New tab/)).toMatch(mac ? /^⌘T$/ : /^Ctrl\+?T$/);
    expect(await chord(/^Reopen closed tab/)).toMatch(mac ? /^⌘⇧T$/ : /^Ctrl\+?Shift\+?T$/);
    expect(await chord(/^Show all tabs/)).toMatch(mac ? /^⌘⇧\\$/ : /^Ctrl\+?Shift\+?A$/);
    const before = await tabs.count();
    await menu.getByRole('menuitem', { name: /^New tab/ }).click();
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(tabs).toHaveCount(before + 1);
    await page.keyboard.press(`${await modifier(page)}+w`);
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveCount(0);
    await expect(tabs).toHaveCount(before);
    await spacer.click({ button: 'right' });
    await expect(menu.getByRole('menuitem', { name: /^Reopen closed tab/ })).toBeEnabled();
    await menu.getByRole('menuitem', { name: /^Reopen closed tab/ }).click();
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toBeVisible();
    await expect(tabs).toHaveCount(before + 1);
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'New tab', exact: true }).focus();
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: 'Tab strip actions', exact: true })).toBeFocused();
    await page.keyboard.press('Shift+F10');
    await expect(menu).toBeVisible();
    // The menu focuses its first row on the next frame. Moving before that frame lands back on New tab.
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(undefined)))));
    await expect(menu.getByRole('menuitem', { name: /^New tab/ })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('ArrowDown');
    await expect(menu.getByRole('menuitem', { name: /^Show all tabs/ })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('dialog', { name: 'All tabs overview' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: 'All tabs overview' })).toHaveCount(0);
    await tabs.first().click({ button: 'right' });
    await expect(menu).not.toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Rename tab', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    // Escape returns focus to the tab, which opens its preview. A click in the page blurs that and shuts the card.
    await page.mouse.click(500, 700);
    await expect(page.locator('.tab-preview')).toHaveCount(0);
    await expect(page.getByRole('menu')).toHaveCount(0);
    await expectAccessible(page);

    await page.setViewportSize({ width: 320, height: 800 });
    await page.getByRole('button', { name: 'Tab strip actions', exact: true }).focus();
    await page.keyboard.press('ContextMenu');
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem')).toHaveCount(3);
    const menuBox = (await menu.boundingBox())!;
    expect(menuBox.x).toBeGreaterThanOrEqual(0);
    expect(menuBox.x + menuBox.width).toBeLessThanOrEqual(320);
    await page.keyboard.press('Escape');
    const spacerBox = await spacer.boundingBox();
    if (spacerBox && spacerBox.width > 8) await spacer.click({ button: 'right' });
    else await page.locator('.workspace-tabbar').click({ button: 'right', position: { x: 2, y: 8 } });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: /^Show all tabs/ })).toBeVisible();
    await page.keyboard.press('Escape');
    await page.mouse.move(0, 0);
    await expect(page.getByRole('menu')).toHaveCount(0);
    await expectAccessible(page);
  });
}

function splitWorkspace() {
  const panes = [tab('p1', 'Left pane'), tab('p2', 'Right pane')];
  return {
    tabs: [tab('a', 'Alpha'), tab('sp', 'Split', { split: { layout: '1x2', focus: 0, panes } }), tab('c', 'Gamma')],
    groups: [],
    closed: [],
    activeId: 'sp',
    nextNumber: 6,
    recentIds: ['sp'],
  };
}

for (const scheme of themes) {
  test(`SH-210 ⌘W closes the whole merged tab and Close pane does not (${scheme})`, async ({ page }) => {
    await boot(page, splitWorkspace(), scheme, 1200);
    const left = page.getByRole('tab', { name: 'Left pane', exact: true });
    const right = page.getByRole('tab', { name: 'Right pane', exact: true });
    await expect(left).toBeVisible();
    await expect(right).toBeVisible();
    await expect(page.getByRole('button', { name: 'Close split Left pane and Right pane', exact: true })).toHaveCount(1);
    const split = page.locator('.workspace-split-tab');
    expect((await split.boundingBox())!.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
    await page.locator('.workspace-pane[data-focused="true"]').hover();
    await page.getByRole('button', { name: 'Pane menu Left pane', exact: true }).click();
    await expect(page.getByRole('menuitem', { name: 'Close pane', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(left).toBeVisible();
    await expect(right).toBeVisible();
    await expectAccessible(page);

    const mod = await modifier(page);
    const closeMerged = async (width: number) => {
      await page.setViewportSize({ width, height: 800 });
      await expect(left).toBeVisible();
      await expect(right).toBeVisible();
      expect((await page.locator('.workspace-split-tab').boundingBox())!.width).toBeGreaterThanOrEqual(px('tab-min-width') - 1);
      await left.focus();
      await page.keyboard.press(`${mod}+w`);
      await expect(page.locator('.workspace-tab-exit')).toHaveCount(0);
      await expect(left).toHaveCount(0);
      await expect(right).toHaveCount(0);
      await expect(page.locator('.workspace-split-tab')).toHaveCount(0);
      await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toBeVisible();
      await expect(page.getByRole('tab', { name: 'Gamma', exact: true })).toBeVisible();
      // The spacer can shrink to nothing once the strip is full, so the keyboard door reopens the closed split.
      await page.getByRole('button', { name: 'Tab strip actions', exact: true }).focus();
      await page.keyboard.press('Shift+F10');
      await page.getByRole('menuitem', { name: /^Reopen closed tab/ }).click();
      await expect(left).toBeVisible();
      await expect(right).toBeVisible();
    };
    await closeMerged(1200);
    await closeMerged(small);
    await closeMerged(320);
    await page.keyboard.press('Escape');
    await expectAccessible(page);
  });
}
