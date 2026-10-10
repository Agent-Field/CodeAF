import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import { message, openApp } from './support/conversation';
import { freshWorkspace, workspaceKey } from '../../src/features/tabs/model';
import { newTab } from '../../src/features/tabs/helpers';

const hues = { now: 250, graphite: 250, sand: 65, sage: 150, tide: 240, iris: 285, rose: 12 };
for (const theme of ['light', 'dark'] as const) {
  for (const [tint, hue] of Object.entries(hues)) {
    test(`CV-207 empty start uses ${tint} in ${theme}`, async ({ page }) => {
      await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
      await installMockEngine(page, { initial: { entries: [], title: 'Quiet chat' } });
      const places = await installMockPlaces(page, {
        places: [{ name: 'Garden', tint: (tint === 'now' ? 'graphite' : tint) as Exclude<keyof typeof hues, 'now'>, pinned: true }],
        chats: [{ id: 'empty-chat', title: 'Quiet chat', places: ['Garden'] }],
      });
      if (tint !== 'now') {
        // Home is a place overview, so an empty saved conversation is reached through its restored tab set.
        const place = places.id('Garden');
        const workspace = freshWorkspace({ id: place, title: 'Garden' });
        workspace.tabs.push(newTab({ id: 'empty-chat-tab', title: 'Quiet chat', sessionFile: sessionFileFor('empty-chat') }));
        await page.addInitScript(({ key, workspace }) => localStorage.setItem(key, JSON.stringify(workspace)), { key: workspaceKey(place), workspace });
      }
      await openApp(page);
      if (tint !== 'now') {
        await page.locator('.place-rail').getByRole('button', { name: /Garden/ }).first().click();
        await expect(page.getByRole('tab', { name: 'Garden', exact: true })).toHaveAttribute('aria-selected', 'true');
        await page.getByRole('tab', { name: 'Quiet chat', exact: true }).click();
      }
      const start = page.locator('.conversation-main[data-empty]');
      await expect(start).toBeVisible();
      const title = start.getByText('What are we building?', { exact: true });
      await expect(title).toBeVisible();
      const measured = await start.evaluate(el => {
        const title = el.querySelector('.empty-start-title')!;
        const composer = el.querySelector('.composer')!;
        const rect = el.getBoundingClientRect();
        const box = composer.getBoundingClientRect();
        return { canvas: getComputedStyle(el).backgroundColor, ink: getComputedStyle(title).color,
          size: getComputedStyle(title).fontSize, weight: getComputedStyle(title).fontWeight,
          centre: box.x + box.width / 2, expectedCentre: rect.x + rect.width / 2,
          middle: box.y + box.height / 2, top: rect.top, bottom: rect.bottom };
      });
      expect(measured.canvas).toBe(`oklch(${theme === 'light' ? '0.985 0.004' : '0.19 0.006'} ${hue})`);
      expect(measured.ink).toBe(`oklch(${theme === 'light' ? '0.47 0.012' : '0.74 0.008'} ${hue})`);
      expect(measured.size).toBe('15px');
      expect(measured.weight).toBe('500');
      expect(Math.abs(measured.centre - measured.expectedCentre)).toBeLessThan(1);
      expect(measured.middle).toBeGreaterThan(measured.top + (measured.bottom - measured.top) * .25);
      expect(measured.middle).toBeLessThan(measured.top + (measured.bottom - measured.top) * .75);
      await expect(message(page)).toHaveValue('');
      await expect(start.locator('.suggestions, .splash')).toHaveCount(0);
    });
  }
}
