import { expect, test, type Page } from '@playwright/test';
import design from '../../design/tokens.json' with { type: 'json' };

const url = (query = '') => `/?specimen=queue-popover${query}`;
const pill = (page: Page) => page.locator('.frame-pill');
const pop = (page: Page) => page.getByRole('group', { name: /need you/ });

async function paint(page: Page, name: string, property: 'color' | 'backgroundColor' | 'boxShadow') {
  return page.evaluate(({ name, property }) => {
    const probe = document.createElement('span');
    probe.style[property] = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe)[property];
    probe.remove();
    return value;
  }, { name, property });
}

test.describe('queue popover', () => {
  for (const scheme of ['light', 'dark'] as const) {
    test(`${scheme}: matches the measured card`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(url('&open=1'));
      const card = pop(page);
      await expect(card).toBeVisible();
      await expect(card).toHaveCSS('width', design.foundation['i2-queue-pop-width']);
      await expect(card).toHaveCSS('border-radius', design.foundation['i2-queue-pop-radius']);
      await expect(card).toHaveCSS('padding-top', design.foundation['i2-queue-pop-pad']);
      await expect(card).toHaveCSS('background-color', await paint(page, 'surface', 'backgroundColor'));
      await expect(card).toHaveCSS('box-shadow', await paint(page, 'sh-2', 'boxShadow'));

      await expect(card.locator('.queue-popover-need')).toHaveText('5 need you');
      await expect(card.locator('.queue-popover-need')).toHaveCSS('font-weight', '600');
      await expect(card.locator('.queue-popover-where')).toHaveText('in other conversations · 3 places');
      await expect(card.locator('.queue-popover-where')).toHaveCSS('color', await paint(page, 'ink-3', 'color'));

      const row = card.locator('.queue-popover-row').first();
      await expect(row).toHaveCSS('padding-top', '8px');
      await expect(row).toHaveCSS('padding-left', '10px');
      await expect(row).toHaveCSS('border-radius', '8px');
      await expect(row.locator('.queue-popover-ask')).toHaveCSS('font-size', '13px');
      await expect(row.locator('.queue-popover-sub')).toHaveText('Codeaf · Config parser');
      await expect(row.locator('.queue-popover-sub')).toHaveCSS('font-size', '11px');
      await expect(row.locator('.queue-popover-tag')).toHaveText('Blocking');
      const tint = await row.locator('.place-swatch').boundingBox();
      expect([tint?.width, tint?.height]).toEqual([8, 8]);
      await expect(row.locator('.place-swatch')).toHaveCSS('border-radius', '3px');

      const start = card.getByRole('button', { name: /^Start/ });
      await expect(start).toHaveCSS('height', '28px');
      await expect(start).toHaveCSS('background-color', await paint(page, 'accent', 'backgroundColor'));
      const accept = card.getByRole('button', { name: 'Accept 3 suggestions' });
      await expect(accept).toHaveCSS('background-color', await paint(page, 'field', 'backgroundColor'));
    });
  }

  test('opens on hover after the delay and closes on leave', async ({ page }) => {
    await page.goto(url());
    await expect(pop(page)).toHaveCount(0);
    await pill(page).hover();
    await expect(pop(page)).toBeVisible();
    await page.mouse.move(0, 0);
    await expect(pop(page)).toHaveCount(0);
  });

  test('opens on keyboard focus and Esc closes it', async ({ page }) => {
    await page.goto(url());
    await pill(page).focus();
    await expect(pop(page)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(pop(page)).toHaveCount(0);
    await expect(pill(page)).toBeFocused();
  });

  test('row hover is a fill change and nothing else', async ({ page }) => {
    await page.goto(url('&open=1'));
    const row = pop(page).locator('.queue-popover-row').nth(1);
    const ink = await row.locator('.queue-popover-ask').evaluate(el => getComputedStyle(el).color);
    await row.hover();
    await expect(row).toHaveCSS('background-color', await paint(page, 'field', 'backgroundColor'));
    await expect(row.locator('.queue-popover-ask')).toHaveCSS('color', ink);
  });

  test('arrows move between rows and Enter jumps to that item', async ({ page }) => {
    await page.goto(url());
    await pill(page).focus();
    await page.keyboard.press('ArrowDown');
    await expect(pop(page).locator('.queue-popover-row').first()).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(pop(page).locator('.queue-popover-row').nth(1)).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');
    await expect(page.locator('body')).toHaveAttribute('data-opened', 'jump:b');
    await expect(pop(page)).toHaveCount(0);
  });

  test('Start and Accept call their handlers', async ({ page }) => {
    await page.goto(url('&open=1'));
    await pop(page).getByRole('button', { name: /^Start/ }).click();
    await expect(page.locator('body')).toHaveAttribute('data-opened', 'start');
    await page.goto(url('&open=1'));
    await pop(page).getByRole('button', { name: /^Accept/ }).click();
    await expect(page.locator('body')).toHaveAttribute('data-opened', 'accept');
  });

  test('no Accept button when nothing is acceptable, and no popover when nothing waits', async ({ page }) => {
    await page.goto(url('&open=1&accept=0'));
    await expect(pop(page).getByRole('button', { name: /^Accept/ })).toHaveCount(0);
    await expect(pop(page).getByRole('button', { name: /^Start/ })).toBeVisible();
    await page.goto(url('&open=1&n=0'));
    await expect(pop(page)).toHaveCount(0);
  });
});
