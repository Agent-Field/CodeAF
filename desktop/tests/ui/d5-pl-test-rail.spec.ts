import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';

async function mount(page: Page, scene: 'full' | 'empty' | 'zero' | 'archived', theme = 'light', drawer = false) {
  await page.route('**/api/engine/**', route => route.abort());
  await page.emulateMedia({ colorScheme: theme === 'dark' ? 'dark' : 'light' });
  await page.goto('/');
  await page.evaluate(async ({ theme, scene, drawer }) => {
    const { mountRailSections } = await import('/tests/ui/fixtures/rail-sections-mount.ts');
    mountRailSections(theme, scene, drawer);
  }, { theme, scene, drawer });
  await expect(page.getByRole('button', { name: 'Now', exact: true })).toBeVisible();
}

const rail = (page: Page) => page.locator('.place-rail');
const names = (page: Page) => rail(page).locator('[data-rail-item]').evaluateAll(rows => rows.map(row => row.getAttribute('data-rail-item')));

for (const theme of ['light', 'dark'] as const) {
  test(`sections follow 6a / 10a / 8e in ${theme}`, async ({ page }) => {
    await mount(page, 'full', theme);
    await expect(page.getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
    expect(await names(page)).toEqual(['now', 'codeaf', 'config', 'all']);
    const y = async (id: string) => (await rail(page).locator(`[data-rail-item="${id}"]`).boundingBox())!.y;
    expect(await y('now')).toBeLessThan(await y('codeaf'));
    expect(await y('codeaf')).toBeLessThan(await y('config'));
    expect(await y('config')).toBeLessThan(await y('all'));
    const pinned = rail(page).getByRole('region', { name: 'Pinned' });
    const open = rail(page).getByRole('region', { name: 'Open' });
    await expect(pinned).toBeVisible();
    await expect(open).toBeVisible();
    await expect(rail(page).getByText('Places you open show here. Pin the ones you live in.')).toHaveCount(0);
    const label = pinned.locator('.rail-section-label');
    const ink3 = await tokenColor(page, 'ink-3');
    expect(await label.evaluate(el => {
      const s = getComputedStyle(el);
      return [s.fontSize, s.fontWeight, s.paddingTop, s.paddingRight, s.paddingBottom, s.paddingLeft, s.color];
    })).toEqual(['11px', '500', '0px', '8px', '6px', '8px', ink3]);
    const closeAll = open.getByRole('button', { name: 'Close all' });
    await open.hover();
    await expect(closeAll).toBeVisible();
    await expect(rail(page).locator('[data-rail-item="all"] .rail-meta')).toHaveText('⌘⇧P');
    await expect(rail(page).locator('[data-rail-count]')).toHaveText('2');
    const now = rail(page).locator('[data-rail-item="now"]');
    expect((await now.boundingBox())!.height).toBe(32);
    await expect(now).toHaveCSS('font-size', '13px');
  });
}

test('empty rail keeps the sentence and the shortcut; zero places keeps Now and All places only', async ({ page }) => {
  await mount(page, 'empty');
  const hint = rail(page).locator('.rail-hint');
  await expect(hint).toHaveText('Places you open show here. Pin the ones you live in.');
  const hintStyle = await hint.evaluate(el => {
    const s = getComputedStyle(el);
    return { size: s.fontSize, color: s.color, leading: parseFloat(s.lineHeight) / parseFloat(s.fontSize) };
  });
  expect(hintStyle.size).toBe('12px');
  expect(hintStyle.color).toBe(await tokenColor(page, 'ink-3'));
  expect(hintStyle.leading).toBeCloseTo(1.55, 2);
  await expect(rail(page).getByText('Pinned', { exact: true })).toHaveCount(0);
  await expect(rail(page).getByText('Open', { exact: true })).toHaveCount(0);
  await expect(rail(page).locator('[data-rail-item="all"] .rail-meta')).toHaveText('⌘⇧P');

  await mount(page, 'zero');
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  await expect(rail(page).getByRole('button', { name: 'All places' })).toBeVisible();
  await expect(rail(page).locator('.rail-hint')).toHaveCount(0);
  await expect(rail(page).locator('.rail-section-label')).toHaveCount(0);
  await expect(rail(page).locator('[data-rail-item="all"] .rail-meta')).toHaveCount(0);
  await expect(rail(page).locator('[data-rail-count]')).toHaveCount(0);
  expect(await names(page)).toEqual(['now', 'all']);
});

test('archived places are not rows, and arrows move without wrapping', async ({ page }) => {
  await mount(page, 'archived');
  await expect(page.getByText('Old pin')).toHaveCount(0);
  await expect(page.getByText('Old open')).toHaveCount(0);
  await expect(rail(page).locator('.rail-section-label')).toHaveCount(0);

  await mount(page, 'full');
  const now = rail(page).locator('[data-rail-item="now"]');
  await now.focus();
  await page.keyboard.press('ArrowUp');
  await expect(now).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(rail(page).locator('[data-rail-item="codeaf"]')).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(rail(page).locator('[data-rail-item="config"]')).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#rail-log')).toHaveText('go:config');
  await rail(page).locator('[data-rail-item="all"]').focus();
  await page.keyboard.press('ArrowDown');
  await expect(rail(page).locator('[data-rail-item="all"]')).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(rail(page).locator('[data-rail-item="config"]')).toBeFocused();
});

test('clicks follow the shell rows: Now, a place, All places, Close all', async ({ page }) => {
  await mount(page, 'full');
  await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
  await expect(page.locator('#rail-log')).toHaveText('now');
  await rail(page).getByRole('button', { name: 'Now', exact: true }).click({ modifiers: ['Control'] });
  await expect(page.locator('#rail-log')).toHaveText('now-window');
  await rail(page).locator('[data-rail-item="now"]').dispatchEvent('auxclick', { button: 1 });
  await expect(page.locator('#rail-log')).toHaveText('now-window');
  await rail(page).locator('[data-rail-item="codeaf"]').click({ modifiers: ['Control'] });
  await expect(page.locator('#rail-log')).toHaveText('window:codeaf');
  await rail(page).locator('[data-rail-item="codeaf"]').dispatchEvent('auxclick', { button: 1 });
  await expect(page.locator('#rail-log')).toHaveText('window:codeaf');
  await rail(page).locator('[data-rail-item="config"]').click();
  await expect(page.locator('#rail-log')).toHaveText('go:config');
  await rail(page).locator('[data-rail-item="all"]').click({ modifiers: ['Control'] });
  await expect(page.locator('#rail-log')).toHaveText('all-tab');
  await rail(page).locator('[data-rail-item="all"]').click();
  await expect(page.locator('#rail-log')).toHaveText('all');
  await rail(page).getByRole('region', { name: 'Open' }).hover();
  await rail(page).getByRole('button', { name: 'Close all' }).click();
  await expect(page.locator('#rail-log')).toHaveText('close-all');
});

test('the same sections fit the navigation drawer at 600px', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 800 });
  await mount(page, 'full', 'light', true);
  const drawer = page.locator('.sidebar-drawer');
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText('Pinned', { exact: true })).toBeVisible();
  await expect(drawer.getByText('Open', { exact: true })).toBeVisible();
  await expect(drawer.getByRole('button', { name: 'All places' })).toBeVisible();
  const box = await drawer.boundingBox();
  expect(box!.width).toBeLessThanOrEqual(260);
  expect(await drawer.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
