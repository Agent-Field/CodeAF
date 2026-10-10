import { test, expect, type Page } from '@playwright/test';

async function show(page: Page, options: { message?: string; durationMs?: number; lead?: string; undo?: boolean } = {}) {
  await page.evaluate(async options => {
    const path = '/src/components/ui/toastQueue.ts';
    const { toast } = await import(/* @vite-ignore */ path);
    toast.show({ lead: options.lead ?? 'dot', message: options.message ?? 'Config stack closed and still running', durationMs: options.durationMs ?? 6000,
      actions: [{ label: 'Stop it', kind: 'ghost', onAction: () => {} }],
      undo: options.undo === false ? undefined : () => {},
    });
  }, options);
}

for (const theme of ['light', 'dark']) {
  test(`toast geometry, actions and live region · ${theme}`, async ({ page }) => {
    await page.goto('/');
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
    await show(page);
    const region = page.locator('.toast-region');
    await expect(region).toHaveAttribute('role', 'status');
    await expect(region).toHaveAttribute('aria-live', 'polite');
    const geometry = await page.locator('.toast').evaluate(el => {
      const s = getComputedStyle(el), r = el.getBoundingClientRect();
      return { height: r.height, radius: s.borderRadius, gap: s.gap, padding: s.padding, width: r.width, x: r.x };
    });
    expect(geometry.height).toBe(40);
    expect(geometry.radius).toBe('12px');
    expect(geometry.gap).toBe('12px');
    expect(geometry.padding).toBe('0px 6px 0px 14px');
    expect(geometry.x + geometry.width / 2).toBeCloseTo(600, 0);
    await expect(region.getByRole('button', { name: 'Undo' })).toHaveCSS('height', '26px');
    await expect(page.locator('.toast')).toHaveCSS('animation-name', 'toast-enter');
    await expect(page.locator('.toast')).toHaveCSS('animation-duration', '0.2s');
    await expect(region).toHaveCSS('bottom', '24px');
    await region.getByRole('button', { name: 'Undo' }).click();
    await expect(page.locator('.toast')).toHaveCount(0);
    await show(page, { lead: 'archive' });
    await expect(page.locator('.toast-icon .app-icon')).toHaveCount(1);
    await expect(page.locator('.toast-dot')).toHaveCount(0);
    await region.getByRole('button', { name: 'Stop it' }).click();
    await expect(page.locator('.toast')).toHaveCount(0);
  });
}

test('toast replacement, remaining timer, independent hover/focus and scoped Escape', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-10-10T00:00:00Z') });
  await page.clock.pauseAt(new Date('2026-10-10T00:00:01Z'));
  await page.goto('/');
  await show(page, { message: 'First' });
  await show(page, { message: 'Second' });
  await expect(page.locator('.toast')).toHaveCount(1);
  await expect(page.locator('.toast-text')).toHaveText('Second');
  await page.clock.runFor(2000);
  await page.locator('.toast').hover();
  const undo = page.locator('.toast').getByRole('button', { name: 'Undo' });
  await page.keyboard.press('Tab');
  await undo.focus();
  expect(await undo.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
  await page.mouse.move(0, 0);
  await page.clock.runFor(20000);
  await expect(page.locator('.toast')).toHaveCount(1);
  await undo.evaluate(el => (el as HTMLElement).blur());
  await page.clock.runFor(3999);
  await expect(page.locator('.toast-region')).not.toHaveAttribute('data-exiting');
  await page.clock.runFor(1);
  await expect(page.locator('.toast-region')).toHaveAttribute('data-exiting', 'true');
  await page.clock.runFor(200);
  await expect(page.locator('.toast')).toHaveCount(0);
  await show(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('.toast')).toHaveCount(1);
  await undo.focus();
  await page.keyboard.press('Escape');
  await page.clock.runFor(200);
  await expect(page.locator('.toast')).toHaveCount(0);
});

for (const width of [320, 600]) {
test(`toast at ${width}px ellipsizes before actions; reduced motion removes interpolation`, async ({ page }) => {
  await page.setViewportSize({ width, height: 560 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  await show(page, { message: 'A very long real notification sentence '.repeat(20) });
  const card = page.locator('.toast');
  const geometry = await card.evaluate(el => {
    const r = el.getBoundingClientRect(), s = getComputedStyle(el);
    const message = el.querySelector('.toast-text')!;
    return { width: r.width, height: r.height, left: r.left, right: r.right, animation: s.animationName,
      ellipsis: getComputedStyle(message).textOverflow, clipped: message.scrollWidth > message.clientWidth };
  });
  expect(geometry.width).toBeLessThanOrEqual(width - 16);
  expect(geometry.height).toBe(40);
  expect(geometry.left).toBeGreaterThanOrEqual(8);
  expect(geometry.right).toBeLessThanOrEqual(width - 8);
  expect(geometry.animation).toBe('none');
  expect(geometry.ellipsis).toBe('ellipsis');
  expect(geometry.clipped).toBe(true);
  await expect(card.getByRole('button', { name: 'Undo' })).toBeVisible();
  await card.getByRole('button', { name: 'Undo' }).click();
  await expect(card).toHaveCount(0);
});
}
