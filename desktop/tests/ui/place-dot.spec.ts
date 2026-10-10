import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

for (const theme of ['light', 'dark']) {
  test(`place dots name attention and keep running quiet in ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
    await installMockPlaces(page, {
      places: [
        { name: 'Software', pinned: true },
        { name: 'Config parser', parents: ['Software'] },
        { name: 'Failed place', pinned: true },
        { name: 'Running place', pinned: true },
        { name: 'Quiet place', pinned: true },
      ],
      chats: [
        { id: 'need-1', places: ['Config parser'], needsYou: true },
        { id: 'need-2', places: ['Config parser'], needsYou: true, tasks: { failed: 1 } },
        { id: 'failed', places: ['Failed place'], tasks: { failed: 1 } },
        { id: 'running', places: ['Running place'], live: true, tasks: { running: 1 } },
      ],
    });
    await page.goto('/');
    const rail = page.locator('.app-shell .place-rail').first();
    const waiting = rail.getByRole('img', { name: '2 need you in Config parser', exact: true });
    const failed = rail.getByRole('img', { name: '1 failed task in Failed place', exact: true });
    await expect(waiting).toBeVisible();
    await expect(failed).toBeVisible();
    for (const [mark, token] of [[waiting, '--amber'], [failed, '--danger']] as const) {
      const measured = await mark.locator('.status-mark-dot').evaluate((dot, token) => {
        const style = getComputedStyle(dot);
        return { width: dot.getBoundingClientRect().width, height: dot.getBoundingClientRect().height,
          color: style.backgroundColor, expected: style.getPropertyValue(token).trim() };
      }, token);
      expect(measured.width).toBe(6);
      expect(measured.height).toBe(6);
      const channels = (color: string) => color.match(/[\d.]+/g)?.map(Number);
      expect(channels(measured.color)).toEqual(channels(measured.expected));
      await mark.hover();
      await expect(page.getByRole('tooltip')).toHaveText(await mark.getAttribute('aria-label') ?? '');
      await page.keyboard.press('Escape');
      await expect(page.getByRole('tooltip')).toHaveCount(0);
    }
    for (const name of ['Running place', 'Quiet place']) {
      const row = rail.locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: name }) });
      await expect(row).toBeVisible();
      await expect(row.getByRole('img')).toHaveCount(0);
    }
    await waiting.click();
    await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Software');
  });
}
