import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

// SH-011: Tauri 2 drags only on the exact target, so the empty strip space is its own element and nothing around it may be no-drag.
test('the empty strip space is a drag region outside every no-drag element', async ({ page, browserName }) => {
  await page.goto('/');
  const spacer = page.locator('.workspace-tab-spacer');
  await expect(spacer).toHaveAttribute('data-tauri-drag-region', 'true');
  const box = await spacer.boundingBox();
  expect(box!.width).toBeGreaterThan(0);
  const noDragAncestor = await spacer.evaluate(el => {
    for (let n: Element | null = el; n; n = n.parentElement) if (getComputedStyle(n).getPropertyValue('-webkit-app-region') === 'no-drag') return true;
    return false;
  });
  expect(noDragAncestor).toBe(false);
  // WebKit does not expose -webkit-app-region through computed style, so the no-drag half is proved on chromium.
  test.skip(browserName !== 'chromium', 'computed -webkit-app-region is chromium-only');
  for (const name of ['New tab', 'All tabs']) {
    const region = await page.getByRole('button', { name, exact: true }).evaluate(el => getComputedStyle(el).getPropertyValue('-webkit-app-region'));
    expect(region, name).toBe('no-drag');
  }
});
