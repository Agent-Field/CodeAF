import { test, expect } from '@playwright/test';
import { openPage } from './support/shell-navigation';
import { tokenColor } from './support/../contracts';

// Council chat against Decisions 12d: every number is read from the design, not from tokens.json, so a drifting token fails here.
for (const theme of ['light', 'dark'] as const) {
  test(`council view matches the design (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/');
    await openPage(page, 'Design system');
    const council = page.getByRole('region', { name: 'Council specimen' });
    await council.scrollIntoViewIfNeeded();

    const message = council.locator('.council-message').first();
    await expect(message).toHaveCSS('max-width', '560px');
    await expect(message).toHaveCSS('column-gap', '10px');
    const swatch = message.locator('.place-swatch');
    await expect(swatch).toHaveCSS('width', '12px');
    await expect(swatch).toHaveCSS('border-top-left-radius', '3.6px');
    await expect(message.locator('.council-message-name')).toHaveCSS('font-size', '12px');
    await expect(message.locator('.council-message-name')).toHaveCSS('font-weight', '600');
    const body = message.locator('.council-message-body');
    await expect(body).toHaveCSS('font-size', '13px');
    // 13px at 1.6; WebKit reports 20.8px with float noise, so compare as a number.
    expect(parseFloat(await body.evaluate(el => getComputedStyle(el).lineHeight))).toBeCloseTo(20.8, 1);
    await expect(body).toHaveCSS('color', await tokenColor(page, 'ink'));

    const card = council.locator('.council-decided');
    await expect(card).toHaveCSS('border-top-left-radius', '12px');
    await expect(card).toHaveCSS('padding', '12px 14px');
    await expect(card).toHaveCSS('background-color', await tokenColor(page, 'field'));
    await expect(card.locator('.council-decided-title')).toHaveText('Decided: promise it for v2.4.1+, outside strict mode');
    await expect(card.locator('.council-decided-title')).toHaveCSS('font-weight', '500');
    await expect(card.locator('.app-icon')).toHaveCSS('width', '12px');
    await expect(card.locator('.council-decided-facts')).toHaveText('Agreed by both places · 2 turns · $0.01 · filed in Marketing and Config parser');
    await expect(card.locator('.council-decided-facts')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
    await expect(council.locator('.council-footer')).toHaveText('You can read and steer a council like any chat. Writing here pauses it until you send.');
    await expect(council.locator('.council-footer')).toHaveCSS('font-size', '12px');
  });
}
