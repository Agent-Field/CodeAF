import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: Live rows match measured 12a and reopen detached work`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await page.goto('/?live=1');
    await expect(page.getByRole('heading', { name: 'Live', exact: true })).toHaveCSS('font-size', '11px');
    const council = page.locator('[data-attention-id="council"] button');
    await expect(council).toContainText('Marketing with Software · can the post promise trailing commas?');
    await expect(council.locator('.home-live-aside')).toHaveText('2 of 6 turns');
    await expect(council).toHaveCSS('min-height', '36px');
    await expect(council).toHaveCSS('border-radius', '9px');
    await expect(council).toHaveCSS('padding', '0px 12px');
    await expect(council).toHaveCSS('gap', '12px');
    await expect(council).toHaveCSS('font-size', '13px');
    await expect(council.locator('.status-mark')).toHaveCSS('width', '6px');
    await expect(council.locator('.ui-live-shimmer')).toHaveCSS('animation-duration', '2.4s');
    const publish = page.locator('[data-attention-id="publish"] button');
    await expect(publish).toContainText('Publish the launch post? · irreversible, always yours');
    await expect(publish.locator('.home-live-aside')).toHaveText('needs you');
    await expect(publish.locator('.ui-live-shimmer')).toHaveCount(0);
    const colors = await council.evaluate(row => {
      const s = getComputedStyle(row);
      return { ink: s.color, accent: s.getPropertyValue('--accent').trim(), dot: getComputedStyle(row.querySelector('.status-mark')!).color };
    });
    expect(colors.dot).not.toBe(colors.ink);
    await council.click();
    await expect(page.getByLabel('Workspace state')).toContainText('"active":"council"');
    await page.locator('[data-attention-id="closed"] button').click();
    await expect(page.getByLabel('Workspace state')).toContainText('"active":"closed"');
    await expect(page.getByLabel('Workspace state')).toContainText('"closed":0');
    await expect(page.getByLabel('Workspace state')).toContainText('"draft":"kept draft"');
    expect(JSON.parse((await page.getByLabel('Workspace state').textContent())!).ids.slice(1)).toEqual(['council', 'closed', 'publish']);
    await page.getByRole('button', { name: 'Clear fixture' }).click();
    await expect(page.getByRole('heading', { name: 'Live', exact: true })).toHaveCount(0);
  });
}

test('Live is keyboard accessible, quiet with reduced motion, and fits 320px', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 320, height: 560 });
  await page.goto('/?live=1');
  const row = page.locator('[data-attention-id="council"] button');
  await expect(row.locator('.ui-live-shimmer')).toHaveCSS('animation-name', 'none');
  await page.keyboard.press('Tab');
  await expect(row).toBeFocused();
  await expect(row).not.toHaveCSS('box-shadow', 'none');
  await page.keyboard.press('Enter');
  await expect(page.getByLabel('Workspace state')).toContainText('"active":"council"');
  expect(await row.evaluate(e => e.getBoundingClientRect().right)).toBeLessThanOrEqual(320);
  await page.goto('/?live=1&readonly=1');
  await expect(page.locator('.home-live-row:disabled')).toHaveCount(3);
});
