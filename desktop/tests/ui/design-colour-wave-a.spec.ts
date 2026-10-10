import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openPage } from './support/shell-navigation';

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: Design system tint swatches, materials and selected text follow Foundations`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await page.goto('/');
  await openPage(page, 'Design system');
  await page.getByRole('radio', { name: 'Rose', exact: true }).click();
  const specimen = page.locator('.controls-specimen[data-tint="rose"]');
  await expect(specimen).toBeVisible();
  const values = await specimen.evaluate(el => {
   const style = getComputedStyle(el);
   const text = el.querySelector('.controls-specimen-label')!;
   const range = document.createRange();
   range.selectNodeContents(text);
   window.getSelection()!.removeAllRanges();
   window.getSelection()!.addRange(range);
   // Resolve custom colour roles through CSS so both engines compare their own serialization.
   const probe = document.createElement('span');
   el.appendChild(probe);
   const resolve = (role: string) => {
    probe.style.backgroundColor = `var(--${role})`;
    return getComputedStyle(probe).backgroundColor;
   };
   const result = {
    swatch: style.getPropertyValue('--swatch').trim(),
    glass: style.getPropertyValue('--frame-glass').trim(),
    blur: style.getPropertyValue('--frame-blur').trim(),
    saturation: style.getPropertyValue('--frame-saturate').trim(),
    tab: style.getPropertyValue('--tab').trim(),
    scrim: resolve('scrim'), palette: resolve('palette-backdrop'), quicklook: resolve('places-quicklook-scrim'),
    selection: getComputedStyle(text, '::selection').backgroundColor,
    accentSoft: resolve('accent-soft'), selected: window.getSelection()!.toString(),
    width: el.getBoundingClientRect().width,
   };
   probe.remove();
   return result;
  });
  expect(values.swatch).toBe('oklch(.6 .13 12)');
  expect(values.glass).toBe(theme === 'light' ? 'oklch(.93 .035 12 / .8)' : 'oklch(.27 .035 12 / .84)');
  expect(values.tab).toBe(theme === 'light' ? 'oklch(1 0 0 / .55)' : 'oklch(1 0 0 / .09)');
  expect(values.blur).toBe('40px');
  expect(values.saturation).toBe('1.4');
  expect(values.scrim).toMatch(theme === 'light' ? /\/ 0\.2\)/ : /\/ 0\.4\)/);
  expect(values.palette).toBe(values.scrim);
  expect(values.quicklook).toMatch(/\/ 0\.2\)/);
  expect(values.selection).toBe(values.accentSoft);
  expect(values.selected).toBe('Colour roles');
  expect(values.width).toBeGreaterThan(0);
 });
}
