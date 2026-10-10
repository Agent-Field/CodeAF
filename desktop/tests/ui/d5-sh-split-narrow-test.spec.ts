import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// SH-211: at 600px and below a split shows only its focused pane; the merged tab's segments and the pane keys
// switch it; drafts survive; nothing overflows sideways. At 1200px all four panes of a 2x2 show.
test.beforeEach(async ({ page }) => { await installMockEngine(page, plainReply()); });

const NAMES = ['Alpha', 'Beta', 'Gamma', 'Delta'];
const panes4 = NAMES.map((title, i) => ({ id: `p${i + 1}`, title, draft: '', kind: 'conversation' }));
async function seed(page: Page) {
  const tab = { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', split: { layout: '2x2', focus: 0, panes: panes4 } };
  const state = { tabs: [tab], groups: [], closed: [], activeId: 'sp', nextNumber: 2, recentIds: ['sp'] };
  await page.addInitScript(value => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem('codeaf.desktop.workspace.v1', value); sessionStorage.setItem('seeded', '1'); } }, JSON.stringify(state));
}
const noSideways = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
const visibleCount = async (page: Page) => (await page.locator('.workspace-pane').evaluateAll(els => els.filter(el => (el as HTMLElement).offsetParent !== null).length));

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.beforeEach(async ({ page }) => { await page.emulateMedia({ colorScheme: scheme }); await seed(page); });

    for (const width of [320, 480, 600]) {
      test(`at ${width}px one pane shows, segments and keys switch it, drafts survive, no sideways scroll`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        await page.goto('/');
        const panes = page.locator('.workspace-pane');
        await expect(panes).toHaveCount(4);
        await expect(panes.nth(0)).toBeVisible();
        expect(await visibleCount(page)).toBe(1);
        expect(await noSideways(page)).toBe(true);

        // A draft typed in the focused pane is still there after visiting every other pane and coming back.
        const message = (n: number) => panes.nth(n).getByRole('textbox', { name: 'Message', exact: true });
        await message(0).fill('half a thought');
        for (const [i, name] of NAMES.entries()) {
          if (i === 0) continue;
          await page.getByRole('tab', { name, exact: true }).click();
          await expect(panes.nth(i)).toBeVisible();
          expect(await visibleCount(page)).toBe(1);
          expect(await noSideways(page)).toBe(true);
        }
        await page.getByRole('tab', { name: 'Alpha', exact: true }).click();
        await expect(message(0)).toHaveValue('half a thought');

        // Keyboard path: the pane-focus keys move the one visible pane.
        const mod = process.platform === 'darwin' ? 'Meta+Alt' : 'Control+Alt';
        await page.keyboard.press(`${mod}+ArrowRight`);
        await expect(panes.nth(1)).toBeVisible();
        await expect(panes.nth(0)).toBeHidden();
        await page.keyboard.press(`${mod}+ArrowLeft`);
        await expect(panes.nth(0)).toBeVisible();
        await expect(message(0)).toHaveValue('half a thought');
        await expectAccessible(page);
      });
    }

    test('at 1200px all four panes show', async ({ page }) => {
      await page.setViewportSize({ width: 1200, height: 800 });
      await page.goto('/');
      await expect(page.locator('.workspace-pane')).toHaveCount(4);
      expect(await visibleCount(page)).toBe(4);
      expect(await noSideways(page)).toBe(true);
      await expectAccessible(page);
    });
  });
}
