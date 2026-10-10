import fs from 'node:fs';
import { openPage } from './support/shell-navigation';
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
  await openPage(page, 'Design system');
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
  await openPage(page, 'Design system');
  const section = page.getByLabel('Attachment try-out');
  await section.getByTestId('composer-file-input').setInputFiles([{ name: 'a.txt', mimeType: 'text/plain', buffer: Buffer.from('a') }]);
  await section.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(chips(section)).toHaveCount(1);
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: mixed pending attachments keep the file compact and remove independently`, async ({ page, browserName }) => {
    await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
    await page.emulateMedia({ colorScheme: theme });
    const engine = await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
    await page.goto('/');
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
    const section = page.locator('.composer-dock').first();
    const composer = section.locator('.composer');
    await section.getByTestId('composer-file-input').setInputFiles([
      { name: 'sketch.png', mimeType: 'image/png', buffer: PNG },
      { name: 'error.log', mimeType: 'text/plain', buffer: Buffer.from('fixture!') },
    ]);
    const file = section.getByRole('listitem', { name: 'error.log, 8 B' });
    await expect(file).toHaveAttribute('title', 'error.log · 8 B');
    await expect(file).toHaveText('error.log');
    const geometry = await file.evaluate(el => {
      const c = getComputedStyle(el), name = getComputedStyle(el.querySelector('.attachment-name')!), remove = el.querySelector('.attachment-remove')!, icon = remove.querySelector('.app-icon')!;
      return { height: el.getBoundingClientRect().height, padding: c.padding, radius: c.borderRadius, gap: c.gap, font: name.fontSize, weight: name.fontWeight, remove: remove.getBoundingClientRect().width, icon: icon.getBoundingClientRect().width };
    });
    expect(geometry).toEqual({ height: 24, padding: '0px 4px 0px 7px', radius: '7px', gap: '5px', font: '12px', weight: '500', remove: 16, icon: 10 });
    await expect(section.getByRole('img', { name: 'sketch.png' })).toBeVisible();
    expect(await section.locator('.attachment-picture').evaluate(el => el.getBoundingClientRect().height)).toBe(44);
    await section.getByRole('textbox', { name: 'Message' }).fill("Here's the crash from this morning");
    if (process.env.CODEAF_UI_RESULTS) {
      await composer.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/${browserName}-${theme}-mixed-attachments.png` });
      const measured = await composer.evaluate(el => {
        const props = ['fontSize','fontWeight','lineHeight','color','backgroundColor','paddingTop','paddingRight','paddingBottom','paddingLeft','borderRadius','boxShadow','gap','borderTopWidth'];
        return Object.fromEntries([['composer',el],['tray',el.querySelector('.attachment-tray')],['picture',el.querySelector('.attachment-picture')],['thumb',el.querySelector('.attachment-thumb')],['pictureRemove',el.querySelector('.attachment-picture .attachment-remove')],['file',el.querySelector('.attachment-file')],['fileName',el.querySelector('.attachment-name')],['fileIcon',el.querySelector('.attachment-file > .app-icon')],['fileRemove',el.querySelector('.attachment-file .attachment-remove')],['fileRemoveIcon',el.querySelector('.attachment-file .attachment-remove .app-icon')],['input',el.querySelector('.composer-field')],['row',el.querySelector('.composer-row')]].map(([key,n]) => {
          const node = n as Element, cs = getComputedStyle(node), b = node.getBoundingClientRect();
          return [key,{width:b.width,height:b.height,styles:Object.fromEntries(props.map(k => [k,(cs as unknown as Record<string,string>)[k]]))}];
        }));
      });
      fs.writeFileSync(`${process.env.CODEAF_UI_RESULTS}/${browserName}-${theme}-mixed-attachments.json`, JSON.stringify(measured,null,2));
    }
    await section.getByRole('button', { name: 'Remove error.log' }).click();
    await expect(chips(section)).toHaveCount(1);
    await expect(section.getByRole('img', { name: 'sketch.png' })).toBeVisible();
    await section.getByRole('button', { name: 'Remove sketch.png' }).click();
    await expect(chips(section)).toHaveCount(0);
    await expect(composer).not.toHaveAttribute('data-attached');
    expect(await composer.evaluate(el => getComputedStyle(el).padding)).toBe('12px 10px 8px 16px');
    expect(posts(engine, '/turn')).toHaveLength(0);
  });
}
