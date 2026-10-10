import { test, expect, type Page } from '@playwright/test';

// Shell 2a/2b/2d and F-MAT-*: frame insets, native-controls clearance, drag regions and the frame material states.
// The browser has no Tauri shell, so the environment attribute is forced the way App.tsx writes it in the desktop app.
const environment = (page: Page, value: string) => page.evaluate(v => { document.documentElement.dataset.environment = v; }, value);
const collapse = async (page: Page) => {
  await page.locator('.sidebar .rail-toggle').click();
  await expect(page.locator('.app-shell.sidebar-collapsed')).toBeVisible();
};

test.beforeEach(async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
});

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
      await page.goto('/');
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    });

    for (const width of [320, 600, 1200]) {
      for (const env of ['mac-desktop', 'desktop']) {
        test(`the content card keeps an 8px right and bottom inset · ${env} · ${width}px`, async ({ page }) => {
          await page.setViewportSize({ width, height: 800 });
          await environment(page, env);
          const gap = await page.evaluate(() => {
            const pane = document.querySelector('.content-pane')!.getBoundingClientRect();
            const card = document.querySelector('.content-pane > :last-child')!.getBoundingClientRect();
            return { right: window.innerWidth - card.right, bottom: window.innerHeight - card.bottom, pane: pane.width };
          });
          expect(gap.right).toBe(8);
          expect(gap.bottom).toBe(8);
        });
      }
    }

    test('a collapsed rail pads the strip by the native-controls inset on mac and not on Linux', async ({ page }) => {
      await page.setViewportSize({ width: 1200, height: 800 });
      await collapse(page);
      const strip = page.locator('.workspace-tabbar');
      const inset = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--native-controls-inset').trim());
      expect(inset).toBe('84px');
      await environment(page, 'mac-desktop');
      await expect(strip).toHaveCSS('padding-left', inset);
      await environment(page, 'desktop');
      await expect(strip).not.toHaveCSS('padding-left', inset);
      await expect(strip).toHaveCSS('padding-left', '8px');
    });

    test('the strip and rail head are drag regions and its tabs and buttons are not', async ({ page }) => {
      await expect(page.locator('.workspace-tabbar')).toHaveAttribute('data-tauri-drag-region', 'true');
      await expect(page.locator('.sidebar .rail-head')).toHaveAttribute('data-tauri-drag-region', 'true');
      const regions = await page.evaluate(() => {
        const r = (el: Element) => (getComputedStyle(el) as unknown as Record<string, string>).webkitAppRegion ?? getComputedStyle(el).getPropertyValue('-webkit-app-region');
        return [...document.querySelectorAll('.workspace-tab, .workspace-tabbar > .icon-button, .workspace-tab-action')].map(r);
      });
      expect(regions.length).toBeGreaterThan(0);
      // Engines that do not implement the property report nothing; where it exists it must be no-drag.
      for (const value of regions) if (value) expect(value).toBe('no-drag');
    });

    test('a blurred window is solid: windowActive false and no backdrop blur on the rail', async ({ page }) => {
      await page.evaluate(() => window.dispatchEvent(new Event('focus')));
      await expect(page.locator('html')).toHaveAttribute('data-window-active', 'true');
      await page.evaluate(() => window.dispatchEvent(new Event('blur')));
      await expect(page.locator('html')).toHaveAttribute('data-window-active', 'false');
      await expect(page.locator('html')).toHaveAttribute('data-material', 'solid');
      await expect(page.locator('.app-shell > .sidebar')).toHaveCSS('backdrop-filter', 'none');
    });

    test('more contrast gives a solid frame even while the window is active', async ({ page }) => {
      await page.emulateMedia({ contrast: 'more' });
      await page.evaluate(() => window.dispatchEvent(new Event('focus')));
      await expect(page.locator('html')).toHaveAttribute('data-material', 'solid');
      await expect(page.locator('.app-shell > .sidebar')).toHaveCSS('backdrop-filter', 'none');
      await expect(page.locator('.app-shell')).toHaveCSS('background-color', await page.evaluate(() => {
        const probe = document.createElement('i'); probe.style.background = 'var(--frame)'; document.body.append(probe);
        const c = getComputedStyle(probe).backgroundColor; probe.remove(); return c;
      }));
    });
  });
}
