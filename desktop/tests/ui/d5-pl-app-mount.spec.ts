import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, type PlacesSeed } from './support/mock-places';
import { openPage } from './support/shell-navigation';

// PL-023: the app shell itself wears the place tint. The Design system page shows the Places specimen (fixtures only).

const garden = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true },
    { name: 'Software', tint: 'iris', lastOpenedAt: 'now' },
  ],
});

const frameOf = { light: { l: 0.93, c: 0.035 }, dark: { l: 0.27, c: 0.035 } };
const hueOf = { graphite: 250, rose: 12 };
const chromaOf = { graphite: 0.012, rose: 0.13 };

async function boot(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await installMockPlaces(page, garden());
  await page.goto('/');
  await expect(page.locator('.app-shell .place-rail').getByRole('button', { name: 'Now', exact: true })).toBeVisible();
}

async function shellTint(page: Page) {
  return page.locator('.app-shell').evaluate(el => {
    const shell = getComputedStyle(el);
    // Glass leaves the shell element transparent and paints the tint on the rail and the strip.
    // The token on the shell is what those surfaces resolve.
    const probe = document.createElement('span');
    probe.style.background = shell.getPropertyValue('--frame');
    el.append(probe);
    const frame = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return {
      tint: el.getAttribute('data-tint') ?? '',
      h: shell.getPropertyValue('--h').trim(),
      a: shell.getPropertyValue('--a').trim(),
      frame,
    };
  });
}

async function expectShellTint(page: Page, theme: 'light' | 'dark', tint: keyof typeof hueOf) {
  const measured = await shellTint(page);
  expect(measured.tint).toBe(tint);
  expect(Number(measured.h)).toBeCloseTo(hueOf[tint], 0);
  expect(Number(measured.a)).toBeCloseTo(chromaOf[tint], 3);
  const expected = await page.evaluate(({ l, c, h }) => {
    const probe = document.createElement('span');
    probe.style.background = `oklch(${l} ${c} ${h})`;
    document.body.append(probe);
    const color = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return color;
  }, { ...frameOf[theme], h: hueOf[tint] });
  await expect.poll(async () => (await shellTint(page)).frame).toBe(expected);
}

for (const theme of ['light', 'dark'] as const) {
  test(`PL-023 shell tint and the Places specimen · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await boot(page, theme);
    await expectShellTint(page, theme, 'graphite');

    await page.locator('.app-shell .place-rail').getByRole('button', { name: /^Marketing/ }).click();
    await expectShellTint(page, theme, 'rose');

    const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
    await page.keyboard.press(`${mac ? 'Meta' : 'Control'}+KeyP`);
    const chooser = page.getByRole('dialog', { name: 'Go to a place, or create one' });
    await expect(chooser).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(chooser).toBeHidden();

    await page.locator('.app-shell .place-rail').getByRole('button', { name: /^Marketing/ }).focus();
    await page.keyboard.press('Space');
    const look = page.getByRole('dialog', { name: 'Quick Look: Marketing' });
    await expect(look).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(look).toBeHidden();

    await openPage(page, 'Design system');
    const specimen = page.getByTestId('places-rail-states');
    await specimen.scrollIntoViewIfNeeded();
    await expect(page.locator('h2.type-section', { hasText: 'Places' })).toBeVisible();
    await expect(page.getByText('Specimen. Tints, tiles, rows and cards in every state; the fixtures are never a person\'s places.')).toBeVisible();
    const row = specimen.locator('.rail-row-main').first();
    const box = await row.evaluate(el => {
      const style = getComputedStyle(el);
      const bounds = el.getBoundingClientRect();
      return { height: bounds.height, fontSize: style.fontSize };
    });
    expect(box.height).toBe(32);
    expect(box.fontSize).toBe('13px');
    // The window is still Marketing. The specimen does not swap the frame.
    await expectShellTint(page, theme, 'rose');
  });
}
