import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, INK3_TEXT, tokenColor } from '../ui/contracts';

// The harness composer is a stand-in, drawn in ink-3, the same waiver the Home suite uses.
INK3_TEXT.push('.home-specimen-composer', '.home-specimen-log');

// Places 8b trail, measured on the design screen: 12px ink-3 names, 6px gaps, 11px chevron-right.
// Interactions (Breadcrumb): click Go to; Command/Control-click and middle-click open a new window.
async function open(page: Page, theme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto('/?scenario=empty');
  await page.getByTestId('home-specimen').waitFor();
}

const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: All places › codeaf › Marketing goes to, opens a window, and marks the last name`, async ({ page }) => {
    await open(page, theme);
    const nav = page.getByRole('navigation', { name: 'Breadcrumb' });
    const names = nav.getByRole('button');
    await expect(names).toHaveText(['All places', 'codeaf', 'Marketing']);
    await expect(names.nth(2)).toHaveAttribute('aria-current', 'page');
    await expect(names.nth(0)).not.toHaveAttribute('aria-current', /.+/);
    await expect(names.nth(1)).not.toHaveAttribute('aria-current', /.+/);

    const ink3 = await tokenColor(page, 'ink-3');
    for (const name of ['All places', 'codeaf', 'Marketing']) {
      const button = nav.getByRole('button', { name, exact: true });
      await expect(button).toHaveCSS('font-size', '12px');
      await expect(button).toHaveCSS('font-weight', '400');
      await expect(button).toHaveCSS('color', ink3);
    }
    const chevrons = nav.locator('[data-icon="chevronRight"]');
    await expect(chevrons).toHaveCount(2);
    await expect(chevrons.first()).toHaveCSS('width', '11px');
    await expect(chevrons.first()).toHaveCSS('height', '11px');
    await expect(chevrons.first()).toHaveCSS('color', ink3);

    const spaces = await nav.locator('.places-crumbs').evaluate(ol => {
      const items = [...ol.querySelectorAll(':scope > li')];
      const box = (el: Element) => el.getBoundingClientRect();
      const gaps: number[] = [];
      for (let i = 1; i < items.length; i++) {
        const prev = box(items[i - 1].querySelector('button')!);
        const icon = box(items[i].querySelector('[data-icon="chevronRight"]')!);
        const next = box(items[i].querySelector('button')!);
        gaps.push(icon.x - (prev.x + prev.width), next.x - (icon.x + icon.width));
      }
      return gaps.map(gap => Math.round(gap * 100) / 100);
    });
    for (const gap of spaces) expect(gap).toBeCloseTo(6, 0);

    // Keyboard focus, before any pointer press, so the shared ring is allowed to show.
    await page.keyboard.press('Tab');
    const focused = page.locator('.places-crumb:focus');
    await expect(focused).toBeFocused();
    const ring = await focused.evaluate(el => {
      const style = getComputedStyle(el);
      return { shadow: style.boxShadow, ring: style.getPropertyValue('--focus-ring-width').trim(), halo: style.getPropertyValue('--focus-halo-width').trim() };
    });
    expect(ring.ring).toBe('2px');
    // The shared halo token is the total outer spread: the 2px ring sits inside it.
    expect(ring.halo).toBe('4px');
    expect(ring.shadow).toContain(ring.ring);
    expect(ring.shadow).toContain(ring.halo);

    await names.nth(2).hover();
    await expect(names.nth(2)).toHaveCSS('background-color', await tokenColor(page, 'field'));
    await expect(names.nth(2)).toHaveCSS('color', ink3);
    await expect(names.nth(2)).toHaveCSS('transition-duration', /0\.12s/);
    await page.mouse.down();
    await expect(names.nth(2)).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
    await expect(names.nth(2)).toHaveCSS('transition-duration', /0\.08s/);
    await page.mouse.up();

    // A pointer press focuses the control and must not draw the keyboard ring.
    expect(await names.nth(2).evaluate(el => getComputedStyle(el).boxShadow)).toBe('none');
    await expect(log(page).last()).toHaveText('goTo:pl_marketing');

    await names.nth(1).click({ modifiers: ['ControlOrMeta'] });
    await expect(log(page).last()).toHaveText('newWindow:pl_codeaf');
    await names.nth(0).dispatchEvent('auxclick', { button: 1 });
    await expect(log(page).last()).toHaveText('newWindow:root');
    await expectAccessible(page);
  });
}
