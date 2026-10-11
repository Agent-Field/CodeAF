import { expect, test, type Page } from '@playwright/test';

const url = (fixture: string) => `/?specimen=places-organize&fixture=${fixture}`;

async function paint(page: Page, name: string) {
  return page.evaluate(token => {
    const probe = document.createElement('span');
    probe.style.color = `var(--${token})`;
    document.body.append(probe);
    const value = getComputedStyle(probe).color;
    probe.remove();
    return value;
  }, name);
}

test.describe('places organize', () => {
  test('idle shows Organize now', async ({ page }) => {
    await page.goto(url('idle-empty'));
    await expect(page.getByRole('button', { name: 'Organize now' })).toBeVisible();
    await expect(page.getByRole('radio', { name: "Add to what's there" })).toBeChecked();
    await expect(page.getByRole('radio', { name: 'Start fresh' })).not.toBeChecked();
    await expect(page.locator('.places-organize-progress')).toHaveCount(0);
    await expect(page.locator('.places-organize-last')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Cancel' })).toHaveCount(0);
    await page.getByRole('radio', { name: 'Start fresh' }).click();
    await expect(page.getByRole('radio', { name: 'Start fresh' })).toBeChecked();
  });

  test('a last run is its sentence, and a cancelled run says Cancelled', async ({ page }) => {
    await page.goto(url('idle-summary'));
    await expect(page.locator('.places-organize-last')).toHaveText('12 new places, 340 chats filed');
    await page.goto(url('cancelled'));
    await expect(page.locator('.places-organize-last')).toHaveText('Cancelled');
  });

  for (const [fixture, line] of [
    ['running-reading', 'Reading 1,240 chats'],
    ['running-grouping', 'Grouping'],
    ['running-naming', 'Naming (12 of 30)'],
    ['running-checking', 'Checking'],
  ] as const) {
    test(`${fixture} shows ${line}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: 'light' });
      await page.goto(url(fixture));
      const progress = page.locator('.places-organize-progress');
      await expect(progress).toContainText(line);
      await expect(progress).toHaveCSS('color', await paint(page, 'ink-3'));
      await expect(progress).toHaveCSS('border-left-style', 'none');
      const dot = progress.locator('.status-mark-dot');
      const box = await dot.boundingBox();
      expect(box?.width).toBe(6);
      expect(box?.height).toBe(6);
      await expect(page.getByRole('button', { name: 'Organize now' })).toBeDisabled();
      await expect(page.getByRole('button', { name: 'Cancel' })).toBeEnabled();
      await page.getByRole('button', { name: 'Cancel' }).click();
      await expect(page.locator('[data-fixture]')).toHaveAttribute('data-cancelled', 'true');
    });
  }

  test('an unknown stage draws no sentence and still offers Cancel', async ({ page }) => {
    await page.goto(url('running-quiet'));
    await expect(page.locator('.places-organize-progress')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible();
  });

  test('dark progress stays ink-3 and the dot stays the running mark', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto(url('running-reading'));
    const progress = page.locator('.places-organize-progress');
    await expect(progress).toHaveCSS('color', await paint(page, 'ink-3'));
    await expect(progress.locator('.status-mark')).toHaveCSS('color', await paint(page, 'mark-running'));
  });

  test('the review sheet unchecks a row, Accept skips it, and Not now declines', async ({ page }) => {
    await page.goto(url('plan'));
    const sheet = page.getByRole('dialog', { name: 'Review' });
    await expect(sheet).toBeVisible();
    await expect(sheet).toContainText('3 places would go, 2 would be renamed, 410 chats would move');
    const rows = sheet.getByRole('checkbox');
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(1)).toHaveText('Rename to “Desk”');
    await expect(rows.nth(0)).toHaveAttribute('aria-checked', 'true');
    const words = await rows.nth(0).evaluate(node => getComputedStyle(node).color);
    await rows.nth(0).hover();
    await expect(rows.nth(0)).toHaveCSS('color', words);
    await expect(rows.nth(0)).toHaveCSS('background-color', await paint(page, 'field-2'));
    await rows.nth(1).click();
    await expect(rows.nth(1)).toHaveAttribute('aria-checked', 'false');
    await sheet.getByRole('button', { name: 'Accept' }).click();
    await expect(page.locator('[data-fixture]')).toHaveAttribute('data-skip', '1');
    await page.goto(url('plan'));
    await page.getByRole('dialog').getByRole('button', { name: 'Not now' }).click();
    await expect(page.locator('[data-fixture]')).toHaveAttribute('data-declined', 'true');
    await expect(page.getByRole('dialog')).toHaveCount(0);
  });

  test('Escape closes the sheet without deciding, and Review opens it again', async ({ page }) => {
    await page.goto(url('plan'));
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.locator('[data-declined]')).toHaveCount(0);
    await page.getByRole('button', { name: 'Review' }).click();
    await expect(page.getByRole('dialog', { name: 'Review' })).toBeVisible();
  });

  test('keyboard focus draws the ring and a pointer press does not', async ({ page }) => {
    await page.goto(url('idle-empty'));
    await page.keyboard.press('Tab');
    const button = page.getByRole('button', { name: 'Organize now' });
    await expect(button).toBeFocused();
    const ring = await button.evaluate(node => getComputedStyle(node).boxShadow);
    expect(ring).not.toBe('none');
    await button.dispatchEvent('pointerdown');
    await button.focus();
    await expect(button).toHaveCSS('box-shadow', 'none');
  });

  test('Undo is offered after an accept and a missing route draws the sentence', async ({ page }) => {
    await page.goto(url('accepted'));
    await page.getByRole('button', { name: 'Undo' }).click();
    await expect(page.locator('[data-fixture]')).toHaveAttribute('data-undone', 'true');
    await page.goto(url('unavailable'));
    await expect(page.getByText('Organizing places is not available from this engine yet.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Organize now' })).toHaveCount(0);
  });
});
