import { test, expect } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine } from './support/mock-engine';
import { openPage } from './support/shell-navigation';

const placeIcons = ['folderTree', 'chevronsUpDown', 'eye', 'palette', 'cpu', 'appWindow', 'archive', 'fileText', 'folderPlus', 'alignLeft', 'folderGit', 'circleDashed', 'arrowRight'] as const;

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: every place icon renders in the design system specimen`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await installMockEngine(page, { initial: {} });
  await page.goto('/');
  await openPage(page, 'Design system');
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  const specimen = page.locator('.icon-specimens');
  for (const name of placeIcons) {
   const button = specimen.getByRole('button', { name: `${name} icon`, exact: true });
   await button.scrollIntoViewIfNeeded();
   await expect(button).toBeVisible();
   const icon = button.locator(`[data-icon="${name}"]`);
   await expect(icon).toHaveAttribute('aria-hidden', 'true');
   await expect(icon).toHaveAttribute('data-motion', 'none');
   await expect(icon.locator('svg')).toHaveCount(1);
   const glyph = await icon.locator('svg').evaluate(svg => {
    const rect = svg.getBoundingClientRect();
    const style = getComputedStyle(svg);
    return { width: rect.width, height: rect.height, stroke: style.stroke, color: style.color, strokeWidth: style.strokeWidth, marks: svg.children.length, animations: svg.getAnimations({ subtree: true }).length };
   });
   expect(glyph.width).toBe(parseFloat(design.foundation['icon-md']));
   expect(glyph.height).toBe(parseFloat(design.foundation['icon-md']));
   expect(parseFloat(glyph.strokeWidth)).toBe(Number(design.foundation['icon-stroke']));
   expect(glyph.stroke).toBe(glyph.color);
   expect(glyph.marks).toBeGreaterThan(0);
   expect(glyph.animations).toBe(0);
  }
 });
}
