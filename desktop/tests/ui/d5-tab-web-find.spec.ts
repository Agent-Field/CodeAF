import { test, expect } from '@playwright/test';
import { desktopTabEvent } from '../../src/lib/desktopMenuRoute';
import { dismissCoveringToasts, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';

for (const theme of ['light', 'dark']) {
  for (const width of [320, 1200]) {
    test(`${theme} ${width}: find field counts matches and Esc restores the address`, async ({ page }) => {
      await page.route('**/api/engine/**', route => route.abort());
      await installNativeWebMock(page);
      await seedWebTab(page);
      // The native dependency supplies totals, but deliberately has no current index.
      await page.addInitScript(theme => {
        localStorage.setItem('codeaf-theme', theme);
        const native = (window as any).__TAURI_INTERNALS__;
        const invoke = native.invoke;
        native.invoke = (cmd: string, args: any) => {
          if (cmd !== 'web_find') return invoke(cmd, args);
          (window as any).__nativeWeb.calls.push({ cmd, args });
          if (args.query === 'failed') return Promise.reject('Search failed');
          if (args.query === 'slow') return new Promise(resolve => { (window as any).__finishFind = () => resolve({ found: true, matches: 99 }); });
          return Promise.resolve(args.query === 'counted' ? { found: true, matches: 3 } : { found: !!args.query });
        };
      }, theme);
      await page.setViewportSize({ width, height: 800 });
      await page.goto('/');
      await dismissCoveringToasts(page);
      await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
      const address = page.locator('.web-header .web-address');
      const before = await address.boundingBox();
      // Native menu events and DOM shortcuts both reach the focused pane.
      if (width === 1200) {
        const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
        await page.keyboard.press(`${modifier}+f`);
      } else {
        await page.evaluate(name => window.dispatchEvent(new CustomEvent(name, { detail: 'web-find' })), desktopTabEvent);
      }
      const input = page.getByRole('textbox', { name: 'Find in page', exact: true });
      await expect(input).toBeFocused();
      await expect(input).toHaveCSS('height', '30px');
      const after = await page.locator('.web-find').boundingBox();
      expect(after?.height).toBe(before?.height);
      expect(after?.width).toBe(before?.width);
      await input.fill('counted');
      const count = page.locator('.web-find-count');
      await expect(count).toHaveText('3 matches');
      expect(await count.evaluate(e => getComputedStyle(e).color)).toBe(await count.evaluate(e => getComputedStyle(e).getPropertyValue('--ink-3').trim()).then(async token => page.evaluate(token => {
        const probe = document.createElement('span'); probe.style.color = token; document.body.append(probe);
        const color = getComputedStyle(probe).color; probe.remove(); return color;
      }, token)));
      await input.press('Enter');
      await expect.poll(async () => (await nativeCalls(page, 'web_find')).at(-1)?.args.forward).toBe(true);
      await input.press('Shift+Enter');
      await expect.poll(async () => (await nativeCalls(page, 'web_find')).at(-1)?.args.forward).toBe(false);
      await input.fill('slow');
      await expect.poll(async () => (await nativeCalls(page, 'web_find')).at(-1)?.args.query).toBe('slow');
      await input.fill('counted');
      await expect(count).toHaveText('3 matches');
      await page.evaluate(() => (window as any).__finishFind());
      await expect(count).toHaveText('3 matches');
      await input.fill('failed');
      await expect(count).toHaveCount(0);
      await input.fill('unknown');
      await expect(count).toHaveCount(0);
      await input.press('Escape');
      await expect(input).toHaveCount(0);
      await expect(page.locator('.web-address-site')).toHaveText('pkg.go.dev');
      await expect.poll(async () => (await nativeCalls(page, 'web_find')).at(-1)?.args.query).toBe('');
      expect(await nativeCalls(page, 'web_navigate')).toHaveLength(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    });
  }
}

test('find stays absent until the native command exists', async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await installNativeWebMock(page);
  await seedWebTab(page);
  await page.goto('/');
  await dismissCoveringToasts(page);
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
  await page.evaluate(name => window.dispatchEvent(new CustomEvent(name, { detail: 'web-find' })), desktopTabEvent);
  await expect.poll(async () => (await nativeCalls(page, 'web_find')).length).toBe(1);
  await expect(page.locator('.web-find')).toHaveCount(0);
  await expect(page.locator('.web-address-site')).toHaveText('pkg.go.dev');
});
