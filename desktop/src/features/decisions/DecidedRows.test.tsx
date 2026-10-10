import { expect, test, type Locator, type Page } from '@playwright/test';

async function tokenColor(page: Page, token: string) {
  return page.evaluate(name => {
    const probe = document.createElement('span');
    probe.style.color = `var(--${name})`;
    document.body.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, token);
}

async function open(page: Page, theme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto('/');
  await page.getByTestId('decided-home').getByRole('heading', { name: 'Decided automatically · 9' }).waitFor();
}

const home = (page: Page) => page.getByTestId('decided-home');
const row = (page: Page, id: string) => home(page).locator(`[data-decided-id="${id}"]`);
const main = (locator: Locator) => locator.locator('.decided-main');

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test('label, All, and the three newest rows match 12a', async ({ page }) => {
      await open(page, theme);
      const ink = await tokenColor(page, 'ink');
      const ink2 = await tokenColor(page, 'ink-2');
      const ink3 = await tokenColor(page, 'ink-3');
      const label = home(page).getByRole('heading', { name: 'Decided automatically · 9' });
      await expect(label).toHaveCSS('font-size', '11px');
      await expect(label).toHaveCSS('font-weight', '500');
      await expect(label).toHaveCSS('color', ink3);
      const all = home(page).getByRole('button', { name: 'All', exact: true });
      await expect(all).toHaveCSS('font-size', '12px');
      await expect(all).toHaveCSS('font-weight', '400');
      await expect(all).toHaveCSS('color', ink2);
      const labelBox = await label.boundingBox();
      const allBox = await all.boundingBox();
      expect(allBox!.x).toBeGreaterThan(labelBox!.x + labelBox!.width);

      const ids = await home(page).locator('[data-decided-id]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-decided-id')));
      expect(ids).toEqual(['staging', 'strict', 'images']);
      await expect(home(page).getByText('Filed the brand note')).toHaveCount(0);

      const staging = row(page, 'staging');
      const button = main(staging);
      await expect(button).toHaveCSS('min-height', '40px');
      await expect(button).toHaveCSS('padding-top', '4px');
      await expect(button).toHaveCSS('padding-bottom', '4px');
      await expect(button).toHaveCSS('padding-left', '12px');
      await expect(button).toHaveCSS('padding-right', '12px');
      await expect(button).toHaveCSS('border-top-left-radius', '9px');
      await expect(button).toHaveCSS('column-gap', '10px');
      const columns = await button.evaluate(element => getComputedStyle(element).gridTemplateColumns);
      expect(columns.startsWith('16px')).toBe(true);
      const box = await button.boundingBox();
      expect(Math.round(box!.height)).toBe(48);

      await expect(staging.locator('.decided-title')).toHaveText('Allowed publishing to staging');
      await expect(staging.locator('.decided-title')).toHaveCSS('font-size', '13px');
      await expect(staging.locator('.decided-title')).toHaveCSS('font-weight', '400');
      await expect(staging.locator('.decided-title')).toHaveCSS('color', ink);
      await expect(staging.locator('.decided-sub')).toHaveText('Launch post · you allowed it twice');
      await expect(staging.locator('.decided-sub')).toHaveCSS('font-size', '11px');
      await expect(staging.locator('.decided-sub')).toHaveCSS('color', ink3);
      await expect(staging.locator('.decided-age')).toHaveText('1h');
      await expect(staging.locator('.decided-age')).toHaveCSS('font-size', '11px');
      await expect(staging.locator('.decided-age')).toHaveCSS('color', ink3);
      const check = staging.locator('.app-icon');
      await expect(check).toHaveAttribute('data-icon', 'check');
      await expect(check).toHaveCSS('width', '12px');
      await expect(check).toHaveCSS('height', '12px');
      await expect(check).toHaveCSS('color', ink3);

      await expect(page.getByTestId('decided-empty')).toHaveText('');
      const sparse = page.getByTestId('decided-sparse');
      await expect(sparse.locator('.decided-title')).toHaveText('Kept the note');
      await expect(sparse.locator('.decided-sub')).toHaveCount(0);
      await expect(sparse.locator('.decided-age')).toHaveCount(0);
      await expect(sparse.getByRole('button', { name: 'All', exact: true })).toHaveCount(0);
    });

    test('hover fills, press darkens, and keyboard focus draws the ring', async ({ page }) => {
      await open(page, theme);
      const field = await tokenColor(page, 'field');
      const field2 = await tokenColor(page, 'field-2');
      const hover = page.getByTestId('state-hover').locator('.decided-main');
      const pressed = page.getByTestId('state-pressed').locator('.decided-main');
      await expect(hover).toHaveCSS('background-color', field);
      expect(await hover.evaluate(element => getComputedStyle(element).transitionDuration)).toContain('0.12s');
      await expect(pressed).toHaveCSS('background-color', field2);
      expect(await pressed.evaluate(element => getComputedStyle(element).transitionDuration)).toContain('0.08s');
      const ring = await page.getByTestId('state-focus').locator('.decided-main').evaluate(element => getComputedStyle(element).boxShadow);
      expect(ring).toContain('2px');
      await row(page, 'staging').locator('.decided-main').hover();
      await expect(main(row(page, 'staging'))).toHaveCSS('background-color', field);
    });

    test('a row opens Why?, All opens the rest, Show fewer folds it', async ({ page }) => {
      await open(page, theme);
      const staging = main(row(page, 'staging'));
      await expect(staging).toHaveAttribute('aria-expanded', 'false');
      await staging.click();
      const why = home(page).getByRole('dialog', { name: 'Why?' });
      await expect(why).toBeVisible();
      await expect(why.locator('.decided-why-title')).toHaveText('Decided automatically');
      await expect(why).toContainText('By');
      await expect(why).toContainText('Launch post');
      await expect(why).toContainText('Because');
      await expect(why).toContainText('You allowed it twice.');
      await expect(why).toContainText('Sure');
      await expect(why).toContainText('97%');
      await expect(why).toHaveCSS('width', '360px');
      await expect(why).toHaveCSS('padding-top', '14px');
      await expect(why).toHaveCSS('padding-left', '16px');
      await expect(why).toHaveCSS('border-top-left-radius', '12px');
      await expect(why.locator('.decided-why-title')).toHaveCSS('font-size', '13px');
      await expect(why.locator('.decided-why-title')).toHaveCSS('font-weight', '600');
      await expect(why.locator('.decided-why-grid')).toHaveCSS('font-size', '12px');
      await expect(why.locator('.decided-why-key').first()).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('staging');
      await page.keyboard.press('Escape');
      await expect(why).toHaveCount(0);
      await expect(staging).toBeFocused();

      await home(page).getByRole('button', { name: 'All', exact: true }).click();
      const ids = await home(page).locator('[data-decided-id]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-decided-id')));
      expect(ids).toEqual(['staging', 'strict', 'images', 'note', 'price', 'suite', 'hero', 'tone', 'seat']);
      await home(page).getByRole('button', { name: 'Show fewer' }).click();
      await expect(home(page).locator('[data-decided-id]')).toHaveCount(3);

      await page.getByTestId('decided-custom').locator('.decided-main').click();
      const custom = page.getByTestId('decided-custom').getByRole('dialog', { name: 'Why?' });
      await expect(custom).toContainText('Open the decision');
      await expect(custom.locator('.decided-why-title')).toHaveCount(0);
      await page.mouse.click(8, 8);
      await expect(custom).toHaveCount(0);
    });

    test('Up and Down move between the rows', async ({ page }) => {
      await open(page, theme);
      const first = main(row(page, 'staging'));
      await first.focus();
      await page.keyboard.press('ArrowDown');
      await expect(main(row(page, 'strict'))).toBeFocused();
      await page.keyboard.press('ArrowDown');
      await expect(main(row(page, 'images'))).toBeFocused();
      await page.keyboard.press('ArrowUp');
      await expect(main(row(page, 'strict'))).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(home(page).getByRole('dialog', { name: 'Why?' })).toContainText('Config parser');
      await expect(home(page).getByRole('dialog', { name: 'Why?' })).toContainText('You allowed go test here 6 times. Reversible.');
    });
  });
}
