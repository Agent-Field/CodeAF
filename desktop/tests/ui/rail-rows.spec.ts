import { test, expect } from '@playwright/test';
import { openPage } from './support/shell-navigation';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Shell 3a, Components C-PLACE-2/5: the Now row in rest, hover, selected and needs-you, light and dark.
const px = (name: string) => parseFloat(design.foundation[name as keyof typeof design.foundation] as string);
const rows = (page: import('@playwright/test').Page) => page.locator('[data-rail-rows-specimen] .nav-item');

for (const scheme of ['light', 'dark'] as const) {
  test(`Now row states, ${scheme}`, async ({ page }) => {
    await page.route('**/api/engine/**', route => route.abort());
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/');
    await openPage(page, 'Design system');
    const all = rows(page);
    await expect(all).toHaveCount(5);
    const [rest, hover, selected, needs, none] = [0, 1, 2, 3, 4].map(i => all.nth(i));
    expect((await rest.boundingBox())!.height).toBe(px('rail-row-height'));
    const fill = (row: typeof rest) => row.evaluate(el => getComputedStyle(el).backgroundColor);
    const tabHover = await page.evaluate(() => { const probe = document.createElement('div'); probe.style.background = 'var(--tab-hover)'; document.body.append(probe); const c = getComputedStyle(probe).backgroundColor; probe.remove(); return c; });
    expect(await fill(rest)).toBe('rgba(0, 0, 0, 0)');
    expect(await fill(hover)).toBe(tabHover);
    expect(await selected.evaluate(el => getComputedStyle(el).fontWeight)).toBe('500');
    expect(await selected.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
    // The count is ink-3 caption type and the amber dot is 6px; zero running draws neither.
    await expect(rest.locator('[data-rail-count]')).toHaveText('3');
    await expect(none.locator('[data-rail-count]')).toHaveCount(0);
    await expect(none.locator('.status-mark')).toHaveCount(0);
    const dot = needs.locator('.status-mark').first();
    expect((await dot.boundingBox())!.width).toBe(px('mark-dot'));
  });
}
