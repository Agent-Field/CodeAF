import { test, expect, type Locator, type Page } from '@playwright/test';
import { INK3_TEXT, expectAccessible, tokenColor } from '../ui/contracts';

// The Places primitives against the designer's measurements (Places 8a, 8b, 8f; Components "Place tile", "History row").
// Every number below is read from the design, not from tokens.json, so a drifting token fails here.
INK3_TEXT.push('.places-row-aside', '.places-row-muted', '.places-chat-excerpt', '.places-chat-time', '.places-tile-meta', '.places-tile-hint', '.places-tile-main',
  '.places-crumb', '.places-specimen-caption', '.type-section-label', '.places-specimen-log', '.places-heading-menu', '.places-chat-lead');

async function open(page: Page, theme: 'light' | 'dark' = 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto('/');
  await page.getByTestId('places-specimen').waitFor();
}
const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');

/** A point on the row the cursor can actually press. A dock can cover the middle of a chat, and a row below the fold is not under the cursor until it is scrolled into view. */
async function pointOn(locator: Locator) {
  const find = () => locator.evaluate(el => {
    const r = el.getBoundingClientRect();
    for (let y = r.top + 2; y < r.bottom - 1; y += 4) {
      for (const x of [r.left + 16, r.left + r.width / 2]) {
        const top = document.elementsFromPoint(x, y)[0];
        if (top && (top === el || el.contains(top))) return { x, y };
      }
    }
    return null;
  });
  let point = await find();
  if (!point) {
    await locator.evaluate(el => el.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
    point = await find();
  }
  if (!point) throw new Error('no point on the dragged row is under the cursor');
  return point;
}
const tile = (page: Page, id: string) => page.locator(`[data-place-id="${id}"]`);
async function shadow(page: Page, value: string) {
  return page.evaluate(declared => {
    const probe = document.createElement('span');
    probe.style.boxShadow = declared;
    document.body.append(probe);
    const painted = getComputedStyle(probe).boxShadow;
    probe.remove();
    return painted;
  }, value);
}
const chat = (page: Page, id: string) => page.locator(`[data-chat-id="${id}"]`);

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme}`, () => {
    test('swatches: six tints in every role, radius 0.3 of the side', async ({ page }) => {
      await open(page, theme);
      const sizes = { rail: [10, '3px'], tile: [12, '3.6px'], sheet: [14, '4.2px'], title: [16, '4.8px'], card: [8, '2.4px'], choice: [12, '4px'], menu: [16, '5px'] } as const;
      for (const tint of ['tide', 'iris', 'rose', 'sand', 'sage', 'graphite']) {
        const colour = await tokenColor(page, `places-swatch-${tint}`);
        for (const [role, [side, radius]] of Object.entries(sizes)) {
          const swatch = page.locator(`[data-tint-row="${tint}"] .place-swatch[data-role="${role}"]`);
          await expect(swatch).toHaveCSS('width', `${side}px`);
          await expect(swatch).toHaveCSS('height', `${side}px`);
          await expect(swatch).toHaveCSS('border-top-left-radius', radius);
          await expect(swatch).toHaveCSS('background-color', colour);
        }
      }
      // Exact swatch values from the design (oklch), light and dark alike.
      const values = await page.evaluate(() => Object.fromEntries(['tide', 'iris', 'rose', 'sand', 'sage', 'graphite'].map(n => [n, getComputedStyle(document.documentElement).getPropertyValue(`--places-swatch-${n}`).trim()])));
      expect(values).toEqual({ tide: 'oklch(.58 .13 240)', iris: 'oklch(.58 .14 285)', rose: 'oklch(.6 .13 12)', sand: 'oklch(.65 .1 65)', sage: 'oklch(.6 .09 150)', graphite: 'oklch(.55 .012 250)' });
    });

    test('tile geometry, type and every state', async ({ page }) => {
      await open(page, theme);
      const rest = tile(page, 'state-Rest');
      await expect(rest).toHaveCSS('height', '84px');
      await expect(rest).toHaveCSS('border-top-left-radius', '12px');
      await expect(rest).toHaveCSS('background-color', await tokenColor(page, 'surface'));
      const main = rest.locator('.places-tile-main');
      await expect(main).toHaveCSS('padding', '12px 14px');
      await expect(main).toHaveCSS('row-gap', '4px');
      await expect(rest.locator('.places-tile-name')).toHaveCSS('font-size', '13px');
      await expect(rest.locator('.places-tile-name')).toHaveCSS('font-weight', '500');
      await expect(rest.locator('.places-tile-meta')).toHaveCSS('font-size', '11px');
      await expect(rest.locator('.places-tile-meta')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(rest.locator('.places-tile-meta')).toHaveText('28 chats');
      await expect(tile(page, 'state-drag').locator('.places-tile-meta')).toHaveText('6 chats · also in Software');
      await expect(tile(page, 'state-failed').locator('.places-tile-meta')).toHaveText('47 places · 212 chats');
      await expect(rest.locator('.place-swatch')).toHaveCSS('width', '12px');
      await expect(rest.locator('.status-mark-dot')).toHaveCSS('width', '6px');
      await expect(rest.locator('.status-mark')).toHaveCSS('width', '6px');
      await expect(rest.locator('.status-mark')).toHaveCSS('height', '6px');
      await expect(rest.locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'amber'));
      await expect(rest.locator('.places-tile-name')).toHaveCSS('color', await tokenColor(page, 'ink'));
      // Name sits at the bottom: margin-top auto pushes it below the head.
      const [head, name] = await Promise.all([rest.locator('.places-tile-head').boundingBox(), rest.locator('.places-tile-name').boundingBox()]);
      expect(name!.y).toBeGreaterThan(head!.y + head!.height);

      await expect(tile(page, 'state-Hover')).toHaveCSS('background-color', await tokenColor(page, 'field'));
      await expect(rest).toHaveCSS('transition-duration', '0.12s, 0.12s, 0.12s');
      await main.focus();
      await expect(main).toHaveCSS('box-shadow', await shadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 calc(var(--focus-ring-width) + var(--focus-halo-width)) var(--accent-soft)'));
      await rest.scrollIntoViewIfNeeded();
      const box = (await rest.boundingBox())!;
      await page.mouse.move(box.x + 8, box.y + 8);
      await page.mouse.down();
      await expect(rest).toHaveCSS('transition-duration', '0.08s');
      await expect(rest).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
      await page.mouse.up();
      await expect(main).toHaveCSS('box-shadow', 'none');
      await expect(tile(page, 'state-Pressed')).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
      const focus = tile(page, 'state-Focus').locator('.places-tile-main');
      await expect(focus).toHaveCSS('box-shadow', await shadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 calc(var(--focus-ring-width) + var(--focus-halo-width)) var(--accent-soft)'));
      const selected = tile(page, 'state-selected');
      await expect(selected).toHaveCSS('box-shadow', await shadow(page, 'var(--sh-1), 0 0 0 var(--places-tile-ring-selected) var(--accent)'));
      await expect(selected.locator('.places-tile-main')).toHaveAttribute('aria-current', 'true');
      const drop = tile(page, 'state-drop');
      await expect(drop).toHaveCSS('background-color', await tokenColor(page, 'accent-soft'));
      expect(await drop.evaluate(el => getComputedStyle(el).boxShadow)).toContain('1.5px');
      const label = drop.locator('.places-tile-drop');
      await expect(label).toHaveText('Add here');
      await expect(label).toHaveCSS('font-size', '11px');
      await expect(label).toHaveCSS('color', await tokenColor(page, 'accent'));
      await expect(tile(page, 'state-drag')).toHaveCSS('opacity', '0.7');
      await expect(tile(page, 'state-disabled')).toHaveCSS('opacity', '0.6');
      await expect(tile(page, 'state-disabled').locator('.places-tile-main')).toHaveCSS('opacity', '1');
      await expect(tile(page, 'state-disabled').locator('.places-tile-main')).toBeDisabled();
      await expect(tile(page, 'state-failed').locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'danger'));

      // New place: no fill, a 1px line inside the edge, a 16px plus and a 12px label.
      const add = page.locator('.places-tile[data-mode="new"]').first();
      await expect(add).toHaveCSS('height', '84px');
      await expect(add).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      expect(await add.evaluate(el => getComputedStyle(el).boxShadow)).toMatch(/inset/);
      await expect(add.locator('.app-icon')).toHaveCSS('width', '16px');
      await expect(add.locator('.places-tile-new-label')).toHaveCSS('font-size', '12px');
      // Creating: five swatches at 12px, the chosen one ringed, the rest at half strength.
      const creating = page.locator('.places-tile[data-mode="creating"]').first();
      await expect(creating.locator('.place-swatch')).toHaveCount(5);
      await expect(creating.locator('.place-swatch[data-selected]')).toHaveCount(1);
      await expect(creating.locator('.place-swatch[data-dimmed]').first()).toHaveCSS('opacity', '0.5');
      await expect(creating.locator('.places-tile-hint')).toHaveText('↵ create · Esc cancel');
    });

    test('attention rows: 36px, neutral text, 6px glyph colour only', async ({ page }) => {
      await open(page, theme);
      const row = page.locator('[data-attention-id="specimen-a1"]');
      await expect(row).toHaveCSS('height', '36px');
      await expect(row).toHaveCSS('border-top-left-radius', '9px');
      const main = row.locator('.places-row-main');
      await expect(main).toHaveCSS('padding-left', '12px');
      await expect(main).toHaveCSS('column-gap', '12px');
      await expect(row.locator('.places-row-text')).toHaveCSS('font-size', '13px');
      await expect(row.locator('.places-row-text')).toHaveCSS('color', await tokenColor(page, 'ink'));
      await expect(row.locator('.places-row-muted')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(row.locator('.places-row-aside')).toHaveCSS('font-size', '12px');
      await expect(row.locator('.places-row-aside')).toHaveText('needs you');
      await expect(row.locator('.status-mark-dot')).toHaveCSS('height', '6px');
      for (const [id, token] of [['specimen-a1', 'amber'], ['specimen-a2', 'accent'], ['specimen-a3', 'danger']]) {
        await expect(page.locator(`[data-attention-id="${id}"] .status-mark`)).toHaveCSS('color', await tokenColor(page, token));
        // Colour is on the glyph only: the text never turns amber, accent or red.
        await expect(page.locator(`[data-attention-id="${id}"] .places-row-text`)).toHaveCSS('color', await tokenColor(page, 'ink'));
      }
      const rows = page.getByRole('list', { name: 'Needing attention' });
      expect(await rows.evaluate(el => getComputedStyle(el).marginLeft)).toBe('-12px');
      await expect(page.locator('[data-attention-id="specimen-Hover"]')).toHaveCSS('background-color', await tokenColor(page, 'field'));
      await expect(page.locator('[data-attention-id="specimen-Pressed"]')).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
      await expect(page.locator('[data-attention-id="specimen-a4"] .places-row-main')).toBeDisabled();
    });

    test('chat rows: home 44px grid and history card', async ({ page }) => {
      await open(page, theme);
      const row = chat(page, 'specimen-c1');
      await expect(row).toHaveCSS('height', '44px');
      await expect(row).toHaveCSS('border-top-left-radius', '9px');
      const main = row.locator('.places-chat-main');
      await expect(main).toHaveCSS('grid-template-columns', /^18px [\d.]+px 56px$/);
      await expect(main).toHaveCSS('column-gap', '12px');
      await expect(main).toHaveCSS('padding-left', '12px');
      await expect(row.locator('.places-chat-title')).toHaveCSS('font-size', '13px');
      await expect(row.locator('.places-chat-excerpt')).toHaveCSS('font-size', '11px');
      await expect(row.locator('.places-chat-excerpt')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(row.locator('.places-chat-time')).toHaveCSS('font-size', '11px');
      await expect(row.locator('.places-chat-time')).toHaveCSS('text-align', 'right');
      await expect(row.locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'accent'));
      await expect(row).toHaveAttribute('data-model', 'DS Flash');
      await expect(row.locator('time')).toHaveAttribute('datetime', '2026-10-09T09:00:00Z');
      // Settled rows carry the 13px message glyph, not a dot.
      await expect(chat(page, 'specimen-c2').locator('.app-icon')).toHaveCSS('width', '13px');
      await expect(chat(page, 'specimen-c2').locator('.status-mark')).toHaveCount(0);
      // Long text fades under a mask: no ellipsis, and the mask is set.
      const title = chat(page, 'specimen-c4').locator('.places-chat-title');
      await expect(title).not.toHaveCSS('text-overflow', 'ellipsis');
      await expect(title).not.toHaveCSS('mask-image', 'none');
      expect(await title.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true);
      await expect(chat(page, 'specimen-c3')).toHaveCSS('background-color', await tokenColor(page, 'field'));
      await expect(chat(page, 'specimen-c3').locator('.places-chat-main')).toHaveAttribute('aria-current', 'true');
      await expect(chat(page, 'specimen-Hover')).toHaveCSS('background-color', await tokenColor(page, 'field'));
      await expect(chat(page, 'specimen-Pressed')).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
      await expect(chat(page, 'specimen-c6').locator('.places-chat-main')).toBeDisabled();
      await expect(chat(page, 'specimen-c7')).toHaveCSS('opacity', '0.7');
      await expect(chat(page, 'specimen-c4').locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'amber'));
      await expect(chat(page, 'specimen-c5').locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'danger'));

      const history = chat(page, 'specimen-h2');
      await expect(history).toHaveCSS('border-top-left-radius', '10px');
      await expect(history).toHaveCSS('background-color', await tokenColor(page, 'field'));
      await expect(history.locator('.places-chat-main')).toHaveCSS('padding', '12px 14px');
      await expect(history.locator('.places-chat-main')).toHaveCSS('grid-template-columns', /^18px [\d.]+px 64px$/);
      await expect(history.locator('.places-chat-title')).toHaveCSS('font-weight', '500');
      await expect(history.locator('.places-chat-excerpt')).toHaveCSS('font-size', '12px');
      await expect(history.locator('.places-chat-excerpt')).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    });

    test('heading: 28/600/-0.022em title, 16px swatch, 12px breadcrumb, 28px menu', async ({ page }) => {
      await open(page, theme);
      const heading = page.locator('.places-heading').first();
      const title = heading.getByRole('heading', { level: 1 });
      await expect(title).toHaveText('codeaf');
      await expect(title).toHaveCSS('font-size', '28px');
      await expect(title).toHaveCSS('font-weight', '600');
      await expect(title).toHaveCSS('letter-spacing', '-0.616px');
      await expect(title).toHaveCSS('color', await tokenColor(page, 'ink'));
      await expect(heading.locator('.place-swatch')).toHaveCSS('width', '16px');
      await expect(heading.locator('.place-swatch')).toHaveCSS('border-top-left-radius', '4.8px');
      const crumb = heading.getByRole('button', { name: 'All places' });
      await expect(crumb).toHaveCSS('font-size', '12px');
      await expect(crumb).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      const menu = heading.getByRole('button', { name: 'Place actions' });
      const box = (await menu.boundingBox())!;
      expect([box.width, box.height]).toEqual([28, 28]);
      expect((await heading.locator('.places-heading-row').boundingBox())!.height).toBeGreaterThanOrEqual(28);
      // A root with no ancestors draws no breadcrumb and one with no menu draws no button.
      const plain = page.locator('.places-heading').nth(1);
      await expect(plain.getByRole('navigation')).toHaveCount(0);
      await expect(plain.getByRole('button')).toHaveCount(0);
      // The section label is the shared 11/500 role.
      const label = page.getByRole('heading', { level: 2 }).first();
      await expect(label).toHaveCSS('font-size', '11px');
      await expect(label).toHaveCSS('font-weight', '500');
    });
  });
}

test('tiles: click, modifier click, Space and Enter, menu and arrows', async ({ page }) => {
  await open(page);
  const grid = page.getByRole('list', { name: 'Places in codeaf' });
  const marketing = grid.locator('[data-place-id="specimen-marketing"] .places-tile-main');
  await marketing.click();
  await expect(log(page).last()).toHaveText('specimen-marketing:goTo');
  await expect(grid.locator('[data-place-id="specimen-marketing"]')).toHaveAttribute('data-selected', 'true');
  await marketing.click({ modifiers: ['ControlOrMeta'] });
  await expect(log(page).last()).toHaveText('specimen-marketing:newWindow');
  await marketing.click({ button: 'middle' });
  await expect(log(page).last()).toHaveText('specimen-marketing:newWindow');
  // Space opens Quick Look and is not also a press; Enter is Go to.
  const before = await log(page).count();
  await marketing.focus();
  await page.keyboard.press('Space');
  await expect(log(page).last()).toHaveText('specimen-marketing:quickLook');
  await page.keyboard.press('Enter');
  await expect(log(page).last()).toHaveText('specimen-marketing:goTo');
  const lines = await log(page).allTextContents();
  expect(lines.slice(-2)).toEqual(['specimen-marketing:quickLook', 'specimen-marketing:goTo']);
  expect(lines.length).toBeGreaterThan(before);
  // Arrow keys move between tiles by row and column, Home and End jump to the ends.
  await page.keyboard.press('ArrowRight');
  await expect(grid.locator('[data-place-id="specimen-release"] .places-tile-main')).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(marketing).toBeFocused();
  await page.keyboard.press('Home');
  await expect(grid.locator('[data-place-id="specimen-software"] .places-tile-main')).toBeFocused();
  await page.keyboard.press('End');
  await expect(grid.getByRole('button', { name: 'New place' })).toBeFocused();
  // Right-click opens the shared menu; its keyboard key does too.
  await marketing.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename' }).click();
  await expect(log(page).last()).toHaveText('specimen-marketing:menu:rename');
  await marketing.focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu', { name: 'Marketing actions' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(marketing).toBeFocused();
});

test('tiles: a dragged chat lights the drop target and lands on it', async ({ page }) => {
  await open(page);
  const target = tile(page, 'specimen-software');
  const source = chat(page, 'specimen-c1');
  const start = await pointOn(source);
  const end = await pointOn(target);
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(start.x + 8, start.y + 4, { steps: 3 });
  await page.mouse.move(end.x, end.y, { steps: 8 });
  await expect(target).toHaveAttribute('data-drop', 'true');
  await expect(target.locator('.places-tile-drop')).toHaveText('Add here');
  await page.mouse.up();
  await expect(target).not.toHaveAttribute('data-drop', 'true');
  await expect(log(page).last()).toHaveText('specimen-software:drop:specimen-c1');
});

test('tiles: inline create takes a name and a tint, Enter creates, Escape cancels', async ({ page }) => {
  await open(page);
  const grid = page.getByRole('list', { name: 'Places in codeaf' });
  await grid.getByRole('button', { name: 'New place' }).click();
  const field = grid.getByRole('textbox', { name: 'Place name' });
  await expect(field).toBeFocused();
  // Enter on an empty name does nothing.
  await page.keyboard.press('Enter');
  await expect(log(page).last()).toHaveText('new:open');
  await field.fill('Talks');
  const radios = grid.getByRole('radio');
  await expect(radios).toHaveCount(5);
  await expect(grid.getByRole('radio', { name: 'Iris' })).toHaveAttribute('aria-checked', 'true');
  await grid.getByRole('radio', { name: 'Sage' }).click();
  await expect(grid.getByRole('radio', { name: 'Sage' })).toHaveAttribute('aria-checked', 'true');
  await grid.getByRole('radio', { name: 'Sage' }).press('ArrowRight');
  await expect(grid.getByRole('radio', { name: 'Tide' })).toBeFocused();
  await expect(grid.getByRole('radio', { name: 'Tide' })).toHaveAttribute('aria-checked', 'true');
  await field.fill('Software');
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await field.fill('Talks');
  await expect(field).not.toHaveAttribute('aria-invalid', 'true');
  await field.press('Enter');
  await expect(log(page).last()).toHaveText('create:Talks:tide');
  await expect(grid.getByRole('button', { name: 'New place' })).toBeVisible();
  await grid.getByRole('button', { name: 'New place' }).click();
  await grid.getByRole('textbox', { name: 'Place name' }).press('Escape');
  await expect(log(page).last()).toHaveText('create:cancel');
});

test('rows: click, modifier click, middle click, menu and drag source', async ({ page }) => {
  await open(page);
  const row = chat(page, 'specimen-c1').locator('.places-chat-main');
  await row.click();
  await expect(log(page).last()).toHaveText('c1:open');
  await row.click({ modifiers: ['ControlOrMeta'] });
  await expect(log(page).last()).toHaveText('c1:newTab');
  await row.click({ button: 'middle' });
  await expect(log(page).last()).toHaveText('c1:newTab');
  await row.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  await expect(log(page).last()).toHaveText('c1:menu:delete');
  const carried = chat(page, 'specimen-c1');
  const software = tile(page, 'specimen-software');
  // The clicks above scroll the row to the middle, which pushes the tile off the top. Both have to be under the cursor before the press.
  await page.evaluate(() => {
    const chatRow = document.querySelector('[data-chat-id="specimen-c1"]');
    const tileRow = document.querySelector('[data-place-id="specimen-software"]');
    if (!chatRow || !tileRow) return;
    const margin = 16;
    const top = Math.min(tileRow.getBoundingClientRect().top, chatRow.getBoundingClientRect().top);
    if (top < margin) window.scrollBy(0, top - margin);
    const bottom = Math.max(tileRow.getBoundingClientRect().bottom, chatRow.getBoundingClientRect().bottom);
    if (bottom > window.innerHeight - margin) window.scrollBy(0, bottom - (window.innerHeight - margin));
  });
  const from = await pointOn(carried);
  const onto = await pointOn(software);
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 8, from.y + 4, { steps: 3 });
  await page.mouse.move(onto.x, onto.y, { steps: 8 });
  await page.mouse.up();
  await expect(log(page).last()).toHaveText('specimen-software:drop:specimen-c1');
  const attention = page.locator('[data-attention-id="specimen-a1"] .places-row-main');
  await attention.click();
  await expect(log(page).last()).toHaveText('a1:open');
  await attention.click({ modifiers: ['ControlOrMeta'] });
  await expect(log(page).last()).toHaveText('a1:newTab');
  // A row without a new-tab handler treats a modifier click as a plain open.
  await chat(page, 'specimen-c2').locator('.places-chat-main').click({ modifiers: ['ControlOrMeta'] });
  await expect(log(page).last()).toHaveText('c2:open');
});

test('hover and press fill rows, the keyboard ring shows only on keyboard focus', async ({ page }) => {
  await open(page);
  const row = chat(page, 'specimen-c2');
  await row.hover();
  await expect(row).toHaveCSS('background-color', await tokenColor(page, 'field'));
  await page.mouse.down();
  await expect(row).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
  await page.mouse.up();
  await page.mouse.move(0, 0);
  const button = chat(page, 'specimen-c2').locator('.places-chat-main');
  await page.keyboard.press('Tab');
  await button.focus();
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Tab');
  await expect(button).toBeFocused();
  expect(await button.evaluate(el => getComputedStyle(el).boxShadow)).toContain('2px');
});

test('heading: breadcrumb, menu and inline rename', async ({ page }) => {
  await open(page);
  const heading = page.locator('.places-heading').first();
  await heading.getByRole('button', { name: 'All places' }).click();
  await expect(log(page).last()).toHaveText('crumb:root');
  await heading.getByRole('button', { name: 'All places' }).click({ modifiers: ['ControlOrMeta'] });
  await expect(log(page).last()).toHaveText('crumb:root:newWindow');
  await heading.getByRole('button', { name: 'Place actions' }).click();
  await page.getByRole('menuitem', { name: 'Rename inline' }).click();
  const field = heading.getByRole('textbox', { name: 'Place name' });
  await expect(field).toBeFocused();
  await expect(field).toHaveCSS('font-size', '28px');
  await field.fill('codeaf desktop');
  await field.press('Enter');
  await expect(heading.getByRole('heading', { level: 1 })).toHaveText('codeaf desktop');
  await expect(log(page).last()).toHaveText('rename:codeaf desktop');
  await heading.getByRole('button', { name: 'Place actions' }).click();
  await page.getByRole('menuitem', { name: 'Rename inline' }).click();
  const again = heading.getByRole('textbox', { name: 'Place name' });
  await again.fill('   ');
  await again.press('Enter');
  await expect(again).toHaveAttribute('aria-invalid', 'true');
  await again.fill('x');
  await expect(again).not.toHaveAttribute('aria-invalid', 'true');
  await again.press('Escape');
  await expect(heading.getByRole('heading', { level: 1 })).toHaveText('codeaf desktop');
});

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 480, 1200]) {
    test(`accessible and contained at ${width}px, ${theme}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await open(page, theme);
      await expectAccessible(page);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
      // Every interactive control stays inside the viewport width and can take focus.
      const bad = await page.evaluate(() => [...document.querySelectorAll<HTMLElement>('button:not(:disabled), input')].filter(el => {
        const box = el.getBoundingClientRect();
        return box.width > 0 && (box.right > document.documentElement.clientWidth + 1 || box.left < -1);
      }).map(el => el.outerHTML.slice(0, 80)));
      expect(bad).toEqual([]);
    });
  }
}

test('320px: the tile grid wraps and arrow order stays in tile order', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await open(page);
  const grid = page.getByRole('list', { name: 'Places in codeaf' });
  const boxes = await grid.locator('.places-tile').evaluateAll(els => els.map(el => { const r = el.getBoundingClientRect(); return [Math.round(r.left), Math.round(r.top), Math.round(r.width)]; }));
  expect(new Set(boxes.map(box => box[0])).size).toBeLessThanOrEqual(2);
  for (const box of boxes) expect(box[2]).toBeGreaterThan(100);
  await grid.locator('[data-place-id="specimen-software"] .places-tile-main').focus();
  // The grid is one tab stop, so the arrow keys carry focus to the next tile in order.
  await page.keyboard.press('ArrowRight');
  await expect(grid.locator('[data-place-id="specimen-reading"] .places-tile-main')).toBeFocused();
});

test('reduced motion removes the fill transition but keeps the state', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await open(page);
  const row = chat(page, 'specimen-c2');
  await expect(row).toHaveCSS('transition-duration', /^0s(, 0s)*$/);
  await row.hover();
  await expect(row).toHaveCSS('background-color', await tokenColor(page, 'field'));
});

test('tile grid: one tab stop that follows focus, four columns, Command Enter opens a window', async ({ page }) => {
  await open(page);
  const grid = page.getByRole('list', { name: 'Places in codeaf' });
  const stops = grid.locator('[data-places-tile-focusable][tabindex="0"]');
  await expect(stops).toHaveCount(1);
  const release = grid.locator('[data-place-id="specimen-release"] .places-tile-main');
  await release.focus();
  await expect(stops).toHaveCount(1);
  await expect(release).toHaveAttribute('tabindex', '0');
  await page.keyboard.press('ControlOrMeta+Enter');
  await expect(log(page).last()).toHaveText('specimen-release:newWindow');
  // Wide pages draw four equal columns; the New place tile is the last cell.
  // The harness column is only 624px, so the grid is given a page-sized width to stand in for a wide Home.
  await grid.evaluate(el => { el.style.width = '1100px'; });
  const columns = await grid.evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length);
  expect(columns).toBe(4);
  const last = await grid.locator('.places-tile').last().getAttribute('data-mode');
  expect(last).toBe('new');
  // A narrow page keeps tiles at 160px or wider by dropping columns.
  await grid.evaluate(el => { el.style.width = '400px'; });
  const narrow = await grid.evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').map(parseFloat));
  expect(narrow.length).toBeLessThan(4);
  for (const width of narrow) expect(width).toBeGreaterThanOrEqual(160);
});
