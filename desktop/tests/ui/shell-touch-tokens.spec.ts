import { expect, test } from '@playwright/test';

for (const theme of ['light', 'dark']) {
 test(`shell coarse-pointer tokens resolve in ${theme}`, async ({ page }) => {
  await page.goto('/');
  const measurements = await page.evaluate(theme => {
   document.documentElement.dataset.theme = theme;
   const names = ['hit-coarse-close', 'hit-coarse-row', 'pane-resize-hit-coarse'];
   return names.map(name => {
    const probe = document.createElement('div');
    probe.style.width = `var(--${name})`;
    probe.style.height = `var(--${name})`;
    document.body.append(probe);
    const result = {
     name,
     token: getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim(),
     width: probe.getBoundingClientRect().width,
     height: probe.getBoundingClientRect().height,
    };
    probe.remove();
    return result;
   });
  }, theme);
  expect(measurements).toEqual([
   { name: 'hit-coarse-close', token: '24px', width: 24, height: 24 },
   { name: 'hit-coarse-row', token: '40px', width: 40, height: 40 },
   { name: 'pane-resize-hit-coarse', token: '16px', width: 16, height: 16 },
  ]);
 });
}
