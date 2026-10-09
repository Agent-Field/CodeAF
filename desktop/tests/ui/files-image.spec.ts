import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { send } from './support/conversation';

// The File view of a raster picture (design gap TF11): the existing confined read, fitted in the body.
// 400x100 and 60x120 PNGs, a 4:1 and a 1:2 picture, so the aspect assertions can tell them apart.
const WIDE = 'iVBORw0KGgoAAAANSUhEUgAAAZAAAABkCAIAAAAnqfEgAAABZUlEQVR4nO3UQQkAMAzAwAqrfyZrFvYbgYMTkFfm7AIkzPcCgEeGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBhWECGYQEZhgVkGBaQYVhAhmEBGYYFZBgWkGFYQIZhARmGBWQYFpBxAfw9W25Hz8fiAAAAAElFTkSuQmCC';
const TALL = 'iVBORw0KGgoAAAANSUhEUgAAADwAAAB4CAIAAAAhVwZfAAAAgUlEQVR4nO3OAQkAIBAAsQ/2/TGWMTxhsACb3fOdeT6QDpOWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYOkJaWDpCWlg6QlpYO+DJ9ATp7Kg69Drg7AAAAAElFTkSuQmCC';
const png = (dataBase64: string) => ({ mime: 'image/png', dataBase64 });

/** Starts a conversation (a file tab reads through its session) and returns the file reads seen so far. */
async function start(page: Page, files: Record<string, { mime: string; dataBase64: string }>) {
  await installMockEngine(page, { ...plainReply(), files: { 'bin/tool.bin': { mime: 'application/octet-stream', dataBase64: btoa('ab\0cd') }, ...files } });
  const reads: string[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (/\/files(\/text)?$/.test(url.pathname) && url.searchParams.has('path')) reads.push(`${url.pathname.endsWith('/text') ? 'text' : 'image'}:${url.searchParams.get('path')}`);
  });
  await page.goto('/');
  await send(page, 'Where are the pictures?');
  await expect(page.getByRole('tab').first()).toBeVisible();
  return reads;
}

/** Opens a file tab the way a person does: the new tab's field, a part of the name, the matching row. */
async function openFile(page: Page, path: string) {
  const name = path.split('/').pop()!;
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.getByRole('combobox', { name: 'Search or start' }).fill(name.replace(/\.[^.]+$/, ''));
  await page.getByRole('option', { name: new RegExp(name.replace('.', '\\.')) }).click();
  await expect(page.getByRole('tab', { name, exact: true })).toHaveAttribute('aria-selected', 'true');
}

const body = (page: Page) => page.locator('.file-body');
const picture = (page: Page) => body(page).locator('.file-picture');
const width = (page: Page) => picture(page).evaluate((img: HTMLImageElement) => img.complete ? img.naturalWidth : 0);
const toggle = (page: Page, name: 'Changes' | 'File') => page.locator('.file-head').getByRole('radio', { name });

for (const theme of ['light', 'dark'] as const) {
  test(`the File view draws a picture fitted inside the body, aspect kept (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const reads = await start(page, { 'art/wide.png': png(WIDE) });
    await openFile(page, 'art/wide.png');
    await expect(picture(page)).toBeVisible();
    await expect(body(page)).toHaveAttribute('aria-label', 'Contents of wide.png');
    await expect(picture(page)).toHaveAttribute('alt', 'wide.png');
    await expect.poll(() => width(page)).toBe(400);
    for (const viewport of [1200, 320]) {
      await page.setViewportSize({ width: viewport, height: 700 });
      const [box, around] = await Promise.all([picture(page).boundingBox(), body(page).boundingBox()]);
      expect(box!.width).toBeLessThanOrEqual(around!.width + 0.5);
      expect(box!.height).toBeLessThanOrEqual(around!.height + 0.5);
      expect(Math.abs(box!.width / box!.height - 4)).toBeLessThan(0.1);
      expect(await body(page).evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    }
    expect(reads).toContain('image:art/wide.png');
    expect(reads).not.toContain('text:art/wide.png');
  });
}

test('the Changes view never reads the picture, and a non-picture binary never asks for image bytes', async ({ page }) => {
  const reads = await start(page, { 'art/wide.png': png(WIDE) });
  await openFile(page, 'art/wide.png');
  await expect(picture(page)).toBeVisible();
  const before = reads.length;
  await toggle(page, 'Changes').click();
  await expect(body(page)).toHaveText(/No changes\.|Binary file/);
  await expect(picture(page)).toHaveCount(0);
  await page.reload();
  await expect(toggle(page, 'Changes')).toBeChecked();
  await expect(body(page)).toHaveText(/No changes\.|Binary file/);
  expect(reads.length).toBe(before);
  await toggle(page, 'File').click();
  await expect(picture(page)).toBeVisible();
  await openFile(page, 'bin/tool.bin');
  await expect(body(page)).toHaveText('Binary file');
  expect(reads).toContain('text:bin/tool.bin');
  expect(reads).not.toContain('image:bin/tool.bin');
});

test('switching the target shows the new picture, never the old one', async ({ page }) => {
  await start(page, { 'art/wide.png': png(WIDE), 'art/tall.png': png(TALL) });
  await openFile(page, 'art/wide.png');
  await expect.poll(() => width(page)).toBe(400);
  await openFile(page, 'art/tall.png');
  await expect.poll(() => width(page)).toBe(60);
  await page.getByRole('tab', { name: 'wide.png', exact: true }).click();
  await expect.poll(() => width(page)).toBe(400);
  await expect(body(page)).toHaveAttribute('aria-label', 'Contents of wide.png');
  await page.getByRole('tab', { name: 'tall.png', exact: true }).click();
  await expect.poll(() => width(page)).toBe(60);
});

test('an unsafe, oversize or corrupt picture is one muted line and an Open in menu', async ({ page }) => {
  await start(page, {
    'art/corrupt.png': png(btoa('this is not a picture')),
    'art/vector.png': { mime: 'image/svg+xml', dataBase64: btoa('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>') },
    'art/page.png': { mime: 'text/html', dataBase64: btoa('<script>alert(1)</script>') },
    'art/huge.png': png('A'.repeat(12_000_000)),
  });
  const line = async (path: string, text: string | RegExp) => {
    await openFile(page, path);
    await expect(body(page)).toHaveText(text);
    await expect(picture(page)).toHaveCount(0);
    await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
    await expect(page.getByRole('menuitem', { name: 'Copy path' })).toBeVisible();
    await page.keyboard.press('Escape');
  };
  await line('art/corrupt.png', 'This image cannot be shown.');
  await line('art/vector.png', 'Binary file');
  await line('art/page.png', 'Binary file');
  await line('art/huge.png', /^Too large to show · /);
});
