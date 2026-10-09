import { expect, type Page } from '@playwright/test';

/**
 * The strip's New tab button opens the new-tab field (design 3f), not a conversation. This opens it, types a
 * first line and presses Enter. With the engine unreachable (most tab specs abort it) the conversation still
 * opens and keeps the words as its draft, so the composer is there to use.
 */
export async function newConversation(page: Page, text = 'New conversation') {
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 const field = page.getByRole('combobox', { name: 'Search or start' });
 await field.fill(text);
 await field.press('Enter');
 await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
}
