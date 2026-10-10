import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, type PlacesSeed } from './support/mock-places';

// Places 6a / 9a / 9d: the frame wears the window place's tint. Now and All places are graphite.
// The swap is the attribute, measured on the next read, with no transition on the frame.

const garden = (): PlacesSeed => ({
  places: [
    { name: 'Marketing', tint: 'rose', pinned: true },
    { name: 'Software', tint: 'iris', lastOpenedAt: 'now' },
    { name: 'Config parser', parents: ['Software'], lastOpenedAt: 'now' },
  ],
});

const frameOf = { light: { l: 0.93, c: 0.035 }, dark: { l: 0.27, c: 0.035 } };
const hueOf = { graphite: 250, rose: 12, iris: 285 };
const chromaOf = { graphite: 0.012, rose: 0.13, iris: 0.14 };

async function boot(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await installMockPlaces(page, garden());
  await page.goto('/');
  await expect(page.locator('.app-shell .place-rail').getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}

async function frame(page: Page) {
  return page.locator('.app-shell').evaluate(el => {
    const shell = getComputedStyle(el);
    const body = getComputedStyle(document.body);
    return {
      tint: document.body.dataset.tint ?? '',
      h: body.getPropertyValue('--h').trim(),
      a: body.getPropertyValue('--a').trim(),
      background: shell.backgroundColor,
      transition: shell.transitionProperty,
      animations: el.getAnimations().length,
    };
  });
}

function oklch(color: string) {
  const match = /oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\)/.exec(color);
  expect(match, color).toBeTruthy();
  return { l: Number(match![1]), c: Number(match![2]), h: Number(match![3]) };
}

async function expectTint(page: Page, theme: 'light' | 'dark', tint: keyof typeof hueOf) {
  const measured = await frame(page);
  expect(measured.tint).toBe(tint);
  expect(Number(measured.h)).toBeCloseTo(hueOf[tint], 0);
  expect(Number(measured.a)).toBeCloseTo(chromaOf[tint], 3);
  const paint = oklch(measured.background);
  expect(paint.l).toBeCloseTo(frameOf[theme].l, 2);
  expect(paint.c).toBeCloseTo(frameOf[theme].c, 3);
  expect(paint.h).toBeCloseTo(hueOf[tint], 0);
  for (const part of measured.transition.split(',').map(item => item.trim())) {
    expect(part === 'background' || part === 'background-color' || part.startsWith('--')).toBe(false);
  }
  expect(measured.animations).toBe(0);
}

for (const theme of ['light', 'dark'] as const) {
  test(`PL-023 frame tint swaps instantly in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: theme === 'dark' ? 'reduce' : 'no-preference' });
    await boot(page, theme);
    await expectTint(page, theme, 'graphite');

    await page.locator('.app-shell .place-rail').getByRole('button', { name: /^Marketing/ }).click();
    await expectTint(page, theme, 'rose');

    await page.locator('.app-shell .place-rail').getByRole('button', { name: /^Config parser/ }).click();
    await expectTint(page, theme, 'iris');

    await page.locator('.app-shell .place-rail').getByRole('button', { name: 'All places' }).click();
    await expect(page.getByRole('tab', { name: 'All places' })).toHaveAttribute('aria-selected', 'true');
    // All places opened from a place is a tab. The window place is still that place, so the frame stays.
    await expectTint(page, theme, 'iris');

    await page.locator('.app-shell .place-rail').getByRole('button', { name: 'Now', exact: true }).click();
    await expectTint(page, theme, 'graphite');
  });
}
