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
 // One in-page pass: per-control locator round trips cost ~15s over the Design system specimen in webkit.
 const offenders = await page.evaluate(() => {
  const shown = (el: Element) => {
   const rect = el.getBoundingClientRect();
   return rect.width > 0 && rect.height > 0 && getComputedStyle(el).visibility !== 'hidden';
  };
  const themed = /(?:button|nav-item|address-field|favorite-button|new-item|select-trigger|palette-close|command-item|text-input|segmented-option|chip-button)/;
  const bad: string[] = [];
  for (const el of document.querySelectorAll('select:not([aria-hidden="true"])')) if (shown(el)) bad.push(`visible native select: ${el.outerHTML.slice(0, 120)}`);
  for (const el of document.querySelectorAll('button,input:not([type=hidden]),textarea')) {
   if (shown(el) && !themed.test(el.getAttribute('class') ?? '')) bad.push(`unstyled control: ${el.outerHTML.slice(0, 120)}`);
  }
  return bad;
 });
 expect(offenders).toEqual([]);
}
