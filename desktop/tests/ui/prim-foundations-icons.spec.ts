import { test, expect } from '@playwright/test';
import { openPage } from './support/shell-navigation';

// F-ICON-5/9/11/13/16/17, Q34: every step category resolves to its designed glyph, and `play` is a one-shot.
const required = ['folderTree','chevronsUpDown','foldVertical','rotateCw','scrollText','reply','quote','terminalSquare','fileJson','maximize','clock3','fileDiff','appWindow','link','archive','panelRight','browse','communicate','plan','edit','stopped','create'];

test('Icon family renders every registry name, mirrored panelRight included', async ({ page }) => {
 await page.goto('/');
 await openPage(page, 'Design system');
 for (const n of required.filter(n => n !== 'stopped')) await expect(page.locator(`.icon-specimens [data-icon="${n}"]`).first()).toBeAttached();
 await expect(page.locator('.icon-specimens [data-icon="panelRight"] svg').first()).toHaveCSS('transform', 'matrix(-1, 0, 0, 1, 0, 0)');
 await expect(page.locator('.icon-specimens [data-icon="sidebar"] svg').first()).toHaveCSS('transform', 'none');
});

test('step categories map to the designed glyph names', async ({ page }) => {
 await page.goto('/');
 await openPage(page, 'Design system');
 const svgOf = (n: string) => page.locator(`.icon-specimens [data-icon="${n}"] svg`).first().evaluate(e => e.innerHTML);
 expect(await svgOf('browse')).toBe(await svgOf('web'));
 expect(await svgOf('edit')).toBe(await svgOf('pencil'));
 expect(await svgOf('plan')).toBe(await svgOf('checklist'));
 expect(await svgOf('communicate')).toBe(await svgOf('tab'));
});

// framer-motion drives the glyph through the Web Animations API, so a running animation is visible to getAnimations().
const live = (page: import('@playwright/test').Page, name: string) => page.locator(`[data-icon="${name}"]`).evaluate(e => e.getAnimations({ subtree: true }).filter(a => a.playState === 'running').length);

test('play: a changed key runs one animation that finishes', async ({ page }) => {
 await page.goto('/tests/icon-play/index.html');
 expect(await live(page, 'check')).toBe(0);
 await page.getByRole('button', { name: 'bump' }).click();
 await expect.poll(() => live(page, 'check')).toBeGreaterThan(0);
 await expect.poll(() => live(page, 'check'), { timeout: 5000 }).toBe(0);
 expect(await live(page, 'plus')).toBe(0);
});

test('play: reduced motion stays static', async ({ page }) => {
 await page.emulateMedia({ reducedMotion: 'reduce' });
 await page.goto('/tests/icon-play/index.html');
 await page.getByRole('button', { name: 'bump' }).click();
 await page.waitForTimeout(150);
 expect(await live(page, 'check')).toBe(0);
});
