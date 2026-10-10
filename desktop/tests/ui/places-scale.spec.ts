import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installPlacesEngine } from './support/places-engine';
import { placesFixture, PLACES_NOW } from './support/places-fixture';

// PL-011, PL-071 and PL-197 exercise the real shell, not the standalone palette specimen.
// Places 6c measures 34px rows and a 15px search field; 6d keeps the rail a working set at 200 places.
const widths = [320, 600, 850, 1200] as const;
const themes = ['light', 'dark'] as const;

async function tabTo(page: Page, target: Locator) {
  for (let attempt = 0; attempt < 30; attempt++) {
    if (await target.evaluate(el => el === document.activeElement)) return;
    await page.keyboard.press('Tab');
  }
  await expect(target).toBeFocused();
}

async function boot(page: Page, width: number, theme: typeof themes[number]) {
  await page.setViewportSize({ width, height: 800 });
  await page.clock.setFixedTime(new Date(PLACES_NOW));
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const seed = placesFixture('200-places');
  expect(seed.places).toHaveLength(200);
  expect(seed.places!.filter(place => place.parents?.includes('reports'))).toHaveLength(47);
  // The shared fixture's readable ids are wire fixtures; navigation requires canonical place ids.
  const ids = new Map(seed.places!.map((place, index) => [place.id!, `pl_${(index + 1).toString(16).padStart(16, '0')}`]));
  seed.places = seed.places!.map(place => ({ ...place, id: ids.get(place.id!), parents: place.parents?.map(id => ids.get(id)!) }));
  seed.chats = seed.chats?.map(chat => ({ ...chat, places: chat.places?.map(id => ids.get(id)!) }));
  const engine = await installPlacesEngine(page, seed);
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  // Waiting for an actual row prevents a shortcut from racing the initial graph read.
  await expect(page.locator('.app-shell .place-rail').first().getByText('Personal', { exact: true })).toBeAttached();
  return engine;
}

for (const theme of themes) for (const width of widths) {
  test(`PL-011 PL-071: 200 places keep a bounded keyboard rail · ${theme} ${width}px`, async ({ page }) => {
    const engine = await boot(page, width, theme);
    if (width <= 600) {
      await tabTo(page, page.getByRole('button', { name: 'Show sidebar', exact: true }));
      await page.keyboard.press('Enter');
      await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).toBeVisible();
    }
    const rail = page.locator('.place-rail:visible');
    await expect(rail.getByRole('region', { name: 'Pinned', exact: true })).toBeVisible();
    await expect(rail.getByRole('region', { name: 'Open', exact: true })).toBeVisible();
    expect(await rail.locator('[data-rail-item]').evaluateAll(rows => rows.map(el => el.getAttribute('data-rail-item'))))
      .toEqual(['now', ...['codeaf', 'Personal', 'Config parser', 'Marketing', 'Q3 report'].map(name => engine.id(name)), 'all']);
    await expect(rail.locator('.rail-row-main.rail-place')).toHaveCount(5);
    await expect(rail.getByRole('button', { name: /^All places(?: Ctrl Shift P| ⌘⇧P)?$/ })).toBeVisible();
    await tabTo(page, rail.getByRole('button', { name: 'Now', exact: true }));
    await page.keyboard.press('ArrowDown');
    const pinned = rail.locator(`.rail-row-main[data-rail-item="${engine.id('codeaf')}"]`);
    await expect(pinned).toBeFocused();
    await expect(pinned).toHaveAccessibleName(/^codeaf/);
    expect(await pinned.evaluate(el => el.matches(':focus-visible'))).toBe(true);
    await expect(pinned).toHaveCSS('box-shadow', /2px.*4px/);
    await page.keyboard.press('End');
    await expect(rail.getByRole('button', { name: /^All places(?: Ctrl Shift P| ⌘⇧P)?$/ })).toBeFocused();
    expect(engine.posts('/places/rail').filter(call => call.body.op === 'visit')).toHaveLength(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  });

  test(`PL-197: Reports has 47 children in a virtualized keyboard palette · ${theme} ${width}px`, async ({ page }) => {
    const engine = await boot(page, width, theme);
    const composer = page.getByRole('textbox', { name: 'Message', exact: true });
    await tabTo(page, composer);
    const primary = await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control';
    await page.keyboard.press(`${primary}+KeyP`);
    const sheet = page.getByRole('dialog', { name: 'Go to a place, or create one', exact: true });
    const field = sheet.getByRole('combobox', { name: 'Go to a place, or create one', exact: true });
    await expect(sheet).toBeVisible();
    await expect(field).toBeFocused();
    expect(await field.evaluate(el => el.matches(':focus-visible'))).toBe(true);
    await expect(field).toHaveAttribute('placeholder', 'Go to a place, or create one');
    await expect(field).toHaveCSS('font-size', '15px');
    await expect(sheet.locator('.goto-count')).toHaveText('200 places');
    await expect(sheet.getByRole('group', { name: 'Recent', exact: true })).toBeVisible();
    const all = sheet.getByRole('group', { name: 'All', exact: true });
    // Soft assertions preserve the remaining keyboard evidence when the owning implementation lane has a gap.
    expect.soft(await all.getByRole('option').count(), 'The All tree must window its rows rather than mount the full expanded graph.').toBeLessThan(47);
    const bounds = await sheet.evaluate(el => el.getBoundingClientRect().toJSON());
    expect(bounds.left).toBeGreaterThanOrEqual(0);
    expect(bounds.right).toBeLessThanOrEqual(width);
    expect(bounds.bottom).toBeLessThanOrEqual(800);
    await expect(sheet.locator('.goto-hints')).toContainText('open in new window');
    await expect(sheet.locator('.goto-hints')).toContainText('new place');

    // The parent ranks first; all 47 children match their ancestor, including Churn deep-dive.
    await field.pressSequentially('Reports');
    const results = sheet.getByRole('group', { name: 'Matching places', exact: true });
    await expect(results.getByRole('option').first()).toHaveAccessibleName('Reports, 47 inside');
    await expect(results.getByRole('option').first()).toHaveCSS('height', '34px');
    expect.soft(await results.getByRole('option').count(), 'Search results must also window the 48 matching places.').toBeLessThan(48);
    const seen = new Set<string>();
    for (let index = 0; index < 48; index++) {
      const selected = sheet.getByRole('option', { selected: true });
      await expect(selected).toHaveCount(1);
      await expect(field).toHaveAttribute('aria-activedescendant', (await selected.getAttribute('id'))!);
      seen.add((await selected.getAttribute('aria-label'))!);
      const visible = await selected.evaluate(el => {
        const row = el.getBoundingClientRect(), group = el.closest('[role="group"]')!.getBoundingClientRect();
        return row.top >= group.top - 1 && row.bottom <= group.bottom + 1;
      });
      expect(visible, 'Keyboard selection must scroll into the list viewport.').toBe(true);
      await field.press('ArrowDown');
    }
    expect(seen.size).toBe(48);
    expect([...seen]).toContain('Churn deep-dive, in Reports');
    expect([...seen]).toContain('Report 46, in Reports');
    expect(engine.posts('/places/rail').filter(call => call.body.op === 'visit')).toHaveLength(0);
    await field.press('Escape');
    await expect(sheet).toHaveCount(0);
    await expect(composer).toBeFocused();

    await page.keyboard.press(`${primary}+KeyP`);
    await field.pressSequentially('Report 46');
    await expect(sheet.getByRole('option', { selected: true })).toHaveAccessibleName('Report 46, in Reports');
    await field.press('Enter');
    await expect(sheet).toHaveCount(0);
    await expect.poll(() => engine.posts('/visit').some(call => call.path === `/places/${engine.id('Report 46')}/visit`)).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
  });
}
