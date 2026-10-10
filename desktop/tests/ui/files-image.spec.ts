import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { send } from './support/conversation';

// The File view of a picture (TF-11): the existing confined read, fitted in the body.
// 400x100 and 60x120 PNGs, a 4:1 and a 1:2 picture, so the aspect assertions can tell them apart. Each is two flat
// halves (wide: red then blue across; tall: green then amber down) and decodes cleanly: WebKit draws nothing for a PNG
// whose zlib checksum is wrong while still reporting its natural width, so a damaged fixture passes without a picture.
const WIDE = 'iVBORw0KGgoAAAANSUhEUgAAAZAAAABkCAIAAAAnqfEgAAABkklEQVR42u3UQQkAAAgAMYsJJrKnrSzhSwarcBdTCeeyB86FtDAsDAvDAsPCsDAsMCwMC8MCw8KwMCwwLAwLwwLDwrAwLDAsDAvDAsPCsDAsMCwMC8MCw8KwMCwwLAwLwwLDwrAwLDAsDAvDAsPCsDAsMCwMC8MCw8KwMCwwLAwLwwLDwrAwLDAsDAvDwrDUhWFhWBgWGBaGhWGBYWFYGBYYFoaFYYFhYVgYFhgWhoVhgWFhWBgWGBaGhWGBYWFYGBYYFoaFYYFhYVgYFhgWhoVhgWFhWBgWGBaGhWGBYWFYGBYYFoaFYYFhYVgYFhgWhoVhgWFhWBgWhqUuDAvDwrDAsDAsDAsMC8PCsMCwMCwMCwwLw8KwwLAwLAwLDAvDwrDAsDAsDAsMC8PCsMCwMCwMCwwLw8KwwLAwLAwLDAvDwrDAsDAsDAsMC8PCsMCwMCwMCwwLw8KwwLAwLAwLDAvDwrAwLDAsDAvDAsPCsDAsMCwMC8MCw8KwMCwwLAwLwwLDwrAwLDAsDAvDAsPCsPhrATBSD7yKWiaDAAAAAElFTkSuQmCC';
const TALL = 'iVBORw0KGgoAAAANSUhEUgAAADwAAAB4CAIAAAAhVwZfAAAAeklEQVR42u3OQQkAQAgAMItZ5IL5to6troUgDBZgkf3OCWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWnplfRUniMtLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tLS0tveIDRdW5elZ2EIgAAAAASUVORK5CYII=';
const png = (dataBase64: string) => ({ mime: 'image/png', dataBase64 });

/** Starts a conversation (a file tab reads through its session) and returns the file reads seen so far. */
async function start(page: Page, files: Record<string, { mime: string; dataBase64: string }>) {
  const engine = await installMockEngine(page, { ...plainReply(), files: { 'bin/tool.bin': { mime: 'application/octet-stream', dataBase64: btoa('ab\0cd') }, ...files } });
  const reads: string[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (/\/files(\/text)?$/.test(url.pathname) && url.searchParams.has('path')) reads.push(`${url.pathname.endsWith('/text') ? 'text' : 'image'}:${url.searchParams.get('path')}`);
  });
  await page.goto('/');
  await send(page, 'Where are the pictures?');
  await expect(page.getByRole('tab').first()).toBeVisible();
  return Object.assign(reads, { engine });
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
/** The colour the decoded picture holds at a pixel, read back through a canvas, so a picture the browser never drew fails. */
const pixel = (page: Page, x: number, y: number) => picture(page).evaluate((img: HTMLImageElement, [x, y]) => {
  const canvas = document.createElement('canvas');
  canvas.width = img.naturalWidth; canvas.height = img.naturalHeight;
  const context = canvas.getContext('2d')!;
  context.drawImage(img, 0, 0);
  return Array.from(context.getImageData(x, y, 1, 1).data.slice(0, 3));
}, [x, y]);
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
    expect(await pixel(page, 100, 99)).toEqual([201, 64, 61]);
    expect(await pixel(page, 300, 99)).toEqual([61, 110, 201]);
    for (const viewport of [1200, 320]) {
      await page.setViewportSize({ width: viewport, height: 700 });
      const [box, around] = await Promise.all([picture(page).boundingBox(), body(page).boundingBox()]);
      expect(box!.width).toBeLessThanOrEqual(400);
      expect(box!.width).toBeLessThanOrEqual(around!.width + 0.5);
      expect(Math.abs(box!.x + box!.width / 2 - around!.x - around!.width / 2)).toBeLessThan(1);
      expect(Math.abs(box!.y + box!.height / 2 - around!.y - around!.height / 2)).toBeLessThan(1);
      expect(await body(page).evaluate(el => getComputedStyle(el).backgroundImage)).toBe('none');
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
    'art/vector.png': { mime: 'image/svg+xml', dataBase64: btoa('<svg broken') },
    'art/page.png': { mime: 'text/html', dataBase64: btoa('<script>alert(1)</script>') },
    'art/huge.png': png('A'.repeat(22_400_000)),
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
  await line('art/vector.png', 'This image cannot be shown.');
  await line('art/page.png', 'Binary file');
  await line('art/huge.png', /^Too large to show · /);
});

test('a corrupt picture hands off to the Open in menu, and a repaired file recovers without leaving the tab', async ({ page }) => {
  const { engine } = await start(page, { 'art/fix.png': png(btoa('this is not a picture')) });
  await openFile(page, 'art/fix.png');
  await expect(body(page)).toHaveText('This image cannot be shown.');
  await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
  await expect(page.getByRole('menuitem', { name: 'Copy path' })).toBeVisible();
  await page.keyboard.press('Escape');
  engine.replaceFile('art/fix.png', png(WIDE));
  await expect(picture(page)).toBeVisible({ timeout: 15_000 });
  await expect.poll(() => width(page)).toBe(400);
  await expect(body(page)).not.toHaveText('This image cannot be shown.');
});

test('a held read for the old target never paints over the new one', async ({ page }) => {
  await start(page, { 'art/wide.png': png(WIDE), 'art/tall.png': png(TALL) });
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route(/\/files\?path=art%2Fwide\.png/, async route => { await held; await route.fallback(); });
  await openFile(page, 'art/wide.png');
  await expect(body(page)).toHaveText('Loading…');
  await openFile(page, 'art/tall.png');
  await expect.poll(() => width(page)).toBe(60);
  release();
  await page.getByRole('tab', { name: 'wide.png', exact: true }).click();
  await expect.poll(() => width(page)).toBe(400);
  await page.getByRole('tab', { name: 'tall.png', exact: true }).click();
  await expect.poll(() => width(page)).toBe(60);
});

for (const theme of ['light', 'dark'] as const) {
  test(`SVG renders from a blob as a restricted image, never inline (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="60" height="120"><script>window.__svgExecuted=true</script><rect width="60" height="120" fill="#c9403d"/></svg>';
    const reads = await start(page, { 'art/vector.svg': { mime: 'image/svg+xml', dataBase64: btoa(svg) } });
    await openFile(page, 'art/vector.svg');
    await expect.poll(() => width(page)).toBe(60);
    await expect(picture(page)).toHaveAttribute('src', /^blob:/);
    expect(await pixel(page, 30, 60)).toEqual([201, 64, 61]);
    await expect(body(page).locator('svg')).toHaveCount(0);
    expect(await page.evaluate(() => '__svgExecuted' in window)).toBe(false);
    const box = await picture(page).boundingBox();
    expect(box!.width).toBe(60);
    expect(box!.height).toBe(120);
    await page.setViewportSize({ width: 320, height: 220 });
    const [small, pane] = await Promise.all([picture(page).boundingBox(), body(page).boundingBox()]);
    expect(small!.height).toBeLessThanOrEqual(pane!.height);
    expect(small!.width).toBeLessThanOrEqual(pane!.width);
    expect(Math.abs(small!.width / small!.height - 0.5)).toBeLessThan(0.01);
    expect(reads).toContain('image:art/vector.svg');
    expect(reads).not.toContain('text:art/vector.svg');
  });
}

test('an engine read refusal keeps the Open in dropdown available', async ({ page }) => {
  await start(page, { 'art/large.png': png(WIDE) });
  await page.route(/\/files\?path=art%2Flarge\.png/, route => route.fulfill({ status: 502, json: { error: 'file exceeds the 16 MiB read limit' } }));
  await openFile(page, 'art/large.png');
  await expect(body(page)).toHaveText('file exceeds the 16 MiB read limit');
  await page.locator('.file-head').getByRole('button', { name: 'Open in' }).click();
  await expect(page.getByRole('menuitem', { name: 'Copy path' })).toBeVisible();
});
