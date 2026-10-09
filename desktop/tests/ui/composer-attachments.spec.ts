import { test, expect, type Locator } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { posts } from './support/conversation';

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

const chips = (section: Locator) => section.getByRole('list', { name: 'Attachments' }).getByRole('listitem');

test('picker, paste and drop add chips; removal and limits behave; send carries the files', async ({ page }) => {
  const engine = await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const section = page.getByLabel('Attachment try-out');
  await section.scrollIntoViewIfNeeded();

  await section.getByTestId('composer-file-input').setInputFiles([
    { name: 'sketch.png', mimeType: 'image/png', buffer: PNG },
    { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') },
  ]);
  await expect(chips(section)).toHaveCount(2);
  await expect(section.getByRole('img', { name: 'sketch.png' })).toBeVisible();
  await expect(section.getByText('notes.txt')).toBeVisible();

  const field = section.getByRole('textbox', { name: 'Message' });
  await field.evaluate((el, bytes) => {
    const data = new DataTransfer();
    data.items.add(new File([new Uint8Array(bytes)], 'pasted.png', { type: 'image/png' }));
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, [...PNG]);
  await expect(chips(section)).toHaveCount(3);

  const composer = section.locator('.composer');
  await composer.evaluate(el => {
    const data = new DataTransfer();
    data.items.add(new File(['x'], 'dropped.md', { type: 'text/markdown' }));
    window.dispatchEvent(new DragEvent('dragenter', { dataTransfer: data }));
    el.dispatchEvent(new DragEvent('dragover', { dataTransfer: data, bubbles: true, cancelable: true }));
  });
  await expect(section.getByText('Drop to attach')).toBeVisible();
  await composer.evaluate(el => {
    const data = new DataTransfer();
    data.items.add(new File(['x'], 'dropped.md', { type: 'text/markdown' }));
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }));
  });
  await expect(chips(section)).toHaveCount(4);
  await expect(section.getByText('Drop to attach')).toBeHidden();

  await section.getByRole('button', { name: 'Remove pasted.png' }).click();
  await expect(chips(section)).toHaveCount(3);

  await section.getByTestId('composer-file-input').setInputFiles([
    { name: 'huge.png', mimeType: 'image/png', buffer: Buffer.alloc(11 * 1024 * 1024) },
  ]);
  await expect(section.getByRole('status')).toContainText('huge.png');
  await expect(chips(section)).toHaveCount(3);

  await section.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(chips(section)).toHaveCount(0);
  const body = posts(engine, '/turn')[0].body as { text: string; files: { name: string; mime: string; dataBase64: string }[] };
  expect(body.text).toBe('');
  expect(body.files.map(f => f.name)).toEqual(['sketch.png', 'notes.txt', 'dropped.md']);
  expect(body.files[0]).toMatchObject({ mime: 'image/png', dataBase64: PNG.toString('base64') });
  expect(body.files[1].dataBase64).toBe(Buffer.from('hello').toString('base64'));
});

test('a refused send keeps the attachments', async ({ page }) => {
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] }, fail: { turn: 500 } });
  await page.goto('/');
  await page.getByRole('button', { name: 'Design system', exact: true }).click();
  const section = page.getByLabel('Attachment try-out');
  await section.getByTestId('composer-file-input').setInputFiles([{ name: 'a.txt', mimeType: 'text/plain', buffer: Buffer.from('a') }]);
  await section.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(chips(section)).toHaveCount(1);
});
