import { expect, test } from '@playwright/test';

// These probes exercise CSS variable resolution without changing the rail feature lane's controls.
for (const theme of ['light', 'dark']) {
  test(`place rail geometry resolves from tokens in ${theme}`, async ({ page }) => {
    await page.route('**/api/engine/**', route => route.abort());
    await page.goto('/');
    const measured = await page.evaluate(theme => {
      document.documentElement.dataset.theme = theme;
      const values: Record<string, string> = {};
      const probe = document.createElement('div');
      document.body.append(probe);
      for (const [name, property] of Object.entries({
        'rail-place-row-height': 'height',
        'rail-row-font': 'font-size',
        'rail-row-gap': 'row-gap',
        'rail-group-gap': 'row-gap',
        'places-swatch-rail': 'width',
        'rail-dot-size': 'width',
        'rail-meta-font': 'font-size',
        'rail-close-size': 'width',
        'rail-insertion-line': 'height',
        'rail-drag-ghost-opacity': 'opacity',
        'rail-switcher-width': 'width',
      })) {
        probe.style.cssText = `${property}: var(--${name})`;
        values[name] = getComputedStyle(probe).getPropertyValue(property);
      }
      probe.remove();
      return values;
    }, theme);
    expect(measured).toEqual({
      'rail-place-row-height': '32px',
      'rail-row-font': '13px',
      'rail-row-gap': '1px',
      'rail-group-gap': '16px',
      'places-swatch-rail': '10px',
      'rail-dot-size': '6px',
      'rail-meta-font': '11px',
      'rail-close-size': '20px',
      'rail-insertion-line': '2px',
      'rail-drag-ghost-opacity': '0.95',
      'rail-switcher-width': '280px',
    });
  });
}
