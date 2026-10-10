import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test(`d5-pl-test-using: design geometry, real policy words and source glyphs (${theme})`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=decided&add=1');
    const chip = page.getByRole('button', { name: /^Using/ });
    await chip.click();
    const popover = page.getByRole('dialog', { name: 'What this conversation is using' });
    await expect(popover).toHaveCSS('width', '380px');
    await expect(popover).toHaveCSS('padding', '6px');
    await expect(popover).toHaveCSS('border-radius', '12px');
    await expect(popover.getByRole('heading', { level: 3 })).toHaveText(['Places', 'Instructions', 'Sources', 'Policy']);
    await expect(popover.getByText('codeaf · inherited')).toBeVisible();
    await expect(popover.getByText('Release wanted Flash · codeaf decided')).toBeVisible();
    await expect(popover.getByText('Model: Pro')).toBeVisible();
    await expect(popover.getByText('1 source trimmed')).toHaveCSS('color', await popover.locator('.using-label').first().evaluate(el => getComputedStyle(el).color));
    for (const [key, icon] of [['repo:/work/codeaf/internal/parse', 'folderGit'], ['file:/work/brand-voice.md', 'fileText'], ['url:https://codeaf.dev/changelog', 'web']]) {
      await expect(popover.locator(`[data-source-key="${key}"] [data-icon="${icon}"]`)).toBeVisible();
      await expect(popover.locator(`[data-source-key="${key}"] [data-icon="${icon}"]`)).toHaveCSS('width', '13px');
    }
    await page.keyboard.press('Escape');
    await expect(popover).toHaveCount(0);
    await expect(chip).toBeFocused();
  });

  for (const width of [320, 480, 600, 800, 1200]) {
    test(`d5-pl-test-using: ${width}px layout (${theme})`, async ({ page }) => {
      await page.setViewportSize({ width, height: 560 });
      await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
      await page.goto('/?scenario=decided&add=1');
      await page.getByRole('button', { name: /^Using/ }).click();
      const popover = page.getByRole('dialog');
      const rect = (await popover.boundingBox())!;
      expect(rect.x).toBeGreaterThanOrEqual(0);
      expect(rect.x + rect.width).toBeLessThanOrEqual(width);
      expect(rect.y + rect.height).toBeLessThanOrEqual(560);
      if (width <= 600) {
        expect(rect.width).toBe(width);
        expect(rect.y + rect.height).toBe(560);
        await expect(popover).toHaveCSS('border-bottom-left-radius', '0px');
      }
      await popover.getByRole('button', { name: 'Add to a place…' }).click();
      await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('add-to-place');
    });
  }
}

test('d5-pl-test-using: pickPlace adds membership and cancellation writes nothing', async ({ page }) => {
  await page.goto('/?scenario=quiet&membership=1');
  await page.getByRole('button', { name: /^Using/ }).click();
  await page.getByRole('button', { name: 'Add to a place…' }).click();
  const pick = await page.evaluate(() => window.__usingPick?.());
  expect(pick).toEqual({ title: 'Add to a place', exclude: ['pl_parser', 'pl_release'] });
  await page.evaluate(() => window.__usingSettle?.());
  await expect(page.getByRole('list', { name: 'Callback log' }).locator('li')).toHaveCount(0);
  await page.getByRole('button', { name: /^Using/ }).click();
  await page.getByRole('button', { name: 'Add to a place…' }).click();
  await page.evaluate(() => window.__usingSettle?.('pl_marketing'));
  await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('membership:pl_marketing:chat-mock-1757');
});

test('d5-pl-test-using: repeated file drops use the chat attachment transport, never membership', async ({ page }) => {
  await page.goto('/?scenario=quiet&attach=1&membership=1');
  await page.getByRole('button', { name: /^Using/ }).click();
  const popover = page.getByRole('dialog');
  for (const name of ['one.txt', 'two.txt']) {
    await popover.evaluate((node, fileName) => {
      const dataTransfer = new DataTransfer();
      dataTransfer.items.add(new File(['chat context'], fileName, { type: 'text/plain' }));
      node.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer }));
    }, name);
    await expect(page.locator('.attachment-tray')).toContainText(name);
  }
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByRole('list', { name: 'Callback log' })).toContainText('send:one.txt,two.txt');
  await expect(page.locator('.attachment-tray')).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Callback log' })).not.toContainText('membership:');
});
