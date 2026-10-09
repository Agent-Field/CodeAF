import { expect, type Locator, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
export async function tokenColor(page: Page, token: string) {
 return page.evaluate(name => {
  const probe = document.createElement('span');
  probe.style.color = `var(--${name})`; document.body.append(probe);
  const color = getComputedStyle(probe).color; probe.remove(); return color;
 }, token);
}
export async function expectThemedSurface(page: Page, surface: Locator) {
 await expect(surface).toHaveCSS('opacity','1');
 await expect(surface).toHaveCSS('background-color', await tokenColor(page, 'overlay-surface'));
 await expect(surface).toHaveCSS('color', await tokenColor(page, 'text'));
}
export async function expectAccessible(page: Page) {
 // Contrast is meaningful after entry/exit motion settles; transient opacity is not a theme color.
 await page.evaluate(async () => {
  const animations = document.getAnimations().filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity);
  await Promise.all(animations.map(animation => animation.finished.catch(() => undefined)));
 });
 const result = await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
 expect(result.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))).toEqual([]);
}
export async function expectNoUnstyledControls(page: Page) {
 const selects = page.locator('select:not([aria-hidden="true"])');
 for (const control of await selects.all()) await expect(control).not.toBeVisible();
 for (const control of await page.locator('button,input:not([type=hidden]),textarea').all()) {
  if (await control.isVisible()) await expect(control).toHaveAttribute('class', /(?:button|nav-item|address-field|favorite-button|new-item|select-trigger|palette-close|command-item|text-input)/);
 }
}
