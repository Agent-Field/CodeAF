import { test, expect } from '@playwright/test';
import { webErrorLine } from '../../src/features/web/strings';
import { installMockEngine } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { emitState, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';

for (const theme of ['light', 'dark']) {
  test(`${theme}: certificate state hides the page, stays muted, marks failed and exposes both actions`, async ({ page }) => {
    await installNativeWebMock(page, { engine: true });
    await installMockEngine(page, richReply());
    await seedWebTab(page);
    await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, 'webpane1', { loading: false, failure: { kind: 'certificate' } });
    const status = page.locator('.web-status');
    await expect(status.locator('.web-status-line')).toHaveText(webErrorLine.certificate);
    await expect(status.locator('svg')).toHaveCount(0);
    const tab = page.locator('.workspace-tab[data-kind="web"]');
    await expect(tab).toHaveAttribute('data-state', 'failed');
    await expect(tab.locator('[data-state="waiting"]')).toHaveCount(0);
    await expect.poll(async () => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible).toBe(false);
    for (const width of [320, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      await expect(status.getByRole('button', { name: 'Open in browser' })).toBeVisible();
      const geometry = await status.evaluate(el => {
        const sheet = el.closest('.web-sheet')!;
        const a = el.getBoundingClientRect(), b = sheet.getBoundingClientRect();
        const line = el.querySelector('.web-status-line')!;
        const probe = document.createElement('span');
        probe.style.color = 'var(--ink-3)';
        sheet.append(probe);
        const muted = getComputedStyle(probe).color;
        probe.remove();
        return { dx: Math.abs(a.x + a.width / 2 - b.x - b.width / 2), dy: Math.abs(a.y + a.height / 2 - b.y - b.height / 2), ink: getComputedStyle(line).color, muted, overflow: document.documentElement.scrollWidth > innerWidth };
      });
      expect(geometry.dx).toBeLessThan(1);
      expect(geometry.dy).toBeLessThan(1);
      expect(geometry.ink.replace(/\s+/g, '')).toBe(geometry.muted.replace(/\s+/g, ''));
      expect(geometry.overflow).toBe(false);
    }
    await status.getByRole('button', { name: 'Open in browser' }).click();
    expect((await nativeCalls(page, 'open_url')).at(-1)?.args.url).toBe('https://pkg.go.dev/encoding/json#Decoder');
    await status.getByRole('button', { name: 'Reload', exact: true }).click();
    expect((await nativeCalls(page, 'web_history')).at(-1)?.args.step).toBe('reload');
    await expect(status).toHaveCount(0);
    await expect(tab).not.toHaveAttribute('data-state', 'failed');
  });
}

for (const [kind, sentence] of Object.entries(webErrorLine)) {
  test(`${kind}: the status component draws one line and both actions`, async ({ page }) => {
    await page.route('**/api/engine/**', route => route.abort());
    await page.goto('/');
    await page.evaluate(async kind => {
      const { mountWebStatusProbe } = await import('/tests/ui/support/web-status-probe.tsx');
      Object.assign(window, { statusProbe: mountWebStatusProbe(kind) });
    }, kind);
    const status = page.locator('.web-status');
    await expect(status.locator('.web-status-line')).toHaveText(sentence);
    await expect(status.locator('p')).toHaveCount(1);
    await status.getByRole('button', { name: 'Open in browser' }).click();
    await status.getByRole('button', { name: 'Reload' }).click();
    expect(await page.evaluate(() => (window as unknown as { statusProbe: { calls: string[] } }).statusProbe.calls)).toEqual(['external', 'reload']);
  });
}
