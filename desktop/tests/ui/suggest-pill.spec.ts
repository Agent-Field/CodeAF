import { test, expect, type Page } from '@playwright/test';

// Shell 2b suggestion pill, measured on its own page so the shell's other cards are not in the way.
const open = (page: Page, query = '') => page.goto(`/?specimen=suggest-pill${query}`);
const pill = (page: Page) => page.getByRole('status');
const card = (page: Page) => page.locator('.suggest-pill-card');

async function paint(page: Page, property: 'backgroundColor' | 'color' | 'boxShadow', name: string) {
  return page.evaluate(({ property, name }) => {
    const probe = document.createElement('span');
    probe.style[property] = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe)[property];
    probe.remove();
    return value;
  }, { property, name });
}

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

test('the pill is the measured surface: layers, the folder sentence, accent Group, Dismiss', async ({ page }) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await open(page);
    const offer = pill(page);
    await expect(offer.locator('.suggest-pill-text')).toHaveText('Group the 3 bench tabs as bench?');
    await expect(offer.locator('b')).toHaveText('bench');
    await expect(offer.locator('[data-icon="layers"]')).toHaveCount(1);
    await expect(offer).toHaveCSS('height', '34px');
    await expect(offer).toHaveCSS('border-top-left-radius', '99px');
    await expect(offer).toHaveCSS('padding', '0px 6px 0px 14px');
    await expect(offer).toHaveCSS('gap', '10px');
    await expect(offer).toHaveCSS('font-size', '12px');
    await expect(offer).toHaveCSS('background-color', await paint(page, 'backgroundColor', 'surface'));
    await expect(offer).toHaveCSS('color', await paint(page, 'color', 'ink'));
    await expect(offer).toHaveCSS('box-shadow', await paint(page, 'boxShadow', 'sh-2'));
    await expect(offer.locator('.suggest-pill-mark')).toHaveCSS('color', await paint(page, 'color', 'ink-2'));
    await expect(offer.locator('b')).toHaveCSS('font-weight', '500');
    const group = offer.getByRole('button', { name: 'Group', exact: true });
    await expect(group).toHaveCSS('height', '24px');
    await expect(group).toHaveCSS('padding', '0px 10px');
    await expect(group).toHaveCSS('border-top-left-radius', '99px');
    await expect(group).toHaveCSS('background-color', await paint(page, 'backgroundColor', 'accent'));
    await expect(group).toHaveCSS('color', await paint(page, 'color', 'accent-ink'));
    const dismiss = offer.getByRole('button', { name: 'Dismiss', exact: true });
    await expect(dismiss).toHaveCSS('width', '22px');
    await expect(dismiss).toHaveCSS('height', '22px');
    await expect(dismiss).toHaveCSS('color', await paint(page, 'color', 'ink-3'));
    await expect(dismiss.locator('[data-icon="close"]')).toHaveCSS('width', '11px');
    const layers = await offer.locator('[data-icon="layers"]').boundingBox();
    expect([layers?.width, layers?.height]).toEqual([13, 13]);
    const frame = (await card(page).boundingBox())!;
    const box = (await offer.boundingBox())!;
    expect(Math.abs(frame.x + frame.width / 2 - (box.x + box.width / 2))).toBeLessThan(1);
    expect(Math.abs(box.y - frame.y - 10)).toBeLessThan(1);
  }
});

test('Group dispatches the suggestion, and Dismiss leaves the tabs loose', async ({ page }) => {
  await open(page);
  await pill(page).getByRole('button', { name: 'Group', exact: true }).click();
  await expect(pill(page)).toHaveCount(0);
  await expect(card(page)).toHaveAttribute('data-group-title', 'bench');
  await expect(card(page)).toHaveAttribute('data-grouped-count', '3');

  await open(page);
  await pill(page).getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(pill(page)).toHaveCount(0);
  await expect(card(page)).toHaveAttribute('data-grouped-count', '0');
});

test('Group and Dismiss are the tab stops, and keyboard focus draws the accent ring', async ({ page }) => {
  await open(page);
  await page.keyboard.press('Tab');
  const group = pill(page).getByRole('button', { name: 'Group', exact: true });
  await expect(group).toBeFocused();
  const ring = await page.evaluate(() => {
    const probe = document.createElement('span');
    probe.style.boxShadow = '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)';
    document.body.append(probe);
    const value = getComputedStyle(probe).boxShadow;
    probe.remove();
    return value;
  });
  await expect(group).toHaveCSS('box-shadow', ring);
  await page.keyboard.press('Tab');
  await expect(pill(page).getByRole('button', { name: 'Dismiss', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(pill(page)).toHaveCount(0);
  await expect(card(page)).toHaveAttribute('data-grouped-count', '0');
});

test('fewer than three tabs draws nothing', async ({ page }) => {
  await open(page, '&count=2');
  await expect(pill(page)).toHaveCount(0);
  await expect(page.locator('.suggest-pill')).toHaveCount(0);
});

test('at 600px the pill wraps inside the card width minus 16px', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 700 });
  await open(page, '&folder=nightly-benchmark-workspace-folder');
  const offer = pill(page);
  await expect(offer.locator('.suggest-pill-text')).toHaveCSS('overflow-wrap', 'anywhere');
  const frame = (await card(page).boundingBox())!;
  const box = (await offer.boundingBox())!;
  expect(box.width).toBeLessThanOrEqual(frame.width - 16 + 0.5);
  expect(box.x).toBeGreaterThanOrEqual(frame.x - 0.5);
  expect(box.x + box.width).toBeLessThanOrEqual(frame.x + frame.width + 0.5);
  expect(box.height).toBeGreaterThan(34);
  await expect(offer.getByRole('button', { name: 'Group', exact: true })).toBeVisible();
  await expect(offer.getByRole('button', { name: 'Dismiss', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
