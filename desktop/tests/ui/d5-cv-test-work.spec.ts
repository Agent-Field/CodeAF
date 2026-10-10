import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  for (const reducedMotion of ['no-preference', 'reduce'] as const) {
    test(`${theme}, ${reducedMotion}: only the last running step shimmers; settling freezes its clock`, async ({ page }) => {
      await page.emulateMedia({ reducedMotion });
      await page.goto('/');
      await page.evaluate(async theme => {
        const path = '/tests/ui/fixtures/work-shimmer-mount.ts';
        const { mountWorkShimmer } = await import(path);
        const render = mountWorkShimmer(theme);
        (window as any).renderWorkShimmer = render;
        render(false, 13000);
      }, theme);
      const titles = page.locator('.work-step-title');
      await expect(titles).toHaveCount(4);
      await expect(titles.nth(0)).toHaveCSS('animation-name', 'none');
      await expect(titles.nth(1)).toHaveCSS('animation-name', reducedMotion === 'reduce' ? 'none' : 'cf-shimmer');
      await expect(titles.nth(2)).toHaveCSS('animation-name', 'none');
      await expect(titles.nth(3)).toHaveCSS('animation-name', 'none');
      await expect(titles.nth(1)).toHaveCSS('white-space', 'nowrap');
      await expect(titles.nth(1)).toHaveCSS('text-overflow', 'ellipsis');
      if (reducedMotion === 'no-preference') {
        await expect(titles.nth(1)).toHaveCSS('animation-duration', '2.4s');
        await expect(titles.nth(1)).toHaveCSS('animation-timing-function', 'linear');
        await expect(titles.nth(1)).toHaveCSS('background-size', '250% 100%');
        const gradient = await titles.nth(1).evaluate(el => getComputedStyle(el).backgroundImage);
        expect(gradient).toContain('50%');
      } else {
        expect(await titles.nth(1).evaluate(el => {
          const probe = document.createElement('span'); probe.style.color = 'var(--ink-2)'; el.append(probe);
          const matches = getComputedStyle(el).color === getComputedStyle(probe).color; probe.remove(); return matches;
        })).toBe(true);
      }
      const clock = page.locator('.work-step').nth(1).locator('.work-time');
      await expect(clock).toHaveText('12s');
      await page.evaluate(() => (window as any).renderWorkShimmer(false, 16000));
      await expect(clock).toHaveText('15s');
      await page.evaluate(() => (window as any).renderWorkShimmer(true, 17000));
      await expect(clock).toHaveText('12s');
      await page.evaluate(() => (window as any).renderWorkShimmer(true, 60000));
      await expect(clock).toHaveText('12s');
      for (const title of await titles.all()) await expect(title).toHaveCSS('animation-name', 'none');
      expect(await titles.nth(1).evaluate(el => {
        const probe = document.createElement('span'); probe.style.color = 'var(--ink-3)'; el.append(probe);
        const matches = getComputedStyle(el).color === getComputedStyle(probe).color; probe.remove(); return matches;
      })).toBe(true);
    });
  }
}
