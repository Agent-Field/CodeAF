import { test, expect, type Page } from '@playwright/test';
import { tokenColorIn } from './contracts';
import { dismissCoveringToasts, emitState, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';

const PANE = 'webpane1';

const lastVisible = async (page: Page) => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible;
const sheetFill = (page: Page) => page.evaluate(() => {
  const probe = document.createElement('div');
  probe.style.backgroundColor = 'var(--web-sheet-fill)';
  document.body.append(probe);
  const color = getComputedStyle(probe).backgroundColor;
  probe.remove();
  return color;
});

test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await installNativeWebMock(page);
  await seedWebTab(page);
});

test('opening a tab menu over a web tab hides the page and restores it', async ({ page }) => {
  test.setTimeout(90_000);
  for (const theme of ['light', 'dark'] as const) {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/');
    await dismissCoveringToasts(page);
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, title: 'encoding/json' });
    await expect.poll(() => lastVisible(page)).toBe(true);

    const sheet = page.locator('.content-pane .web-sheet');
    await expect(sheet.locator('.web-occluded-title')).toHaveCount(0);
    const hides = (await nativeCalls(page, 'web_hide_all')).length;
    const shows = (await nativeCalls(page, 'web_show_all')).length;

    await page.locator('.workspace-tab[data-active="true"]').click({ button: 'right' });
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible();
    const meets = await page.evaluate(() => {
      const sheetBox = document.querySelector('.content-pane .web-sheet')!.getBoundingClientRect();
      const menuBox = document.querySelector('[role="menu"]')!.getBoundingClientRect();
      return sheetBox.width > 0 && menuBox.width > 0
        && sheetBox.left < menuBox.right && menuBox.left < sheetBox.right
        && sheetBox.top < menuBox.bottom && menuBox.top < sheetBox.bottom;
    });
    expect(meets).toBe(true);
    await expect.poll(() => lastVisible(page)).toBe(false);
    await expect.poll(async () => (await nativeCalls(page, 'web_hide_all')).length).toBe(hides + 1);
    await expect(sheet).toHaveAttribute('data-blank', 'true');
    await expect(sheet).toHaveCSS('background-color', await sheetFill(page));
    const title = sheet.locator('.web-occluded-title');
    await expect(title).toHaveText('encoding/json');
    await expect(title).toHaveCSS('color', await tokenColorIn(title, 'ink-3'));
    await expect(sheet).not.toContainText(/page hidden|menu is open/i);

    await page.keyboard.press('Escape');
    await page.mouse.move(8, 8);
    await expect(menu).toHaveCount(0);
    await expect(page.locator('.hover-preview')).toHaveCount(0);
    await expect.poll(() => lastVisible(page)).toBe(true);
    await expect.poll(async () => (await nativeCalls(page, 'web_show_all')).length).toBe(shows + 1);
    await expect(sheet.locator('.web-occluded-title')).toHaveCount(0);
    await expect(sheet).not.toHaveAttribute('data-blank', 'true');
  }
});
