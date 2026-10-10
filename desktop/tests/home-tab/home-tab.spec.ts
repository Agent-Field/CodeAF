import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from '../ui/contracts';

// Places 6a (rest and active Home), 9c (switcher), Components "Home tab as switcher", Interactions Home tab (pinned).
// Numbers are the design's measured chip, compared with tokens so light and dark stay one source.

const cell = (page: Page, variant: string) => page.locator(`[data-variant="${variant}"]`);
const tab = (page: Page, variant: string) => cell(page, variant).locator('.home-tab');
const button = (page: Page, variant: string) => cell(page, variant).locator('.home-tab-select');

async function open(page: Page, theme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto('/');
  await page.getByTestId('home-tab-harness').waitFor();
}

async function boxShadow(page: Page, css: string) {
  return page.evaluate(value => {
    const probe = document.createElement('div');
    probe.style.boxShadow = value;
    document.body.append(probe);
    const shadow = getComputedStyle(probe).boxShadow;
    probe.remove();
    return shadow;
  }, css);
}

async function duration(page: Page, token: string) {
  return page.evaluate(name => {
    const probe = document.createElement('div');
    probe.style.transitionDuration = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe).transitionDuration;
    probe.remove();
    return value;
  }, token);
}

async function near(locator: Locator, axis: 'width' | 'height', px: number) {
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  expect(Math.abs((axis === 'width' ? box!.width : box!.height) - px)).toBeLessThan(0.6);
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test('rest, active, hover, press and keyboard focus match the chip', async ({ page }) => {
      await open(page, theme);
      const ink = await tokenColor(page, 'ink');
      const ink2 = await tokenColor(page, 'ink-2');
      const canvas = await tokenColor(page, 'canvas');
      const hover = await tokenColor(page, 'tab-hover');
      const pressed = await tokenColor(page, 'field-2');
      const lift = await boxShadow(page, 'var(--sh-1)');
      const ring = await boxShadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)');

      const rest = tab(page, 'rest');
      await expect(rest).toHaveCSS('height', '30px');
      await expect(rest).toHaveCSS('border-top-left-radius', '8px');
      await expect(rest).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      await expect(rest).toHaveCSS('color', ink2);
      await expect(rest).toHaveCSS('box-shadow', 'none');
      const restButton = button(page, 'rest');
      await expect(restButton).toHaveCSS('padding-left', '10px');
      await expect(restButton).toHaveCSS('padding-right', '10px');
      await expect(restButton).toHaveCSS('gap', '8px');
      await expect(restButton).toHaveCSS('font-size', '12px');
      await expect(restButton).toHaveCSS('font-weight', '500');
      const swatch = rest.locator('.place-swatch');
      await near(swatch, 'width', 10);
      await near(swatch, 'height', 10);
      await expect(swatch).toHaveCSS('border-top-left-radius', '3px');
      await expect(rest.locator('.home-tab-switcher')).toHaveCount(0);
      await expect(rest.locator('.home-tab-alert')).toHaveCount(0);
      await expect(cell(page, 'rest').getByRole('button', { name: /close/i })).toHaveCount(0);

      const hairline = cell(page, 'rest').locator('.home-tab-hairline');
      await near(hairline, 'width', 1);
      await near(hairline, 'height', 16);
      await expect(hairline).toHaveCSS('margin-left', '6px');
      await expect(hairline).toHaveCSS('margin-right', '6px');
      await expect(hairline).toHaveCSS('background-color', await tokenColor(page, 'line'));
      const gap = await cell(page, 'rest').evaluate(root => {
        const chip = root.querySelector('.home-tab')!.getBoundingClientRect();
        const rule = root.querySelector('.home-tab-hairline')!.getBoundingClientRect();
        return rule.left - chip.right;
      });
      expect(Math.abs(gap - 8)).toBeLessThan(0.6);

      const active = tab(page, 'active');
      await expect(active).toHaveCSS('background-color', canvas);
      await expect(active).toHaveCSS('color', ink);
      await expect(active).toHaveCSS('box-shadow', lift);
      await expect(button(page, 'active')).toHaveAttribute('aria-selected', 'true');

      await expect(tab(page, 'hover')).toHaveCSS('background-color', hover);
      await expect(tab(page, 'hover')).toHaveCSS('color', ink);
      const pressDuration = await duration(page, 'dur-press');
      await expect(tab(page, 'pressed')).toHaveCSS('background-color', pressed);
      await expect(tab(page, 'pressed')).toHaveCSS('transition-duration', pressDuration);
      await expect(button(page, 'focus')).toHaveCSS('box-shadow', ring);

      await rest.hover();
      await expect(rest).toHaveCSS('background-color', hover);
      await expect(rest).toHaveCSS('color', ink);
      const box = await restButton.boundingBox();
      await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
      await page.mouse.down();
      await expect(rest).toHaveCSS('background-color', pressed);
      await expect(rest).toHaveCSS('transition-duration', pressDuration);
      await page.mouse.up();

      // Active keeps the canvas lift under the pointer. Press replaces the fill and leaves the lift.
      await active.hover();
      await expect(active).toHaveCSS('background-color', canvas);
      await expect(active).toHaveCSS('box-shadow', lift);
      const activeBox = await button(page, 'active').boundingBox();
      await page.mouse.move(activeBox!.x + 8, activeBox!.y + 8);
      await page.mouse.down();
      await expect(active).toHaveCSS('background-color', pressed);
      await expect(active).toHaveCSS('box-shadow', lift);
      await page.mouse.up();
    });

    test('collapsed rail: chevron opens the switcher, amber sits on the swatch, right-click is the place menu', async ({ page }) => {
      await open(page, theme);
      const ink3 = await tokenColor(page, 'ink-3');
      const amber = await tokenColor(page, 'amber');
      const switcher = tab(page, 'switcher');
      const chevron = switcher.locator('.home-tab-switcher .app-icon');
      await near(chevron, 'width', 12);
      await near(chevron, 'height', 12);
      await expect(chevron).toHaveAttribute('data-icon', 'switcher');
      await expect(switcher.locator('.home-tab-switcher')).toHaveCSS('color', ink3);
      await expect(button(page, 'switcher')).toHaveCSS('padding-left', '10px');
      await expect(button(page, 'switcher')).toHaveCSS('padding-right', '6px');
      await expect(button(page, 'switcher')).toHaveAttribute('aria-haspopup', 'menu');
      await expect(switcher.locator('.home-tab-alert')).toHaveCount(0);

      await button(page, 'switcher').click();
      await expect(page.getByRole('list', { name: 'Callback log' })).toHaveText('switcher');

      const alert = tab(page, 'alert');
      const dot = alert.locator('.home-tab-alert');
      await near(dot, 'width', 6);
      await near(dot, 'height', 6);
      await expect(dot).toHaveCSS('background-color', amber);
      await expect(button(page, 'alert')).toHaveAttribute('aria-description', 'Needs you in Config parser');
      const place = await alert.evaluate(root => {
        const swatch = root.querySelector('.place-swatch')!.getBoundingClientRect();
        const mark = root.querySelector('.home-tab-alert')!.getBoundingClientRect();
        return { right: mark.right - swatch.right, bottom: mark.bottom - swatch.bottom };
      });
      expect(Math.abs(place.right - 3)).toBeLessThan(0.6);
      expect(Math.abs(place.bottom - 3)).toBeLessThan(0.6);
      await expect(tab(page, 'quiet').locator('.home-tab-alert')).toHaveCount(0);

      await tab(page, 'rest').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Rename' }).click();
      await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('menu:rename');
      await expect(page.locator('.home-tab').getByRole('button', { name: /close/i })).toHaveCount(0);

      await page.keyboard.press('Tab');
      await expect(button(page, 'keyboard')).toBeFocused();
      await expect(button(page, 'keyboard')).toHaveCSS('box-shadow', await boxShadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)'));
      await button(page, 'keyboard').click();
      await expect(button(page, 'keyboard')).toHaveCSS('box-shadow', 'none');

      await expectAccessible(page);
    });
  });
}
