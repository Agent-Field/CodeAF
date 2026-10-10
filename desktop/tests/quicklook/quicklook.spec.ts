import { test, expect, type Page } from '@playwright/test';

async function open(page: Page, theme = 'light', empty = false) {
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await page.goto(empty ? '/?empty' : '/');
  const tile = page.locator('[data-place-id="reading"] .places-tile-main');
  await tile.focus();
  await page.keyboard.press('Space');
  const sheet = page.getByRole('dialog', { name: 'Quick Look: Reading' });
  await expect(sheet).toBeVisible();
  return { tile, sheet };
}

for (const theme of ['light', 'dark']) {
  test(`${theme}: measured Home sheet is read-only and does not change the window`, async ({ page }) => {
    const { sheet } = await open(page, theme);
    await expect(sheet).toHaveCSS('width', '520px');
    await expect(sheet).toHaveCSS('border-radius', '14px');
    await expect(sheet.locator('.home-quicklook-body')).toHaveCSS('padding', '22px 24px 18px');
    await expect(sheet.locator('.home-quicklook-body')).toHaveCSS('gap', '14px');
    await expect(sheet.locator('.home-quicklook-title')).toHaveCSS('font-size', '20px');
    await expect(sheet.locator('.home-quicklook-hint')).toHaveText('Space to close');
    await expect(sheet.locator('.place-swatch')).toHaveCSS('width', '14px');
    const styles = await sheet.evaluate(el => ({
      shadow: getComputedStyle(el).boxShadow,
      scrim: getComputedStyle(el, '::backdrop').backgroundColor,
    }));
    expect(styles.shadow).toContain('28px 70px -16px');
    expect(styles.scrim).toMatch(/0\.2\)/);
    await expect(sheet.locator('[data-chat-id]')).toHaveCount(3);
    await expect(sheet).not.toContainText('Chat 4');
    await expect(sheet.locator('.home-quicklook-recap')).toHaveText('You finished the Raft notes.');
    await expect(sheet.locator('.home-quicklook-places')).toHaveText('Places: Papers');
    await sheet.locator('[data-chat-id="chat-0"]').click({ force: true });
    await expect(page.getByRole('status', { name: 'Actions' })).toBeEmpty();
    await expect(page.locator('.home-page')).toContainText('All places');
    await expect(page.locator('[data-testid=window]')).toHaveAttribute('data-tint', 'graphite');
  });

  test(`${theme}: focus is trapped and Space/Escape restore the selected tile`, async ({ page }) => {
    const { sheet, tile } = await open(page, theme);
    await expect(sheet.getByRole('button', { name: 'Go to Reading' })).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(sheet.getByRole('button', { name: 'Open in new window' })).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(sheet.getByRole('button', { name: 'Go to Reading' })).toBeFocused();
    await page.keyboard.press('Space');
    await expect(sheet).toHaveCount(0);
    await expect(tile).toBeFocused();
    await expect(page.getByRole('status', { name: 'Actions' })).toBeEmpty();
    await page.keyboard.press('Space');
    await expect(sheet).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(sheet).toHaveCount(0);
    await expect(tile).toBeFocused();
  });
}

test('only the explicit footer actions navigate', async ({ page }) => {
  const { sheet, tile } = await open(page);
  await sheet.getByRole('button', { name: 'Go to Reading' }).click();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByRole('status', { name: 'Actions' })).toHaveText('go:reading');
  await tile.focus();
  await page.keyboard.press('Space');
  await sheet.getByRole('button', { name: 'Open in new window' }).click();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByRole('status', { name: 'Actions' })).toHaveText('go:reading,window:reading');
});

test('unknown recap, chats and child places draw nothing', async ({ page }) => {
  const { sheet } = await open(page, 'light', true);
  await expect(sheet.locator('.home-quicklook-recap, .places-chat-list, .home-quicklook-places')).toHaveCount(0);
});

test('sheet is full width at 600px and below in short windows', async ({ page }) => {
  const { sheet } = await open(page);
  for (const width of [600, 480, 320]) {
    await page.setViewportSize({ width, height: 360 });
    const rect = await sheet.boundingBox();
    expect(rect?.width).toBe(width);
    expect(rect?.x).toBe(0);
    expect(rect!.height).toBeLessThanOrEqual(328);
    await expect(sheet.getByRole('button', { name: 'Open in new window' })).toBeVisible();
  }
});

test('scrim dismissal restores focus without navigation', async ({ page }) => {
  const { sheet, tile } = await open(page);
  await page.mouse.click(10, 10);
  await expect(sheet).toHaveCount(0);
  await expect(tile).toBeFocused();
  await expect(page.getByRole('status', { name: 'Actions' })).toBeEmpty();
});

test('without navigation handlers the sheet keeps focus on itself', async ({ page }) => {
  await page.goto('/?bare');
  const tile = page.locator('[data-place-id="reading"] .places-tile-main');
  await tile.focus();
  await page.keyboard.press('Space');
  const sheet = page.getByRole('dialog', { name: 'Quick Look: Reading' });
  await expect(sheet).toBeVisible();
  await expect(sheet.locator('.home-quicklook-foot')).toHaveCount(0);
  await page.keyboard.press('Tab');
  await expect(sheet).toBeFocused();
  await page.keyboard.press('Space');
  await expect(sheet).toHaveCount(0);
  await expect(tile).toBeFocused();
});
