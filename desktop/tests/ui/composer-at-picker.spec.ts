import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { message, openApp, posts, send } from './support/conversation';

const LONG = 'src/features/conversation/composer/deep/nested/folder/again/and/more/segments/lexer.go';
const RANKED = [
  'cmd/lex.go',
  'internal/parse/lexer.go',
  'lex.go',
  'notes/lexical.md',
  LONG,
  'docs/lexicon/readme.md',
];

const blob = () => ({ mime: 'text/plain', dataBase64: Buffer.from('x').toString('base64') });

function watchFinds(page: Page) {
  const queries: { q: string; limit: string }[] = [];
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.pathname.endsWith('/files/find')) queries.push({ q: url.searchParams.get('q') ?? '', limit: url.searchParams.get('limit') ?? '' });
  });
  return queries;
}

async function boot(page: Page) {
  const files = Object.fromEntries(RANKED.map(path => [path, blob()]));
  const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' }, files });
  await openApp(page);
  return engine;
}

/** A conversation exists only after the first send, which is when @ can search. */
async function conversation(page: Page) {
  const engine = await boot(page);
  await send(page, 'hello');
  await expect(page.locator('.user-message-text').filter({ hasText: 'hello' })).toBeVisible();
  await expect(message(page)).toHaveValue('');
  return engine;
}

async function openList(page: Page, text = '@lex') {
  const field = message(page);
  await field.fill(text);
  const list = page.getByRole('listbox', { name: 'Files' });
  await expect(list).toBeVisible();
  await list.evaluate(node => Promise.all(node.getAnimations().map(animation => animation.finished)));
  return list;
}

async function tokenColor(page: Page, token: string, property: 'backgroundColor' | 'color') {
  return page.evaluate(({ token, property }) => {
    const probe = document.createElement('div');
    probe.style[property === 'backgroundColor' ? 'background' : 'color'] = `var(--${token})`;
    document.body.appendChild(probe);
    const value = getComputedStyle(probe)[property];
    probe.remove();
    return value;
  }, { token, property });
}

async function pause(page: Page) {
  await page.evaluate(() => new Promise(resolve => setTimeout(resolve, 400)));
}

test('before a conversation exists, @ draws nothing and does not search', async ({ page }) => {
  const found = watchFinds(page);
  await boot(page);
  await message(page).pressSequentially('@lex', { delay: 20 });
  await pause(page);
  await expect(page.getByRole('listbox', { name: 'Files' })).toHaveCount(0);
  expect(found).toHaveLength(0);
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: the list is menu paper above the composer, rows are 28px field-2, and the directory is middle-truncated`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await conversation(page);
    await page.mouse.move(0, 0);
    const list = await openList(page);
    const field2 = await tokenColor(page, 'field-2', 'backgroundColor');
    const ink = await tokenColor(page, 'ink', 'color');
    const ink2 = await tokenColor(page, 'ink-2', 'color');
    const ink3 = await tokenColor(page, 'ink-3', 'color');
    const composer = page.locator('.conversation-footer .composer');
    const placed = await list.evaluate(node => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return { shadow: style.boxShadow, radius: style.borderRadius, border: style.borderTopWidth, width: rect.width, left: rect.left, bottom: rect.bottom };
    });
    const box = await composer.evaluate(node => {
      const rect = node.getBoundingClientRect();
      return { width: rect.width, left: rect.left, top: rect.top };
    });
    expect(placed.shadow).not.toBe('none');
    expect(placed.radius).toBe('10px');
    expect(placed.border).toBe('0px');
    expect(Math.round(placed.width)).toBe(320);
    expect(box.width).toBeGreaterThan(320);
    expect(Math.abs(placed.left - box.left)).toBeLessThanOrEqual(1);
    expect(Math.abs(placed.bottom - (box.top - 8))).toBeLessThanOrEqual(1);

    const options = list.getByRole('option');
    expect(await options.evaluateAll(nodes => nodes.map(node => node.getAttribute('aria-label')))).toEqual(RANKED);
    const row = options.first();
    const measured = await row.evaluate(node => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return { height: rect.height, radius: style.borderRadius, pad: style.paddingLeft, gap: style.columnGap, font: style.fontSize, bg: style.backgroundColor, color: style.color, shadow: style.boxShadow };
    });
    expect(Math.round(measured.height)).toBe(28);
    expect(measured.radius).toBe('6px');
    expect(measured.pad).toBe('10px');
    expect(measured.gap).toBe('10px');
    expect(measured.font).toBe('13px');
    expect(measured.bg).toBe(field2);
    expect(measured.color).toBe(ink);
    expect(measured.shadow).toBe('none');
    const quiet = await options.nth(1).evaluate(node => getComputedStyle(node).backgroundColor);
    expect(['rgba(0, 0, 0, 0)', 'transparent']).toContain(quiet);

    const icon = row.locator('.app-icon');
    const iconBox = await icon.boundingBox();
    expect(Math.round(iconBox?.width ?? 0)).toBe(14);
    expect(Math.round(iconBox?.height ?? 0)).toBe(14);
    expect(await icon.evaluate(node => getComputedStyle(node).color)).toBe(ink2);
    await expect(row.locator('.at-picker-name')).toHaveText('lex.go');
    await expect(options.nth(2).locator('.at-picker-dir')).toHaveCount(0);
    const folder = LONG.slice(0, LONG.lastIndexOf('/'));
    const clipped = await options.nth(4).locator('.at-picker-dir').evaluate(node => {
      const head = node.querySelector('.at-picker-dir-head') as HTMLElement;
      const tail = node.querySelector('.at-picker-dir-tail') as HTMLElement;
      return { headClipped: head.scrollWidth > head.clientWidth, tailWhole: tail.scrollWidth <= tail.clientWidth + 1, tail: tail.textContent ?? '', color: getComputedStyle(node).color };
    });
    expect(clipped.color).toBe(ink3);
    expect(clipped.headClipped).toBe(true);
    expect(clipped.tailWhole).toBe(true);
    expect(clipped.tail).toBe(folder.slice(Math.ceil(folder.length / 2)));

    await options.nth(1).hover();
    // Hover fills over 120ms. Read the colour once that transition has landed.
    await expect.poll(() => options.nth(1).evaluate(node => getComputedStyle(node).backgroundColor)).toBe(field2);
    await page.mouse.down();
    await expect.poll(() => options.nth(1).evaluate(node => getComputedStyle(node).filter)).not.toBe('none');
    await page.mouse.up();
  });
}

test('arrows move without wrapping, Enter Tab and click insert the path, Esc and scroll close', async ({ page }) => {
  const engine = await conversation(page);
  const field = message(page);
  const list = await openList(page, 'see @lex');
  const options = list.getByRole('option');
  await expect(field).toHaveAttribute('aria-autocomplete', 'list');
  await expect(field).toHaveAttribute('aria-activedescendant', await options.first().getAttribute('id') ?? '');
  await field.press('ArrowUp');
  await expect(options.first()).toHaveAttribute('aria-selected', 'true');
  await field.press('ArrowDown');
  await expect(options.nth(1)).toHaveAttribute('aria-selected', 'true');
  for (let step = 0; step < RANKED.length; step += 1) await field.press('ArrowDown');
  await expect(options.last()).toHaveAttribute('aria-selected', 'true');
  await field.press('ArrowDown');
  await expect(options.last()).toHaveAttribute('aria-selected', 'true');
  const turns = posts(engine, '/turn').length;
  await field.press('Enter');
  await expect(list).toHaveCount(0);
  await expect(field).toHaveValue('see docs/lexicon/readme.md');
  await expect(field).toBeFocused();
  expect(posts(engine, '/turn')).toHaveLength(turns);
  await field.pressSequentially(' next');
  await field.press('Enter');
  await expect.poll(() => posts(engine, '/turn').length).toBe(turns + 1);
  expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: 'see docs/lexicon/readme.md next' });
  await expect(page.locator('.user-message-text').last()).toHaveText('see docs/lexicon/readme.md next');

  const again = await openList(page);
  await field.press('Tab');
  await expect(again).toHaveCount(0);
  await expect(field).toHaveValue('cmd/lex.go');
  await expect(field).toBeFocused();
  expect(posts(engine, '/turn')).toHaveLength(turns + 1);

  const third = await openList(page);
  await third.getByRole('option', { name: 'notes/lexical.md', exact: true }).click();
  await expect(field).toHaveValue('notes/lexical.md');
  await expect(field).toBeFocused();

  const fourth = await openList(page);
  await field.press('Shift+Enter');
  await expect(field).toHaveValue('@lex\n');
  await expect(fourth).toHaveCount(0);

  await field.fill('@lex');
  const fifth = page.getByRole('listbox', { name: 'Files' });
  await expect(fifth).toBeVisible();
  await field.press('Escape');
  await expect(fifth).toHaveCount(0);
  await expect(field).toBeFocused();
  await expect(field).toHaveValue('@lex');
  await field.press('Escape');
  await expect(field).not.toBeFocused();

  await field.focus();
  // Esc dismissed this exact @lex. A different query is what opens the list again.
  const sixth = await openList(page, '@lexer');
  await page.evaluate(() => window.dispatchEvent(new Event('scroll')));
  await expect(sixth).toHaveCount(0);
  await expect(field).toBeFocused();
});

test('a bare @, an email, a miss, and an engine error draw nothing', async ({ page }) => {
  const found = watchFinds(page);
  const engine = await conversation(page);
  const field = message(page);
  const list = page.getByRole('listbox', { name: 'Files' });
  await field.fill('@');
  await pause(page);
  await expect(list).toHaveCount(0);
  expect(found).toHaveLength(0);

  await field.fill('mail me at user@example.com');
  await pause(page);
  await expect(list).toHaveCount(0);
  expect(found).toHaveLength(0);

  await field.fill('@zzzz');
  await expect.poll(() => found.some(query => query.q === 'zzzz')).toBe(true);
  await expect(list).toHaveCount(0);
  await expect(page.locator('.composer-error')).toHaveCount(0);

  engine.setOffline(true);
  await field.fill('@lex');
  await expect.poll(() => found.some(query => query.q === 'lex')).toBe(true);
  await expect(list).toHaveCount(0);
  await expect(page.locator('.composer-error')).toHaveCount(0);
});

test('typing collapses to one search, and Enter before the list is up sends the typed text', async ({ page }) => {
  const found = watchFinds(page);
  const engine = await conversation(page);
  const field = message(page);
  const before = found.length;
  await field.pressSequentially('@lex', { delay: 15 });
  await expect(page.getByRole('listbox', { name: 'Files' })).toBeVisible();
  const added = found.slice(before);
  expect(added.length).toBeGreaterThan(0);
  expect(added.length).toBeLessThanOrEqual(2);
  expect(added.at(-1)).toEqual({ q: 'lex', limit: '20' });

  await field.fill('');
  await expect(page.getByRole('listbox', { name: 'Files' })).toHaveCount(0);
  const turns = posts(engine, '/turn').length;
  await field.evaluate(node => {
    const field = node as HTMLTextAreaElement;
    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
    set?.call(field, '@lex');
    field.dispatchEvent(new InputEvent('input', { bubbles: true }));
    field.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
  });
  await expect.poll(() => posts(engine, '/turn').length).toBe(turns + 1);
  expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: '@lex' });
});

test('at 600px the list is as wide as the composer', async ({ page }) => {
  await conversation(page);
  await page.setViewportSize({ width: 600, height: 800 });
  const list = await openList(page);
  const widths = await list.evaluate(node => {
    const composer = document.querySelector('.conversation-footer .composer');
    return { list: node.getBoundingClientRect().width, composer: composer?.getBoundingClientRect().width ?? 0 };
  });
  expect(widths.composer).toBeGreaterThan(28);
  expect(Math.abs(widths.list - widths.composer)).toBeLessThanOrEqual(1);
});

test('coarse pointers use 36px rows', async ({ browser, baseURL }) => {
  const context = await browser.newContext({ baseURL, hasTouch: true, isMobile: true, viewport: { width: 800, height: 800 } });
  const page = await context.newPage();
  try {
    await conversation(page);
    expect(await page.evaluate(() => matchMedia('(pointer: coarse)').matches)).toBe(true);
    const list = await openList(page);
    expect(Math.round(await list.getByRole('option').first().evaluate(node => node.getBoundingClientRect().height))).toBe(36);
  } finally {
    await context.close();
  }
});
