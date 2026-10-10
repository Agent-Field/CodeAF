import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import design from '../../src/design/tokens.json' with { type: 'json' };

const title = 'Last tab';
const draft = 'A saved thought from the last tab';
const padding = Number.parseFloat(design.foundation['preview-collision-padding']);
const column = Number.parseFloat(design.foundation['preview-card-width']);
const inlinePadding = Number.parseFloat(design.foundation['preview-card-inline-padding']);
const preview = (page: Page) => page.getByRole('group', { name: `Preview of ${title}`, exact: true });
const tab = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });

async function previewViolations(page: Page) {
  const result = await new AxeBuilder({ page }).include('.tab-preview').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  return result.violations.flatMap(violation => violation.nodes.map(node => JSON.stringify({ id: violation.id, target: node.target }))).sort();
}

async function open(page: Page, theme: 'light' | 'dark') {
  await installMockEngine(page, { initial: {} });
  await page.addInitScript(({ theme, draft }) => {
    localStorage.setItem('codeaf-theme', theme);
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [
        { id: 'first', title: 'First tab', titleSource: 'manual', kind: 'conversation', draft: '', pinned: false },
        { id: 'last', title: 'Last tab', titleSource: 'manual', kind: 'conversation', draft, pinned: false },
      ],
      groups: [], closed: [], activeId: 'first', nextNumber: 3, recentIds: ['first', 'last'],
    }));
  }, { theme, draft });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  await expect(tab(page, title)).toBeVisible();
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    test.describe(`${theme}, ${width}px`, () => {
      test.use({ viewport: { width, height: 800 }, colorScheme: theme, reducedMotion: 'reduce' });

      test('SH-168: the last tab preview fits the viewport and Escape preserves keyboard navigation', async ({ page }) => {
        await open(page, theme);
        // Compare with the wide preview without disabling rules: its existing ink-3 kind label fails contrast in Light.
        await page.setViewportSize({ width: 1200, height: 800 });
        await tab(page, title).hover();
        await expect(preview(page)).toBeVisible();
        const baseline = await previewViolations(page);
        await page.keyboard.press('Escape');
        await page.mouse.move(0, 0);
        await page.setViewportSize({ width, height: 800 });
        await tab(page, title).hover();
        const card = preview(page).locator('.preview-card');
        await expect(card).toBeVisible();
        await expect(card).toContainText(draft);
        // Shell 3k measures 328px including padding; SH-168 caps the outer card at 300px on small windows.
        const expectedWidth = Math.min(width <= 600 ? column : column + inlinePadding * 2, width - padding * 2);
        await expect.poll(() => card.evaluate(el => el.getBoundingClientRect().width)).toBe(expectedWidth);
        for (const surface of [preview(page), card]) {
          const box = await surface.evaluate(el => {
            const rect = el.getBoundingClientRect();
            return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
          });
          expect(box.left).toBeGreaterThanOrEqual(padding);
          expect(box.right).toBeLessThanOrEqual(width - padding);
          expect(box.top).toBeGreaterThanOrEqual(padding);
          expect(box.bottom).toBeLessThanOrEqual(800 - padding);
        }
        expect((await previewViolations(page)).filter(violation => !baseline.includes(violation))).toEqual([]);
        await page.keyboard.press('Escape');
        await expect(preview(page)).toHaveCount(0);
        await expectAccessible(page);
        await page.mouse.move(0, 0);
        await tab(page, 'First tab').focus();
        await page.keyboard.press('End');
        await expect(tab(page, title)).toBeFocused();
        await expect(tab(page, title)).toHaveAttribute('aria-selected', 'true');
        await expect(preview(page)).toHaveCount(0);
        await page.keyboard.press('Home');
        await expect(tab(page, 'First tab')).toBeFocused();
        await expect(tab(page, 'First tab')).toHaveAttribute('aria-selected', 'true');
      });

      test.describe('touch pointer', () => {
        test.use({ hasTouch: true });

        test('SH-169: taps select tabs without opening a card, including delayed or keyboard paths', async ({ page }) => {
          await open(page, theme);
          expect(await page.evaluate(() => matchMedia('(hover: none)').matches)).toBe(true);
          await page.clock.install();
          // Record even a transient card: asserting absence only after the tap could miss a card closed by selection.
          await page.evaluate(() => {
            const observed: string[] = [];
            (window as typeof window & { previewObservations: string[] }).previewObservations = observed;
            new MutationObserver(records => {
              for (const record of records) for (const node of record.addedNodes) {
                if (node instanceof Element && (node.matches('.tab-preview') || node.querySelector('.tab-preview'))) observed.push('opened');
              }
            }).observe(document.body, { childList: true, subtree: true });
          });
          await tab(page, title).tap();
          await expect(tab(page, title)).toHaveAttribute('aria-selected', 'true');
          await page.clock.runFor(design.interaction.previewOpenDelay * 2);
          await tab(page, 'First tab').tap();
          await expect(tab(page, 'First tab')).toHaveAttribute('aria-selected', 'true');
          // Mouse events on a touch-capable screen still cannot open a card when the media query says hover:none.
          await tab(page, title).hover();
          await page.clock.runFor(design.interaction.previewOpenDelay * 2);
          await page.mouse.move(0, 0);
          await tab(page, 'First tab').focus();
          await page.keyboard.press('End');
          await expect(tab(page, title)).toBeFocused();
          await expect(tab(page, title)).toHaveAttribute('aria-selected', 'true');
          await page.clock.runFor(design.interaction.previewOpenDelay * 2);
          await expect(page.locator('.tab-preview')).toHaveCount(0);
          expect(await page.evaluate(() => (window as typeof window & { previewObservations: string[] }).previewObservations)).toEqual([]);
          await expectAccessible(page);
        });
      });
    });
  }
}
