import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { posts } from './support/conversation';

const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join('\n');

async function paste(field: import('@playwright/test').Locator, text: string) {
  await field.evaluate((el, value) => {
    const data = new DataTransfer();
    data.setData('text/plain', value);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, text);
}

test('a paste over 12 lines becomes a removable card and is sent as tagged text', async ({ page }) => {
  const engine = await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const section = page.getByLabel('Attachment try-out');
  await section.scrollIntoViewIfNeeded();
  const field = section.getByRole('textbox', { name: 'Message' });

  await paste(field, lines(12));
  await expect(section.getByText('Pasted text')).toHaveCount(0);

  await paste(field, lines(214));
  await expect(section.getByText('Pasted text')).toBeVisible();
  await expect(section.getByText('214 lines')).toBeVisible();

  await section.getByRole('button', { name: 'Remove pasted text' }).click();
  await expect(section.getByText('Pasted text')).toHaveCount(0);

  await paste(field, lines(30));
  await field.fill('What failed?');
  await section.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(section.getByText('Pasted text')).toHaveCount(0);
  const body = posts(engine, '/turn')[0].body as { text: string };
  expect(body.text).toBe(`<pasted-text lines="30">\n${lines(30)}\n</pasted-text>\n\nWhat failed?`);
});

test('typed text caps at 8 lines and scrolls', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const field = page.getByLabel('Attachment try-out').getByRole('textbox', { name: 'Message' });
  await field.fill(lines(8));
  const eight = (await field.boundingBox())!.height;
  await field.fill(lines(20));
  const twenty = (await field.boundingBox())!.height;
  expect(twenty).toBe(eight);
  expect(await field.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
});
