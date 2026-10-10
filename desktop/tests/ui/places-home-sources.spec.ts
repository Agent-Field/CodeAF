import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

async function boot(page: Page, theme = 'light', empty = false) {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, {
    disk: ['/work/project', '/work/rules.md'],
    places: [{ name: 'Garden', lastOpenedAt: 'now', sources: empty ? [] : [
      { kind: 'folder', ref: '/work/project', label: 'Project' },
      { kind: 'file', ref: '/work/rules.md', label: 'rules.md' },
      { kind: 'url', ref: 'https://example.com/guide', label: 'Guide' },
    ] }],
    // A populated Home must also hide an empty Sources section despite having an Add verb.
    chats: empty ? [{ id: 'chat_garden', title: 'Garden notes', places: ['Garden'] }] : [],
  });
  await page.goto('/');
  await page.locator('.app-shell .place-rail .rail-row').filter({ has: page.locator('.rail-place-name', { hasText: /^Garden/ }) }).locator('.rail-place').click();
  await expect(page.locator('.home-page')).toBeVisible();
  return places;
}
const sources = (page: Page) => page.getByRole('region', { name: 'Sources', exact: true });

for (const theme of ['light', 'dark']) {
  test(`${theme}: real folder, file and URL sources remove through receipts and Undo`, async ({ page }) => {
    const places = await boot(page, theme);
    const section = sources(page);
    await expect(section.locator('.home-source-label')).toHaveText(['Project', 'rules.md', 'Guide']);
    await expect(section.locator('.home-source-note')).toHaveText(['folder', 'file', 'url']);
    await expect(page.getByRole('region', { name: 'Empty place' })).toHaveCount(0);
    await expect(section.locator('.row').first()).toHaveCSS('min-height', '30px');
    await expect(section.locator('.home-source-label').first()).toHaveCSS('mask-image', /linear-gradient/);
    const labels = ['Project', 'rules.md', 'Guide'];
    for (const label of labels) {
      const button = section.getByRole('button', { name: `Remove ${label}`, exact: true });
      await button.focus();
      await expect(button).toBeFocused();
      await expect(button.locator('..')).toHaveCSS('opacity', '1');
      await button.press('Enter');
      await expect(section.locator('.home-source-label').filter({ hasText: label })).toHaveCount(0);
    }
    await expect(section).toHaveCount(0);
    expect(places.posts('/sources/remove')).toHaveLength(3);
    await page.locator('.toast').getByRole('button', { name: 'Undo', exact: true }).click();
    await expect(section.locator('.home-source-label')).toHaveText(['Guide']);
    const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
    await page.locator('.workspace-tab.is-place-home').getByRole('tab').focus();
    await page.keyboard.press(`${modifier}+z`);
    await expect(section.locator('.home-source-label')).toHaveText(['rules.md', 'Guide']);
    expect(places.posts('/places/undo')).toHaveLength(2);
    await page.reload();
    await expect(sources(page).locator('.home-source-label')).toHaveText(['rules.md', 'Guide']);
  });

  test(`${theme}: empty sources stay hidden on populated Home`, async ({ page }) => {
    await boot(page, theme, true);
    await expect(page.getByRole('region', { name: 'Chats', exact: true })).toBeVisible();
    await expect(sources(page)).toHaveCount(0);
  });

  test(`${theme}: source actions remain reachable without overflow at supported widths`, async ({ page }) => {
    await boot(page, theme);
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      const button = sources(page).getByRole('button', { name: 'Remove Guide', exact: true });
      await button.focus();
      await expect(button).toBeInViewport();
      const geometry = await sources(page).locator('.home-source-list').evaluate(el => ({ width: el.clientWidth, scroll: el.scrollWidth, row: el.querySelector('.row')!.getBoundingClientRect().height }));
      expect(geometry.scroll).toBeLessThanOrEqual(geometry.width);
      expect(geometry.row).toBe(30);
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
    }
  });
}

test('failed source removal preserves engine rows and permits retry', async ({ page }) => {
  const places = await boot(page);
  places.fail('sources/remove', { status: 409, error: 'This place changed. Try again.', code: 'conflict' });
  const button = sources(page).getByRole('button', { name: 'Remove Guide', exact: true });
  await button.focus();
  await button.press('Enter');
  await expect(sources(page).getByRole('alert')).toHaveText('This place changed. Try again.');
  await expect(sources(page).locator('.home-source-label')).toHaveCount(3);
  await expect(button).toBeEnabled();
  expect(places.state().places[0].sources).toHaveLength(3);
  places.fail('sources/remove', undefined);
  await button.press('Enter');
  await expect(sources(page).locator('.home-source-label')).toHaveText(['Project', 'rules.md']);
  await expect(sources(page).getByRole('alert')).toHaveCount(0);
});

test('pending source removal keeps rows and disables duplicate writes', async ({ page }) => {
  const places = await boot(page);
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/sources/remove', async route => { await held; await route.fallback(); });
  const button = sources(page).getByRole('button', { name: 'Remove Guide', exact: true });
  await button.focus();
  await button.press('Enter');
  await expect(sources(page)).toHaveAttribute('aria-busy', 'true');
  await expect(button).toBeDisabled();
  await expect(sources(page).locator('.home-source-label')).toHaveCount(3);
  await expect(sources(page).getByRole('button', { name: 'Remove Project', exact: true })).toBeDisabled();
  release();
  await expect(sources(page).locator('.home-source-label')).toHaveText(['Project', 'rules.md']);
  expect(places.posts('/sources/remove')).toHaveLength(1);
});

test('offline Home keeps its source list without mutation controls', async ({ page }) => {
  const places = await boot(page);
  places.fail('home', 'abort');
  places.nudge();
  await expect(page.locator('.home-notice[data-connection="offline"]')).toContainText('Can’t reach the engine.');
  await expect(sources(page).locator('.home-source-label')).toHaveText(['Project', 'rules.md', 'Guide']);
  await expect(sources(page).getByRole('button')).toHaveCount(0);
  expect(places.posts('/sources/remove')).toHaveLength(0);
});
