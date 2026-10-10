import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

for (const theme of ['light', 'dark']) {
  for (const sources of [false, true]) {
    test(`${theme}: empty place names inherited context only with real sources (${sources})`, async ({ page }) => {
      await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
      await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
      const rig = await installMockPlaces(page, { places: [
        { name: 'Marketing', pinned: true, sources: sources ? [{ kind: 'file', ref: '/work/brand-voice.md', label: 'brand-voice.md' }, { kind: 'url', ref: 'https://codeaf.dev', label: 'codeaf.dev' }] : [] },
        { name: 'Launch site', parents: ['Marketing'], pinned: true },
      ], disk: ['/work/brand-voice.md'] });
      await page.goto('/');
      await page.locator('.place-rail .rail-place').filter({ hasText: /^Launch site/ }).click();
      const empty = page.getByRole('region', { name: 'Empty place' });
      await expect(empty).toBeVisible();
      await expect(empty.locator('p')).toHaveText('Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know.');
      await expect(empty.locator('p')).toHaveCSS('font-size', '15px');
      await expect(empty.locator('p')).toHaveCSS('line-height', '24px');
      await expect(empty.locator('p')).toHaveCSS('max-width', '480px');
      if (sources) await expect(page.locator('.home-context')).toHaveText("Uses Marketing's context: brand-voice.md, codeaf.dev");
      else await expect(page.locator('.home-context')).toHaveCount(0);
      await page.setViewportSize({ width: 320, height: 560 });
      await expect(empty.getByRole('button', { name: 'Add files or links' })).toBeInViewport();
      await expect(empty.getByRole('button', { name: 'Write instructions' })).toBeInViewport();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.setViewportSize({ width: 1200, height: 800 });
      await empty.getByRole('button', { name: 'Write instructions' }).click();
      await expect(page.getByRole('textbox', { name: 'Instructions', exact: true })).toBeFocused();
      await page.keyboard.press('Escape');
      await empty.getByRole('button', { name: 'Add files or links' }).click();
      const dialog = page.getByRole('dialog', { name: 'Files and links for “Launch site”' });
      await expect(dialog).toBeVisible();
      await dialog.getByRole('textbox', { name: 'Add a path or a link' }).fill('https://example.com/context');
      await dialog.getByRole('button', { name: 'Add', exact: true }).click();
      await expect.poll(() => rig.posts('/sources').length).toBe(1);
      await dialog.getByRole('button', { name: 'Done', exact: true }).click();
      await expect(page.getByRole('region', { name: 'Sources' })).toContainText('example.com');
      await expect(empty).toHaveCount(0);
    });
  }
}

test('shared ancestors appear once, with no context beyond two levels', async ({ page }) => {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  await installMockPlaces(page, { places: [
    { name: 'Beyond', sources: [{ kind: 'url', ref: 'https://beyond.example', label: 'too far' }] },
    { name: 'Brand', parents: ['Beyond'], sources: [{ kind: 'url', ref: 'https://brand.example', label: 'brand guide' }] },
    { name: 'Marketing', parents: ['Brand'], sources: [{ kind: 'url', ref: 'https://marketing.example', label: 'voice' }] },
    { name: 'Sales', parents: ['Brand'] },
    { name: 'Launch site', parents: ['Marketing', 'Sales'], pinned: true },
  ] });
  await page.goto('/');
  await page.locator('.place-rail .rail-place').filter({ hasText: /^Launch site/ }).click();
  await expect(page.locator('.home-context')).toHaveText(["Uses Marketing's context: voice", "Uses Brand's context: brand guide"]);
});
