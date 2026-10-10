import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';

// PL-190 PL-194 PL-196 PL-199 PL-210 PL-213 PL-261: ⌘P Go to, its pick mode (Add to another place…), Quick Look and the first-place journey.
// Everything is driven from the keyboard against the real shell; the pointer variants live in places-shell.spec.ts.

const WIDTHS = [320, 600, 850, 1200] as const;
const THEMES = ['light', 'dark'] as const;
const fresh = (): Scenario => ({ initial: { entries: [], title: '', sessionFile: sessionFileFor('mock-1') }, turns: [] });

/** Marketing is pinned, Software has a child, and Launch copy is filed in Marketing. */
const garden = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true, lastOpenedAt: 'now' },
    { name: 'Software', tint: 'iris' },
    { name: 'Config parser', parents: ['Software'] },
  ],
  chats: [{ id: 'c-launch', title: 'Launch copy', places: ['Marketing'] }],
  live: ['mock-1'],
});

async function boot(page: Page, width: number, theme: (typeof THEMES)[number], seed: PlacesSeed = garden()) {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.setViewportSize({ width, height: 800 });
  await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, seed);
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  return { places, primary: mac ? 'Meta' : 'Control' };
}

const chooser = (page: Page) => page.locator('dialog.goto-chooser[open]');
const field = (page: Page) => chooser(page).getByRole('combobox');
const tile = (page: Page, name: string) => page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: new RegExp(`^${name}$`) }) });
const tabs = (page: Page) => page.locator('.workspace-tabstrip').getByRole('tab');

/** Layout size of the dialog, not its box: the sheet scales in on open and a bounding box would be read mid-animation. */
const layout = (page: Page, selector: string) => page.locator(selector).first().evaluate(el => ({ w: (el as HTMLElement).offsetWidth, h: (el as HTMLElement).offsetHeight }));
const backdrop = (page: Page, selector: string) => page.locator(selector).first().evaluate(el => getComputedStyle(el, '::backdrop').backgroundColor.replace(/\s/g, ''));
/** The alpha written in a backdrop colour, whichever syntax the engine serialises it in. */
const alphaOf = (color: string) => Number(/\/([\d.]+)\)$|,([\d.]+)\)$/.exec(color)?.slice(1).find(Boolean));

for (const theme of THEMES) for (const width of WIDTHS) {
  test.describe(`${theme} ${width}px`, () => {
    test(`PL-190 PL-196 ⌘P opens Go to over the place: name, field, footer, selection fill, Esc gives focus back`, async ({ page }) => {
      const { primary } = await boot(page, width, theme);
      const composer = page.getByRole('textbox', { name: 'Message', exact: true });
      await composer.focus();
      await page.keyboard.press(`${primary}+KeyP`);
      await expect(page.getByRole('dialog', { name: 'Go to a place, or create one' })).toBeVisible();
      await expect(field(page)).toBeFocused();
      await expect(field(page)).toHaveAttribute('placeholder', 'Go to a place, or create one');
      await expect(field(page)).toHaveAccessibleName('Go to a place, or create one');
      await expect(chooser(page).locator('.goto-count')).toHaveText('3 places');
      await expect(chooser(page).getByRole('group', { name: 'Recent' })).toBeVisible();
      await expect(chooser(page).getByRole('group', { name: 'All' })).toBeVisible();
      await expect(chooser(page).locator('.goto-hints')).toContainText('open in new window');
      await expect(chooser(page).locator('.goto-hints')).toContainText('new place');
      await page.waitForTimeout(300);
      // 620×560 on a wide window; narrower windows hold the same sheet inside the gutters, never wider than the window.
      const size = await layout(page, 'dialog.goto-chooser');
      if (width >= 700) expect(size).toEqual({ w: 620, h: 560 });
      else expect(size.w).toBeLessThanOrEqual(width);
      expect(alphaOf(await backdrop(page, 'dialog.goto-chooser'))).toBeCloseTo(theme === 'light' ? 0.18 : 0.4, 2);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
      await expectAccessible(page, 'dialog.goto-chooser');

      // ↑↓ rove with selection as the soft fill; the field keeps focus and names the active row.
      await field(page).press('ArrowDown');
      const selected = chooser(page).getByRole('option', { selected: true });
      await expect(selected).toHaveCount(1);
      await expect(field(page)).toHaveAttribute('aria-activedescendant', (await selected.getAttribute('id'))!);
      const fills = await selected.evaluate(el => {
        const probe = document.createElement('span');
        probe.style.background = 'var(--field)';
        document.body.append(probe);
        const expected = getComputedStyle(probe).backgroundColor;
        probe.remove();
        return { expected, border: getComputedStyle(el).borderLeftWidth };
      });
      expect(fills.border).toBe('0px');
      // The row fades to its fill over 120ms, so the colour is polled rather than read mid-transition.
      await expect.poll(() => selected.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(fills.expected);

      await field(page).press('Escape');
      await expect(chooser(page)).toHaveCount(0);
      await expect(composer).toBeFocused();
    });

    test(`PL-194 ↵ opens the place the name ranks first`, async ({ page }) => {
      const { primary } = await boot(page, width, theme);
      await page.keyboard.press(`${primary}+KeyP`);
      // A name match ranks first; Config parser also matches through its ancestor path and comes second.
      await field(page).fill('soft');
      await expect(chooser(page).getByRole('option').first()).toHaveAccessibleName(/^Software/);
      await page.keyboard.press('Enter');
      await expect(chooser(page)).toHaveCount(0);
      await expect(page.locator('.workspace-tabstrip').getByRole('tab', { selected: true })).toHaveAccessibleName('Software');
    });

    test(`PL-194 ⌘↵ goes to the place where there is no second window`, async ({ page }) => {
      const { primary } = await boot(page, width, theme);
      await page.keyboard.press(`${primary}+KeyP`);
      await field(page).fill('marketing');
      await page.keyboard.press(`${primary}+Enter`);
      await expect(chooser(page)).toHaveCount(0);
      await expect(page.locator('.workspace-tabstrip').getByRole('tab', { selected: true })).toHaveAccessibleName('Marketing');
    });

    test(`PL-194 PL-195 ⌘N creates a place under the typed name; the Create row shows when nothing matches`, async ({ page }) => {
      const { places, primary } = await boot(page, width, theme);
      await page.keyboard.press(`${primary}+KeyP`);
      await field(page).fill('Zebra');
      await expect(chooser(page).getByRole('option')).toHaveCount(0);
      await page.keyboard.press(`${primary}+KeyN`);
      await expect(chooser(page)).toHaveCount(0);
      await expect(page.locator('.workspace-tabstrip').getByRole('tab', { selected: true })).toHaveAccessibleName('Zebra');
      expect(places.state().places.map(place => place.name)).toContain('Zebra');
    });

    test(`PL-199 the tree opens and closes by ← and →, and the sheet never overflows the window`, async ({ page }) => {
      const { primary } = await boot(page, width, theme);
      await page.keyboard.press(`${primary}+KeyP`);
      const all = chooser(page).getByRole('group', { name: 'All' });
      const before = await all.getByRole('option').count();
      // Top-level branches start open, so Config parser is drawn under Software until ← closes the branch.
      await expect(all.getByRole('option', { name: /^Config parser/ })).toHaveCount(1);
      await field(page).press('ArrowDown');
      const selectedName = async () => (await chooser(page).getByRole('option', { selected: true }).getAttribute('aria-label')) ?? '';
      for (let step = 0; step < 6 && !(await selectedName()).startsWith('Software, '); step += 1) await field(page).press('ArrowDown');
      expect(await selectedName()).toMatch(/^Software, /);
      await field(page).press('ArrowLeft');
      await expect(all.getByRole('option')).toHaveCount(before - 1);
      await field(page).press('ArrowRight');
      await expect(all.getByRole('option')).toHaveCount(before);
      const box = await chooser(page).evaluate(el => el.getBoundingClientRect());
      expect(box.left).toBeGreaterThanOrEqual(0);
      expect(box.right).toBeLessThanOrEqual(width);
    });

    test(`PL-198 pick mode (Add to another place…) asks its question, drops the place itself and hides the new-window hint`, async ({ page }) => {
      const { places, primary } = await boot(page, width, theme);
      await page.keyboard.press(`${primary}+Shift+KeyP`);
      await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
      await tile(page, 'Software').locator('.places-tile-main').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Add to another place…' }).click();
      const sheet = page.getByRole('dialog', { name: 'Add “Software” to another place…' });
      await expect(sheet).toBeVisible();
      await expect(sheet.getByRole('combobox')).toBeFocused();
      await expect(sheet.getByRole('option', { name: /^Software/ })).toHaveCount(0);
      await expect(sheet.locator('.goto-hints')).not.toContainText('open in new window');
      await expect(sheet.locator('.goto-hints')).toContainText('add');
      await sheet.getByRole('combobox').fill('Marketing');
      await page.keyboard.press('Enter');
      await expect(sheet).toHaveCount(0);
      await expect.poll(() => places.posts('/parents').length).toBe(1);
    });
  });
}

for (const theme of THEMES) for (const width of WIDTHS) {
  test(`PL-210 PL-213 Quick Look · ${theme} ${width}px: Space opens a read-only Home, Tab stays inside, Space closes and gives focus back`, async ({ page }) => {
    const { primary } = await boot(page, width, theme);
    await page.keyboard.press(`${primary}+Shift+KeyP`);
    await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
    const button = tile(page, 'Marketing').locator('.places-tile-main');
    await button.focus();
    const tabsBefore = await tabs(page).allInnerTexts();
    const tint = await page.locator('body').getAttribute('data-tint');
    await page.keyboard.press('Space');
    const sheet = page.getByRole('dialog', { name: 'Quick Look: Marketing' });
    await expect(sheet).toBeVisible();
    await page.waitForTimeout(300);

    const size = await layout(page, 'dialog.home-quicklook');
    // Up to 600px the sheet fills the window (PL-214); wider, it is 520.
    expect(size.w).toBe(width <= 600 ? width : 520);
    const look = await sheet.evaluate(el => {
      const style = getComputedStyle(el);
      const probe = document.createElement('span');
      probe.style.boxShadow = 'var(--sh-3)';
      document.body.append(probe);
      const shadow = getComputedStyle(probe).boxShadow;
      probe.remove();
      return { radius: style.borderTopLeftRadius, shadow: style.boxShadow, expectedShadow: shadow };
    });
    expect(look.shadow).toBe(look.expectedShadow);
    if (width > 600) expect(look.radius).toBe('14px');
    expect(alphaOf(await backdrop(page, 'dialog.home-quicklook'))).toBeCloseTo(theme === 'light' ? 0.2 : 0.4, 2);

    await expect(sheet.locator('.home-quicklook-title')).toHaveText('Marketing');
    await expect(sheet.locator('.home-quicklook-hint')).toHaveText('Space to close');
    await expect(sheet).toContainText('Launch copy');
    await expect(sheet.getByRole('button', { name: 'Go to Marketing' })).toBeVisible();
    await expect(sheet.getByRole('button', { name: 'Open in new window' })).toBeVisible();
    await expectAccessible(page, 'dialog.home-quicklook');

    // Focus is trapped: Tab walks the two footer actions and wraps; Shift+Tab wraps the other way.
    const goTo = sheet.getByRole('button', { name: 'Go to Marketing' });
    const newWindow = sheet.getByRole('button', { name: 'Open in new window' });
    await goTo.focus();
    await page.keyboard.press('Tab');
    await expect(newWindow).toBeFocused();
    await expect(newWindow).toHaveCSS('outline-style', 'solid');
    await page.keyboard.press('Tab');
    await expect(goTo).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(newWindow).toBeFocused();

    await page.keyboard.press('Space');
    await expect(sheet).toHaveCount(0);
    await expect(button).toBeFocused();
    expect(await tabs(page).allInnerTexts()).toEqual(tabsBefore);
    expect(await page.locator('body').getAttribute('data-tint')).toBe(tint);

    // Esc closes it the same way.
    // Quick Look reads the place's Home before it draws, so under load the first Space can land before the tile is armed again.
    await expect(async () => {
      if (!await sheet.isVisible()) { await button.focus(); await page.keyboard.press('Space'); }
      await expect(sheet).toBeVisible({ timeout: 2000 });
    }).toPass();
    await page.keyboard.press('Escape');
    await expect(sheet).toHaveCount(0);
    await expect(button).toBeFocused();
  });
}

test('PL-210 Space on a rail row opens Quick Look for that place without opening it', async ({ page }) => {
  const { places } = await boot(page, 1200, 'light');
  const row = page.locator('.app-shell .place-rail').first().locator('.rail-place', { hasText: 'Marketing' }).first();
  await row.focus();
  const visits = places.posts('/visit').length;
  await page.keyboard.press('Space');
  const sheet = page.getByRole('dialog', { name: 'Quick Look: Marketing' });
  await expect(sheet).toBeVisible();
  await page.keyboard.press('Space');
  await expect(sheet).toHaveCount(0);
  await expect(row).toBeFocused();
  expect(places.posts('/visit').length).toBe(visits);
});

for (const theme of THEMES) {
  test(`PL-261 journey · ${theme}: first place via ⌘P, type a name, ⌘N`, async ({ page }) => {
    const { places, primary } = await boot(page, 1200, theme, { places: [], live: ['mock-1'] });
    await page.keyboard.press(`${primary}+KeyP`);
    await expect(page.getByRole('dialog', { name: 'Go to a place, or create one' })).toBeVisible();
    // With nothing yet the count and the lists draw nothing (the emptiness law); the field still works.
    await expect(chooser(page).locator('.goto-count')).toHaveCount(0);
    await expect(field(page)).toBeFocused();
    await field(page).pressSequentially('Garden');
    await expect(chooser(page).getByRole('option')).toHaveCount(0);
    await page.keyboard.press(`${primary}+KeyN`);
    await expect(chooser(page)).toHaveCount(0);
    await expect(tabs(page).first()).toHaveAccessibleName('Garden');
    await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
    expect(places.state().places.map(place => place.name)).toEqual(['Garden']);
  });
}
