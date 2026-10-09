import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, type MockPlaces, type PlacesSeed } from './support/mock-places';

// The untouched-place suggestion (design 6d "Too many places", Interactions "Not now hides it for 30 days"): a place nobody
// has touched for 60 days gets one quiet line with Merge, Archive and Not now, on All places and in ⌘P. The engine's rule
// (internal/placegraph/stale.go) is proved in Go to the nanosecond; these journeys drive the real controls and prove what a
// person sees and what is sent. Merge and Archive are the same writes the place menu makes, with the same Undo.

/** Two places that are due, the oldest first; one a day short of due; one pinned; one with work running. */
const crowded = (): PlacesSeed => ({
  places: [
    { name: 'Launch week', tint: 'rose', untouchedDays: 75 },
    { name: 'Old notes', tint: 'sand', untouchedDays: 61 },
    { name: 'Almost due', tint: 'sage', untouchedDays: 59 },
    { name: 'On the rail', tint: 'iris', untouchedDays: 200, pinned: true },
    { name: 'Still working', tint: 'tide', untouchedDays: 120 },
  ],
  chats: [{ id: 'sess-run', title: 'Long build', places: ['Still working'], live: true, doing: 'working', tasks: { running: 1 }, at: '2026-01-01T00:00:00Z' }],
});

async function boot(page: Page, seed: PlacesSeed = crowded()): Promise<MockPlaces> {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, seed);
  await page.goto('/');
  await expect(page.locator('.workspace-tabstrip')).toBeVisible();
  return places;
}

/** ⌘⇧P / Ctrl⇧P: the same door at every width, since the rail is a drawer when the window is narrow. */
const allPlaces = async (page: Page) => {
  await page.keyboard.press(`${await modifier(page)}+Shift+KeyP`);
  await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
};
const line = (page: Page) => page.getByRole('group', { name: 'Suggestion' });
const chooser = (page: Page) => page.locator('dialog.goto-chooser[open]');
const toast = (page: Page) => page.locator('.toast-region .toast');
const launch = '“Launch week” hasn’t been touched in 75 days';
const notes = '“Old notes” hasn’t been touched in 61 days';
const modifier = async (page: Page) => ((await page.evaluate(() => /Mac/.test(navigator.platform))) ? 'Meta' : 'Control');

test.describe('All places', () => {
  test('offers only the oldest due place, with the three verbs in the design’s order', async ({ page }) => {
    await boot(page);
    await allPlaces(page);
    await expect(line(page)).toHaveCount(1);
    await expect(line(page)).toContainText(launch);
    await expect(line(page).getByRole('button')).toHaveText(['Merge', 'Archive', 'Not now']);
    // 59 days, pinned, and work running are not due; nothing says so on the page.
    await expect(page.getByText('hasn’t been touched')).toHaveCount(1);
    await expectAccessible(page);
  });

  test('Not now hides that place in every window, survives a reload, and shows the next one', async ({ page }) => {
    const places = await boot(page);
    await allPlaces(page);
    const revision = places.state().revision;
    await line(page).getByRole('button', { name: 'Not now' }).click();
    await expect(line(page)).toContainText(notes);
    expect(places.posts('/places/' + places.id('Launch week') + '/stale-snooze')).toHaveLength(1);
    expect(Object.keys(places.snoozes())).toEqual([places.id('Launch week')]);
    // A snooze is not a change to the person’s places: no revision, no receipt, no Undo toast.
    expect(places.state().revision).toBe(revision);
    await expect(toast(page)).toHaveCount(0);

    await page.reload();
    await expect(page.locator('.workspace-tabstrip')).toBeVisible();
    await allPlaces(page);
    await expect(line(page)).toContainText(notes);
    await expect(line(page)).not.toContainText('Launch week');

    await line(page).getByRole('button', { name: 'Not now' }).click();
    await expect(line(page)).toHaveCount(0);
  });

  test('Archive is the place menu’s archive, with its toast and Undo, and the line follows', async ({ page }) => {
    const places = await boot(page);
    await allPlaces(page);
    await line(page).getByRole('button', { name: 'Archive' }).click();
    await expect(toast(page)).toContainText('Archived “Launch week”');
    expect(places.posts('/archive')).toHaveLength(1);
    expect(places.state().places.find(p => p.name === 'Launch week')?.archived).toBe(true);
    await expect(line(page)).toContainText(notes);
    await toast(page).getByRole('button', { name: 'Undo' }).click();
    await expect(line(page)).toContainText(launch);
    expect(places.state().places.find(p => p.name === 'Launch week')?.archived).toBe(false);
  });

  test('Merge opens the merge chooser for that place and merging removes it', async ({ page }) => {
    const places = await boot(page);
    await allPlaces(page);
    await line(page).getByRole('button', { name: 'Merge' }).click();
    await expect(chooser(page)).toBeVisible();
    await expect(chooser(page).getByRole('combobox')).toHaveAttribute('aria-label', 'Merge “Launch week” into…');
    // The place itself is never offered as its own target.
    await expect(chooser(page).getByRole('option', { name: /^Launch week/ })).toHaveCount(0);
    await chooser(page).getByRole('combobox').fill('Almost due');
    await page.keyboard.press('Enter');
    await expect(chooser(page)).toHaveCount(0);
    expect(places.posts('/merge')[0].body).toMatchObject({ into: places.id('Almost due') });
    expect(places.state().places.some(p => p.name === 'Launch week')).toBe(false);
    await expect(line(page)).toContainText(notes);
  });

  test('a place with nothing due draws no line, and a bridge without the route draws none and no error', async ({ page }) => {
    const places = await boot(page, { places: [{ name: 'Fresh', untouchedDays: 3 }] });
    await allPlaces(page);
    await expect(line(page)).toHaveCount(0);
    places.fail('stale', { status: 404, error: 'unknown engine action' });
    await page.reload();
    await allPlaces(page);
    await expect(line(page)).toHaveCount(0);
    await expect(page.locator('.home-notice, .home-banner')).toHaveCount(0);
  });

  test('a refused Not now is read out and the place stays offered', async ({ page }) => {
    const places = await boot(page);
    await allPlaces(page);
    places.fail('stale-snooze', { status: 404, error: 'That place doesn’t exist any more.', code: 'not_found' });
    await line(page).getByRole('button', { name: 'Not now' }).click();
    await expect(page.getByText('That place doesn’t exist any more.')).toBeVisible();
    await expect(line(page)).toContainText(launch);
  });
});

test.describe('⌘P', () => {
  test('the same line sits under the search, hides while typing, and Not now advances it', async ({ page }) => {
    const places = await boot(page);
    const key = await modifier(page);
    await page.keyboard.press(`${key}+KeyP`);
    await expect(chooser(page)).toBeVisible();
    await expect(line(chooser(page))).toContainText(launch);
    await expectAccessible(page);
    await chooser(page).getByRole('combobox').fill('Old');
    await expect(line(chooser(page))).toHaveCount(0);
    await chooser(page).getByRole('combobox').fill('');
    await line(chooser(page)).getByRole('button', { name: 'Not now' }).click();
    await expect(line(chooser(page))).toContainText(notes);
    expect(Object.keys(places.snoozes())).toEqual([places.id('Launch week')]);
  });

  test('Archive keeps the sheet open on the next place; Merge turns it into the merge question', async ({ page }) => {
    const places = await boot(page);
    const key = await modifier(page);
    await page.keyboard.press(`${key}+KeyP`);
    await line(chooser(page)).getByRole('button', { name: 'Archive' }).click();
    await expect(chooser(page)).toBeVisible();
    await expect(line(chooser(page))).toContainText(notes);
    expect(places.state().places.find(p => p.name === 'Launch week')?.archived).toBe(true);
    await line(chooser(page)).getByRole('button', { name: 'Merge' }).click();
    await expect(chooser(page).getByRole('combobox')).toHaveAttribute('aria-label', 'Merge “Old notes” into…');
    await expect(line(chooser(page))).toHaveCount(0);
  });

  test('going to a place counts as touching it', async ({ page }) => {
    const places = await boot(page);
    const key = await modifier(page);
    await page.keyboard.press(`${key}+KeyP`);
    await chooser(page).getByRole('combobox').fill('Launch week');
    await page.keyboard.press('Enter');
    await expect(chooser(page)).toHaveCount(0);
    await expect.poll(() => places.posts('/visit').length).toBe(1);
    await allPlaces(page);
    await expect(line(page)).toContainText(notes);
    await expect(line(page)).not.toContainText('Launch week');
  });
});

test.describe('narrow windows', () => {
  for (const width of [320, 480, 800]) {
    test(`${width}px: every verb is reachable and the page does not scroll sideways`, async ({ page }) => {
      await page.setViewportSize({ width, height: 700 });
      await boot(page);
      await allPlaces(page);
      await expect(line(page)).toContainText(launch);
      for (const name of ['Merge', 'Archive', 'Not now']) {
        const button = line(page).getByRole('button', { name });
        await button.scrollIntoViewIfNeeded();
        await expect(button).toBeInViewport();
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    });
  }
});

// Review screenshots, only when asked for (STALE_SHOTS=<dir>); they live outside the source tree.
const shots = process.env.STALE_SHOTS;
test.describe('review screenshots', () => {
  test.skip(!shots, 'set STALE_SHOTS to a directory to write screenshots');
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [1200, 320]) {
      test(`${theme} ${width}px`, async ({ page }, info) => {
        await page.emulateMedia({ colorScheme: theme });
        await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
        await page.setViewportSize({ width, height: 800 });
        await boot(page);
        await allPlaces(page);
        await expect(line(page)).toContainText(launch);
        await page.screenshot({ path: `${shots}/all-places-${theme}-${width}-${info.project.name}.png` });
        await page.keyboard.press(`${await modifier(page)}+KeyP`);
        await expect(line(chooser(page))).toContainText(launch);
        await page.screenshot({ path: `${shots}/command-p-${theme}-${width}-${info.project.name}.png` });
      });
    }
  }
});
