import { test, expect, type Locator, type Page } from '@playwright/test';
import { tokenColorIn } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, type PlacesSeed } from './support/mock-places';

// Places 10a and Interactions accessibility supply the row measurements and words.
// I2.1 removes Inbox, while I2.6 retains closed work on Home rather than an Inbox.
// A fixed wall clock and distinct visit times make newest-first order deterministic.
// Browser fixtures feed the real shell; no production state is injected into React.
const fixtureTime = '2026-10-10T13:00:00Z';
const populated: PlacesSeed = {
  places: [
    { name: 'Software', pinned: true, tint: 'iris' },
    { name: 'Config parser', parents: ['Software'], lastOpenedAt: fixtureTime },
    { name: 'Marketing', tint: 'rose', lastOpenedAt: '2026-10-10T12:59:00Z' },
    { name: 'Running place', pinned: true },
    { name: 'Archived place', pinned: true, archived: true },
  ],
  chats: [
    { id: 'need-a', places: ['Config parser'], needsYou: true },
    { id: 'need-b', places: ['Config parser'], needsYou: true, tasks: { failed: 1 } },
    { id: 'failed', places: ['Marketing'], tasks: { failed: 1 } },
    { id: 'running', places: ['Running place'], live: true, tasks: { running: 1 } },
  ],
};
const rail = (page: Page) => page.locator('.place-rail:visible');
const row = (page: Page, name: string) => rail(page).locator('.rail-row').filter({
  has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}(?: ·|$)`) }),
});
const main = (page: Page, name: string) => row(page, name).locator('.rail-row-main');

async function boot(page: Page, theme: 'light' | 'dark', seed: PlacesSeed) {
  await page.clock.setFixedTime(new Date(fixtureTime));
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, { now: fixtureTime, ...seed });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  return places;
}

// Tab through actual controls so focus-visible and the drawer's initial focus are exercised.
async function tabTo(page: Page, target: Locator) {
  for (let attempt = 0; attempt < 25; attempt++) {
    if (await target.evaluate(el => el === document.activeElement)) return;
    await page.keyboard.press('Tab');
  }
  await expect(target).toBeFocused();
}
async function openRail(page: Page) {
  if (page.viewportSize()!.width > 600) return;
  const show = page.getByRole('button', { name: 'Show sidebar', exact: true });
  await tabTo(page, show);
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Hide sidebar', exact: true })).toBeFocused();
}
async function primary(page: Page) {
  return await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control';
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 850, 1200]) {
    test(`PL-043/044/045/047/052/057/072/258/259 · ${theme} · ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      const places = await boot(page, theme, populated);
      await openRail(page);
      const list = rail(page);
      await expect(list.getByRole('region', { name: 'Pinned', exact: true })).toBeVisible();
      await expect(list.getByRole('region', { name: 'Open', exact: true })).toBeVisible();
      await expect(list.getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
      await expect(list.getByText('Archived place', { exact: true })).toHaveCount(0);
      expect(await list.locator('[data-rail-item]').evaluateAll(rows => rows.map(el => el.getAttribute('data-rail-item'))))
        .toEqual(['now', places.id('Software'), places.id('Running place'), places.id('Config parser'), places.id('Marketing'), 'all']);
      await expect(main(page, 'Config parser').locator('.rail-place-name')).toHaveText('Config parser · Software');
      await expect(list.getByRole('button', { name: 'All places' })).toBeVisible();
      await expect(list.getByRole('button', { name: 'Close all', exact: true })).toBeVisible();
      await expect(list.getByRole('region', { name: 'Pinned' }).locator('.rail-row-close')).toHaveCount(0);
      const config = main(page, 'Config parser');
      const before = await config.boundingBox();
      expect(await config.evaluate(el => {
        const s = getComputedStyle(el), r = el.getBoundingClientRect();
        return [r.height, s.fontSize, s.gap, s.borderRadius];
      })).toEqual([32, '13px', '10px', '8px']);
      const ink = await config.evaluate(el => getComputedStyle(el).color);
      const fill = await config.evaluate(el => getComputedStyle(el).backgroundColor);
      await config.hover();
      await expect(config).not.toHaveCSS('background-color', fill);
      await expect(config).toHaveCSS('color', ink);
      expect(await config.boundingBox()).toEqual(before);
      await expect(row(page, 'Config parser').getByRole('button', { name: 'Close Config parser', exact: true })).toHaveCSS('opacity', '1');
      for (const [name, label, token] of [
        ['Software', '2 need you in Config parser', 'amber'],
        ['Marketing', '1 failed task in Marketing', 'danger'],
      ]) {
        const mark = row(page, name).getByRole('img', { name: label, exact: true });
        await expect(mark).toHaveAccessibleName(label);
        const dot = mark.locator('.status-mark-dot');
        expect((await dot.boundingBox())!.width).toBe(6);
        expect((await dot.boundingBox())!.height).toBe(6);
        await expect(dot).toHaveCSS('background-color', await tokenColorIn(mark, token));
        await mark.hover();
        await expect(page.getByRole('tooltip', { name: label, exact: true })).toHaveText(label);
        await page.keyboard.press('Escape');
        await expect(page.getByRole('tooltip', { name: label, exact: true })).toHaveCount(0);
        await openRail(page);
      }
      await expect(row(page, 'Running place').getByRole('img')).toHaveCount(0);
      await tabTo(page, list.getByRole('button', { name: 'Now', exact: true }));
      await page.keyboard.press('ArrowUp');
      await expect(list.getByRole('button', { name: 'Now', exact: true })).toBeFocused();
      await page.keyboard.press('ArrowDown');
      await expect(main(page, 'Software')).toBeFocused();
      await page.keyboard.press('ArrowDown');
      await expect(main(page, 'Running place')).toBeFocused();
      await page.keyboard.press('ArrowDown');
      await expect(config).toBeFocused();
      expect(await config.evaluate(el => el.matches(':focus-visible'))).toBe(true);
      await expect(config).toHaveCSS('box-shadow', /2px.*4px/);
      // Moving the cursor must not navigate or write a visit before Enter.
      expect(places.posts('/places/rail').filter(call => call.body?.op === 'visit')).toHaveLength(0);
      await page.keyboard.press('End');
      await page.keyboard.press('ArrowDown');
      await expect(list.getByRole('button', { name: 'All places' })).toBeFocused();
      await page.keyboard.press('ArrowUp');
      await expect(main(page, 'Marketing')).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Marketing');
      await openRail(page);
      const selected = main(page, 'Marketing');
      await expect(selected).toHaveAttribute('aria-current', 'page');
      await expect(selected).toHaveCSS('font-weight', '500');
      await expect(selected).toHaveCSS('color', await tokenColorIn(selected, 'ink'));
      await expect(selected).not.toHaveCSS('box-shadow', 'none');
      await expect(selected).toHaveCSS('background-color', await tokenColorIn(selected, 'tab'));
      expect((await list.boundingBox())!.width).toBe(width <= 600 ? 260 : width <= 850 ? 190 : 252);
      expect(await list.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      if (width <= 600) {
        // A native modal must refuse focus on background workspace controls.
        await page.locator('.workspace-tab-actions').getByRole('button', { name: 'New tab', exact: true, includeHidden: true }).evaluate(el => (el as HTMLElement).focus());
        expect(await page.getByRole('dialog', { name: 'Navigation', exact: true }).evaluate(el => el.contains(document.activeElement))).toBe(true);
        await page.keyboard.press('Escape');
        await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).not.toBeVisible();
        await expect(page.getByRole('button', { name: 'Show sidebar', exact: true })).toBeFocused();
      }
    });

    test(`PL-063/064/259 · empty and zero · ${theme} · ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await boot(page, theme, { places: [{ name: 'Unopened place' }] });
      await openRail(page);
      await expect(rail(page).locator('.rail-hint')).toHaveText('Places you open show here. Pin the ones you live in.');
      await expect(rail(page).locator('.rail-section-label')).toHaveCount(0);
      await expect(rail(page).locator('[data-rail-item="all"] .rail-meta')).not.toBeEmpty();
      // A new browser document reads the same empty graph shape as a first launch.
      await installMockPlaces(page, {});
      await page.reload();
      await openRail(page);
      await expect(rail(page).locator('[data-rail-item]')).toHaveCount(2);
      await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
      await expect(rail(page).getByRole('button', { name: 'All places', exact: true })).toBeVisible();
      await expect(rail(page).locator('.rail-section-label,.rail-hint,[data-rail-item] .rail-meta,[data-rail-count]')).toHaveCount(0);
      await expect(rail(page).getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
    });
  }

  test(`PL-058 · exact close tooltip and independent action · ${theme}`, async ({ page }) => {
    await page.route('**/api/engine/**', route => route.abort());
    await page.goto('/');
    await page.evaluate(async theme => {
      const path = '/tests/ui/fixtures/rail-row-mount.ts';
      const { mountRailRows } = await import(path);
      mountRailRows(theme);
    }, theme);
    const marketing = main(page, 'Marketing');
    const close = row(page, 'Marketing').getByRole('button', { name: 'Close Marketing', exact: true });
    await expect(close).toHaveCSS('opacity', '0');
    await marketing.hover();
    await close.hover();
    const tooltip = page.getByRole('tooltip');
    await expect(tooltip).toHaveText('Close Marketing · 4 tabs⌘⇧W');
    await expect(tooltip.locator('.tooltip-shortcut')).toHaveText('⌘⇧W');
    await close.click();
    await expect(marketing).toHaveCount(0);
    await expect(page.locator('#visited')).toBeEmpty();
  });

  test(`PL-052/058/060 · close, restore saved tabs, finish closed work · ${theme}`, async ({ page }) => {
    const places = await boot(page, theme, {
      places: [{ name: 'Quiet', lastOpenedAt: 'now' }, { name: 'Busy', lastOpenedAt: 'now' }, { name: 'Question', lastOpenedAt: 'now' }],
      chats: [{ id: 'busy', places: ['Busy'], live: true, tasks: { running: 1 } }, { id: 'question', places: ['Question'], needsYou: true }],
    });
    await main(page, 'Quiet').click();
    const chord = await primary(page);
    await page.keyboard.press(`${chord}+KeyT`);
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toBeVisible();
    await page.keyboard.press(`${chord}+Shift+KeyW`);
    await expect(row(page, 'Quiet')).toHaveCount(0);
    await page.keyboard.press(`${chord}+KeyP`);
    await page.getByRole('dialog', { name: 'Go to a place, or create one' }).getByRole('combobox').fill('Quiet');
    await page.keyboard.press('Enter');
    await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Quiet');
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toBeVisible();
    await page.keyboard.press(`${chord}+Shift+KeyW`);
    await rail(page).getByRole('button', { name: 'All places' }).click();
    await page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: /^Quiet$/ }) }).locator('.places-tile-main').click();
    await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Quiet');
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toBeVisible();
    await row(page, 'Busy').hover();
    await row(page, 'Busy').getByRole('button', { name: 'Close Busy', exact: true }).click();
    await expect(row(page, 'Busy')).toHaveAttribute('data-busy-closed', 'true');
    await expect(row(page, 'Busy')).toContainText('closed · still running');
    await expect(main(page, 'Busy')).toHaveCSS('color', await tokenColorIn(main(page, 'Busy'), 'ink-3'));
    await expect(row(page, 'Busy').locator('.rail-row-close')).toHaveCount(0);
    await expect(row(page, 'Busy').getByRole('img')).toHaveCount(0);
    places.setChat('busy', { live: false, tasks: { running: 0 } });
    await expect(row(page, 'Busy')).toHaveCount(0);
    await row(page, 'Question').hover();
    await row(page, 'Question').getByRole('button', { name: 'Close Question', exact: true }).click();
    await expect(row(page, 'Question')).toHaveAttribute('data-busy-closed', 'true');
    await expect(row(page, 'Question').getByRole('img', { name: '1 needs you in Question', exact: true })).toBeVisible();
    places.setChat('question', { needsYou: false });
    await expect(row(page, 'Question')).toHaveCount(0);
    const closeAll = rail(page).getByRole('button', { name: 'Close all', exact: true });
    await tabTo(page, closeAll);
    await page.keyboard.press('Enter');
    await expect(rail(page).getByRole('region', { name: 'Open', exact: true })).toHaveCount(0);
    await expect(rail(page).locator('.rail-hint')).toHaveText('Places you open show here. Pin the ones you live in.');
  });
}
