import { test, expect, type Locator, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine } from './support/mock-engine';

// SH-072, Shell 3j "Compressed": at the small breakpoint an inactive tab is a 44px chip.
// Measured from getComputedStyle and getBoundingClientRect, not from a screenshot.
const foundation = design.foundation as Record<string, string>;
const px = (name: string) => parseFloat(foundation[name]);
const small = design.breakpoints.small;
const KEY = 'codeaf.desktop.workspace.v1';

const pane = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', kind: 'conversation', titleSource: 'manual', ...over });
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

async function seed(page: Page) {
  const overflow = Array.from({ length: 12 }, (_, index) => tab(`o${index}`, `Overflow ${index + 1}`));
  const state = {
    tabs: [
      tab('pin', 'Pinned', { pinned: true }),
      tab('ask', 'Release notes', { sessionFile: 'mock-session-1.jsonl' }),
      tab('read', 'Reading now'),
      tab('sp', 'Config · Fixtures', { split: { layout: '1x2', focus: 0, panes: [pane('p1', 'Config'), pane('p2', 'Fixtures', { kind: 'task' })] } }),
      ...overflow,
      tab('grouped', 'Grouped work', { groupId: 'g1' }),
    ],
    groups: [{ id: 'g1', title: 'Docs', collapsed: false }],
    closed: [],
    activeId: 'ask',
    nextNumber: 20,
    recentIds: ['ask'],
  };
  await page.addInitScript(([key, json]) => localStorage.setItem(key, json), [KEY, JSON.stringify(state)] as const);
}

const chip = (page: Page, name: string) => page.locator('.workspace-tab').filter({ has: page.getByRole('tab', { name, exact: true }) });

async function box(locator: Locator) {
  return locator.evaluate(el => {
    const style = getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    const mark = el.querySelector('.tab-dot, .workspace-tab-select .app-icon');
    const markRect = mark?.getBoundingClientRect();
    return {
      width: rect.width,
      height: rect.height,
      cssWidth: style.width,
      minWidth: style.minWidth,
      compressed: el.getAttribute('data-compressed'),
      title: el.querySelectorAll('.workspace-tab-title').length,
      markWidth: markRect?.width ?? 0,
      dx: markRect ? Math.abs(markRect.left + markRect.width / 2 - (rect.left + rect.width / 2)) : 99,
      dy: markRect ? Math.abs(markRect.top + markRect.height / 2 - (rect.top + rect.height / 2)) : 99,
    };
  });
}

for (const theme of ['light', 'dark'] as const) {
  test(`SH-072 inactive tabs are 44px chips at ${small}px and the active tab keeps its title · ${theme}`, async ({ page }) => {
    expect(small).toBe(600);
    expect(px('tab-compressed-width')).toBe(44);
    expect(px('tab-min-width')).toBe(112);
    expect(px('tab-pinned-width')).toBe(30);
    expect(px('mark-dot')).toBe(6);
    expect(px('icon-xs')).toBe(13);

    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, {
      initial: {
        title: 'Release notes',
        needsPerson: true,
        questions: [{ id: 3, kind: 'choice', ask: 'Pick one?', head: 'Pick one', options: [{ key: 'a', label: 'A' }, { key: 'b', label: 'B' }] }],
      },
    });
    await seed(page);
    await page.setViewportSize({ width: small + 200, height: 800 });
    await page.goto('/');

    const reading = () => chip(page, 'Reading now');
    const notes = () => chip(page, 'Release notes');
    await expect(reading()).not.toHaveAttribute('data-compressed', /.+/);
    await expect(reading().locator('.workspace-tab-title')).toHaveText('Reading now');
    expect((await box(reading())).width).toBeGreaterThanOrEqual(px('tab-min-width'));

    await page.setViewportSize({ width: small, height: 800 });
    expect(await page.evaluate(query => matchMedia(query).matches, `(max-width: ${small}px)`)).toBe(true);
    // The chip is a render, not only a media query: wait until the attribute is committed before measuring.
    await expect(reading()).toHaveAttribute('data-compressed', 'true');

    const quiet = await box(reading());
    expect(quiet.compressed).toBe('true');
    expect(quiet.cssWidth).toBe(`${px('tab-compressed-width')}px`);
    expect(quiet.width).toBeCloseTo(px('tab-compressed-width'), 0);
    expect(quiet.height).toBeCloseTo(px('tab-height'), 0);
    expect(quiet.title).toBe(0);
    expect(quiet.markWidth).toBeCloseTo(px('icon-xs'), 0);
    expect(quiet.dx).toBeLessThan(1.5);
    expect(quiet.dy).toBeLessThan(1.5);
    await expect(reading().locator('.tab-dot')).toHaveCount(0);
    await expect(reading().locator('.workspace-tab-select .app-icon')).toHaveCount(1);
    await expect(page.getByRole('tab', { name: 'Reading now', exact: true })).toHaveAttribute('aria-label', 'Reading now');
    await expect(page.getByRole('button', { name: 'Close Reading now', exact: true })).toHaveCount(0);

    const active = await box(notes());
    expect(active.compressed).toBeNull();
    expect(active.minWidth).toBe(`${px('tab-min-width')}px`);
    expect(active.width).toBeGreaterThanOrEqual(px('tab-min-width'));
    expect(active.title).toBe(1);
    await expect(page.getByRole('button', { name: 'Close Release notes', exact: true })).toBeVisible();

    const pinned = await box(chip(page, 'Pinned'));
    expect(pinned.compressed).toBeNull();
    expect(pinned.cssWidth).toBe(`${px('tab-pinned-width')}px`);
    expect(pinned.width).toBeCloseTo(px('tab-pinned-width'), 0);
    expect(pinned.title).toBe(0);

    const splitChip = chip(page, 'Config · Fixtures');
    const splitBox = await box(splitChip);
    expect(splitBox.compressed).toBe('true');
    expect(splitBox.width).toBeCloseTo(px('tab-compressed-width'), 0);
    expect(splitBox.title).toBe(0);
    await expect(page.getByRole('tab', { name: 'Config · Fixtures', exact: true })).toHaveAttribute('aria-label', 'Config · Fixtures');

    const member = chip(page, 'Grouped work');
    const memberBox = await box(member);
    expect(memberBox.compressed).toBe('true');
    expect(memberBox.width).toBeCloseTo(px('tab-compressed-width'), 0);
    expect(memberBox.height).toBeCloseTo(px('tab-group-height'), 0);
    const slot = await box(page.locator('.workspace-group-tab-slot', { has: member }));
    expect(slot.width).toBeCloseTo(px('tab-compressed-width'), 0);
    await expect(page.locator('.workspace-group-name')).toHaveText('Docs');

    await reading().getByRole('tab').hover();
    await expect(page.getByRole('tooltip')).toHaveText('Reading now');
    await expect(page.locator('.tab-preview')).toHaveCount(0);

    const hiddenTitle = await page.locator('.workspace-tabstrip').evaluate(strip => {
      const bounds = strip.getBoundingClientRect();
      const hidden = [...strip.querySelectorAll<HTMLElement>('[role="tab"]')].find(el => !el.closest('[inert]') && el.getBoundingClientRect().right > bounds.right + 1);
      return hidden?.getAttribute('aria-label') ?? '';
    });
    expect(hiddenTitle).not.toBe('');
    const more = page.getByRole('button', { name: 'Tab actions', exact: true });
    await expect(more).toBeVisible();
    await expect(more).toHaveText(/^\+\d+/);
    await more.click();
    const hiddenRow = page.getByRole('menuitemcheckbox', { name: hiddenTitle, exact: true });
    await hiddenRow.scrollIntoViewIfNeeded();
    await expect(hiddenRow).toBeVisible();
    await page.keyboard.press('Escape');

    await expect(notes().locator('.tab-dot[data-state="waiting"]')).toBeVisible({ timeout: 15_000 });
    const dot = notes().locator('.tab-dot[data-state="waiting"]');
    await expect(dot).toHaveCSS('width', `${px('mark-dot')}px`);
    const dotColor = await dot.evaluate(el => getComputedStyle(el).backgroundColor);
    const chipColor = await notes().evaluate(el => getComputedStyle(el).color);
    expect(dotColor).not.toBe(chipColor);

    await page.getByRole('tab', { name: 'Reading now', exact: true }).click();
    const waiting = await box(notes());
    expect(waiting.compressed).toBe('true');
    expect(waiting.width).toBeCloseTo(px('tab-compressed-width'), 0);
    expect(waiting.title).toBe(0);
    expect(waiting.markWidth).toBeCloseTo(px('mark-dot'), 0);
    await expect(page.getByRole('tab', { name: 'Release notes', exact: true })).toHaveAttribute('aria-label', 'Release notes');
    await expect(page.getByRole('tab', { name: 'Release notes', exact: true })).toHaveAttribute('aria-description', 'Needs you');
    await notes().getByRole('tab').hover();
    await expect(page.getByRole('tooltip')).toHaveText('Release notes');
    await expect(page.locator('.tab-preview')).toHaveCount(0);

    await page.getByRole('tab', { name: 'Config · Fixtures', exact: true }).click();
    await expect(page.getByRole('tab', { name: 'Config', exact: true })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Fixtures', exact: true })).toBeVisible();
    const split = page.locator('.workspace-split-tab');
    expect((await split.boundingBox())!.width).toBeGreaterThanOrEqual(px('tab-min-width'));
    await expect(split.locator('.workspace-tab-title')).toHaveCount(2);
    expect((await box(notes())).compressed).toBe('true');

    await page.setViewportSize({ width: small + 1, height: 800 });
    expect(await page.evaluate(query => matchMedia(query).matches, `(max-width: ${small}px)`)).toBe(false);
    await expect(notes()).not.toHaveAttribute('data-compressed', /.+/);
    await expect(notes().locator('.workspace-tab-title')).toHaveText('Release notes');
    await expect(notes().locator('.tab-dot[data-state="waiting"]')).toBeVisible();
    expect((await box(notes())).width).toBeGreaterThanOrEqual(px('tab-min-width'));
  });
}
