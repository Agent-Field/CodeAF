import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import design from '../../src/design/tokens.json' with { type: 'json' };

// SH-042, SH-087, SH-136, SH-212: with a coarse pointer the active tab's close hit is 24px, rail rows are 40px,
// the pane divider's hit is 16px, and holding a tab opens its "Actions for <title>" menu without selecting it.
const px = (name: string) => parseFloat((design.foundation as Record<string, string>)[name]);
const holdMs = design.interaction.longPressDelay;

const panes = ['Alpha', 'Beta'].map((title, i) => ({ id: `p${i + 1}`, title, draft: '', kind: 'conversation' }));
const tabs = [
  { id: 'a', title: 'First tab', titleSource: 'manual', kind: 'conversation', draft: '', pinned: false },
  { id: 'b', title: 'Second tab', titleSource: 'manual', kind: 'conversation', draft: '', pinned: false },
  { id: 'sp', title: 'Split', titleSource: 'manual', kind: 'conversation', draft: '', pinned: false, split: { layout: '1x2', focus: 0, panes } },
];

async function open(page: Page, theme: string, coarse: boolean) {
  await installMockEngine(page, plainReply());
  await page.addInitScript(({ theme, tabs }) => {
    localStorage.setItem('codeaf-theme', theme);
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs, groups: [], closed: [], activeId: 'sp', nextNumber: 4, recentIds: ['sp', 'a', 'b'] }));
  }, { theme, tabs });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  await expect(page.getByRole('tab', { name: 'First tab', exact: true })).toBeVisible();
  expect(await page.evaluate(() => matchMedia('(pointer: coarse)').matches)).toBe(coarse);
}

// A finger's hold: pointerdown with pointerType touch, then the delay passes with the contact still down.
async function hold(page: Page, selector: string) {
  await page.locator(selector).first().evaluate(async (el, ms) => {
    const r = el.getBoundingClientRect();
    const init = { bubbles: true, cancelable: true, pointerId: 7, pointerType: 'touch', isPrimary: true, button: 0, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
    el.dispatchEvent(new PointerEvent('pointerdown', init));
    await new Promise(done => setTimeout(done, ms + 150));
  }, holdMs);
}
const release = (page: Page) => page.evaluate(() => document.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerId: 7, pointerType: 'touch' })));

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    test.describe(`${theme}, ${width}px`, () => {
      test.use({ viewport: { width, height: 800 }, colorScheme: theme, hasTouch: true, isMobile: true, reducedMotion: 'reduce' });

      test('SH-087 SH-136: the open tab shows a 24px close and a hold opens its menu without selecting it', async ({ page }) => {
        await open(page, theme, true);
        const close = page.locator('.workspace-tab[data-active="true"] .workspace-tab-close');
        const box = (await close.boundingBox())!;
        expect(box.width).toBeGreaterThanOrEqual(px('hit-coarse-close'));
        expect(box.height).toBeGreaterThanOrEqual(px('hit-coarse-close'));
        expect(await close.evaluate(el => getComputedStyle(el).opacity)).toBe('1');

        await hold(page, '.workspace-tab:not([data-active="true"])');
        await expect(page.getByRole('menu', { name: 'Actions for First tab' })).toBeVisible();
        await release(page);
        await page.keyboard.press('Escape');
        await expect(page.getByRole('menu')).toHaveCount(0);
        await expect(page.getByRole('tab', { name: 'First tab', exact: true })).toHaveAttribute('aria-selected', 'false');

        const axe = await new AxeBuilder({ page }).include('.workspace-tabstrip').withTags(['wcag2a', 'wcag2aa']).analyze();
        // Contrast of ink-3 labels is judged elsewhere; this spec adds no structural violation.
        expect(axe.violations.filter(v => v.id !== 'color-contrast')).toEqual([]);
      });

      test('SH-087: a hold that starts on the close mark stays a close, not a menu', async ({ page }) => {
        await open(page, theme, true);
        await hold(page, '.workspace-tab[data-active="true"] .workspace-tab-close');
        await release(page);
        await expect(page.getByRole('menu')).toHaveCount(0);
      });

      test('SH-212: the pane divider hit is 16px and stays centred on the gap', async ({ page }) => {
        await open(page, theme, true);
        const handle = page.locator('.pane-resize[data-axis="col"]');
        if (width <= 600) { await expect(handle).toBeHidden(); return; }
        const box = (await handle.boundingBox())!;
        expect(box.width).toBe(px('pane-resize-hit-coarse'));
        const gap = await page.evaluate(() => {
          const [a, b] = [...document.querySelectorAll('.workspace-pane')].map(p => p.getBoundingClientRect());
          return (a.right + b.left) / 2;
        });
        expect(Math.abs(box.x + box.width / 2 - gap)).toBeLessThanOrEqual(1);
      });

      test('SH-042: rail rows are 40px', async ({ page }) => {
        await open(page, theme, true);
        const rows = page.locator('.sidebar.rail .nav-item:visible');
        if (width <= 600 && (await rows.count()) === 0) test.skip(true, 'The rail is collapsed at this width; rows are covered at 1200px.');
        expect(await rows.count()).toBeGreaterThan(0);
        for (const h of await rows.evaluateAll(els => els.map(el => el.getBoundingClientRect().height))) expect(h).toBeGreaterThanOrEqual(px('hit-coarse-row'));
      });
    });
  }

  test.describe(`${theme}, fine pointer`, () => {
    test.use({ viewport: { width: 1200, height: 800 }, colorScheme: theme, reducedMotion: 'reduce' });
    test('a mouse keeps the 8px divider, a close under 24px and no hold menu', async ({ page }) => {
      await open(page, theme, false);
      expect((await page.locator('.pane-resize[data-axis="col"]').boundingBox())!.width).toBe(px('pane-resize-hit'));
      expect((await page.locator('.workspace-tab[data-active="true"] .workspace-tab-close').boundingBox())!.width).toBeLessThan(px('hit-coarse-close'));
      await page.locator('.workspace-tab:not([data-active="true"])').first().dispatchEvent('pointerdown', { pointerType: 'mouse', button: 0 });
      await page.waitForTimeout(holdMs + 150);
      await expect(page.getByRole('menu')).toHaveCount(0);
    });
  });
}
