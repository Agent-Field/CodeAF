import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { message, openApp } from './support/conversation';

// The composer keeps ONE look. Only keyboard focus on the field may draw a ring.
const look = (box: Locator) =>
  box.evaluate((el) => {
    const style = getComputedStyle(el);
    return [style.backgroundColor, style.boxShadow, style.borderTopWidth, style.outlineStyle].join('|');
  });
const ring = (field: Locator) => field.evaluate((el) => getComputedStyle(el).boxShadow);

async function open(page: Page) {
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const section = page.getByLabel('Attachment try-out');
  await section.scrollIntoViewIfNeeded();
  return section;
}

for (const theme of ['light', 'dark'] as const) {
  test(`the composer looks identical at rest, hover, click and typing (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const section = await open(page);
    const box = section.locator('.composer').first();
    const field = section.getByRole('textbox', { name: 'Message' });
    const rest = await look(box);
    const fieldRest = await ring(field);

    await box.hover();
    expect(await look(box)).toBe(rest);
    await field.click();
    await expect(field).toBeFocused();
    expect(await look(box)).toBe(rest);
    expect(await ring(field)).toBe(fieldRest);
    await page.keyboard.type('typing changes nothing');
    expect(await look(box)).toBe(rest);
    expect(await ring(field)).toBe(fieldRest);
  });
}

test('the start screen asks what we are building and hints at @ references', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await expect(page.getByText('What are we building?')).toBeVisible();
  await expect(message(page)).toHaveAttribute('placeholder', 'Ask codeaf, or type @ to reference a file');
});

test('keyboard focus draws the ring on the field alone', async ({ page }) => {
  const section = await open(page);
  const box = section.locator('.composer').first();
  const field = section.getByRole('textbox', { name: 'Message' });
  const rest = await look(box);
  const fieldRest = await ring(field);
  await section.getByRole('button', { name: 'Attach files' }).first().focus();
  await page.keyboard.press('Shift+Tab');
  await expect(field).toBeFocused();
  expect(await ring(field)).not.toBe(fieldRest);
  expect(await look(box)).toBe(rest);
});
