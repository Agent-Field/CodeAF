import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';

const NOW = new Date('2026-10-10T12:00:00Z');
const fresh = (): Scenario => ({ initial: { entries: [], title: '', sessionFile: sessionFileFor('mock-1') }, turns: [] });

/** Reading holds two chats that would become unplaced, one that keeps Papers, and two child places. */
function garden(): PlacesSeed {
  return {
    places: [
      { name: 'Reading', tint: 'tide' },
      { name: 'Papers', parents: ['Reading'] },
      { name: 'Notes', parents: ['Reading'] },
    ],
    chats: [
      { id: 'c1', title: 'Essay one', places: ['Reading'] },
      { id: 'c2', title: 'Essay two', places: ['Reading'] },
      { id: 'c3', title: 'Shared note', places: ['Reading', 'Papers'] },
    ],
    live: ['mock-1'],
  };
}

const tile = (page: Page, name: string) => page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: new RegExp(`^${name}$`) }) });
/** The delete toast only. The follow-up "Undone: …" line repeats the same sentence, so a substring match would still see it. */
const deleteToast = (page: Page) => page.locator('.toast-region .toast').filter({ hasText: /^Deleted “Reading”\. No chat was deleted\./ });

async function openAllPlaces(page: Page) {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, garden());
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await page.keyboard.press(`${mac ? 'Meta' : 'Control'}+Shift+KeyP`);
  await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
  return { engine, places };
}

test('d5-pl-test-org: delete place names unplaced chats and places that move up, then Undo for 10s', async ({ page }) => {
  test.setTimeout(60_000);
  const { engine, places } = await openAllPlaces(page);
  const chatIds = () => places.state().chats.map(chat => chat.id).sort();
  const before = chatIds();
  expect(before).toEqual(['c1', 'c2', 'c3']);

  await tile(page, 'Reading').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete place…' }).click();
  const confirm = page.getByRole('group', { name: 'Delete “Reading”? 2 chats become unplaced · 2 places move up. No chat is deleted.' });
  await expect(confirm).toBeVisible();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(confirm.getByRole('button', { name: 'Cancel' })).toHaveClass(/button-quiet/);
  await expect(confirm.getByRole('button', { name: 'Delete place', exact: true })).toHaveClass(/button-danger/);
  expect(places.posts('/delete')).toEqual([]);

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    const measured = await confirm.evaluate(el => {
      const question = el.querySelector('.place-delete-question');
      const buttons = [...el.querySelectorAll('button')];
      if (!question || buttons.length < 2) return null;
      const painted = (token: string) => {
        const probe = document.createElement('span');
        probe.style.color = `var(${token})`;
        el.appendChild(probe);
        const color = getComputedStyle(probe).color;
        probe.remove();
        return color;
      };
      const row = getComputedStyle(el);
      const ask = getComputedStyle(question);
      const cancel = getComputedStyle(buttons[0]);
      const danger = getComputedStyle(buttons[1]);
      return {
        display: row.display,
        align: row.alignItems,
        background: row.backgroundColor,
        field: painted('--field'),
        ink: painted('--ink'),
        dangerInk: painted('--danger'),
        borderLeft: row.borderLeftWidth,
        question: ask.color,
        mask: ask.getPropertyValue('mask-image') || ask.getPropertyValue('-webkit-mask-image'),
        cancelBg: cancel.backgroundColor,
        deleteBg: danger.backgroundColor,
        deleteColor: danger.color,
        hoverMs: danger.transitionDuration,
      };
    });
    expect(measured, theme).toBeTruthy();
    expect(measured!.display, theme).toBe('flex');
    expect(measured!.align, theme).toBe('center');
    expect(measured!.borderLeft, theme).toBe('0px');
    expect(measured!.background, theme).toBe(measured!.field);
    expect(measured!.question, theme).toBe(measured!.ink);
    expect(measured!.question, theme).not.toBe(measured!.dangerInk);
    expect(measured!.mask, theme).toContain('linear-gradient');
    expect(measured!.deleteBg, theme).not.toBe(measured!.cancelBg);
    expect(measured!.deleteColor, theme).not.toBe(measured!.ink);
    expect(measured!.deleteColor, theme).not.toBe(measured!.question);
    expect(measured!.hoverMs, theme).toMatch(/0\.12s/);
  }

  const danger = confirm.getByRole('button', { name: 'Delete place', exact: true });
  await danger.hover();
  const hovered = await danger.evaluate(el => getComputedStyle(el).filter);
  expect(hovered).toContain('brightness');
  await page.mouse.down();
  const pressed = await danger.evaluate(el => getComputedStyle(el).transitionDuration);
  await page.mouse.move(0, 0);
  await page.mouse.up();
  expect(pressed).toMatch(/0\.08s/);
  await expect(confirm).toBeVisible();

  await confirm.getByRole('button', { name: 'Cancel' }).focus();
  await page.keyboard.press('Tab');
  const ring = await danger.evaluate(el => getComputedStyle(el).boxShadow);
  expect(ring).not.toBe('none');
  expect(await page.evaluate(() => document.documentElement.dataset.input)).toBeUndefined();

  await page.keyboard.press('Escape');
  await expect(confirm).toHaveCount(0);
  await expect(tile(page, 'Reading')).toBeVisible();
  expect(places.posts('/delete')).toEqual([]);
  expect(chatIds()).toEqual(before);

  await tile(page, 'Reading').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete place…' }).click();
  await page.getByRole('group', { name: /2 chats become unplaced/ }).getByRole('button', { name: 'Delete place', exact: true }).click();
  await expect.poll(() => places.posts('/delete').length).toBe(1);
  expect(engine.calls.filter(call => /\/history\/delete|\/sessions\/[^/]+\/delete/.test(call.path))).toEqual([]);
  expect(chatIds()).toEqual(before);
  await expect(tile(page, 'Reading')).toHaveCount(0);
  await expect(tile(page, 'Papers')).toBeVisible();
  await expect(tile(page, 'Notes')).toBeVisible();
  await expect(page.getByText('Essay one')).toBeVisible();
  await expect(page.getByText('Essay two')).toBeVisible();

  await page.clock.fastForward(300);
  await expect(deleteToast(page)).toBeVisible();
  await expect(deleteToast(page).getByRole('button', { name: 'Undo' })).toBeVisible();
  await page.clock.fastForward(9_000);
  await expect(deleteToast(page)).toBeVisible();
  await deleteToast(page).getByRole('button', { name: 'Undo' }).click();
  await expect(deleteToast(page)).toHaveCount(0);
  await expect(page.getByText('Undone: Deleted “Reading”. No chat was deleted.')).toBeVisible();
  await expect.poll(() => places.posts('/places/undo').length).toBe(1);
  await expect(tile(page, 'Reading')).toBeVisible();
  await expect(tile(page, 'Papers')).toHaveCount(0);
  expect(chatIds()).toEqual(before);

  await tile(page, 'Reading').locator('.places-tile-main').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete place…' }).click();
  await page.getByRole('group', { name: /2 chats become unplaced/ }).getByRole('button', { name: 'Delete place', exact: true }).click();
  await page.clock.fastForward(300);
  await expect(deleteToast(page)).toBeVisible();
  await page.clock.fastForward(10_000);
  await expect(deleteToast(page)).toHaveCount(0);
  await expect(tile(page, 'Reading')).toHaveCount(0);
  expect(chatIds()).toEqual(before);
  expect(places.posts('/delete')).toHaveLength(2);
  expect(places.posts('/places/undo')).toHaveLength(1);
});

test('d5-pl-test-org: tint row is five squares, and left/right then Enter picks one', async ({ page }) => {
  test.setTimeout(60_000);
  const { places } = await openAllPlaces(page);
  const reading = tile(page, 'Reading');
  await reading.locator('.places-tile-main').click({ button: 'right' });
  const group = page.getByRole('radiogroup', { name: 'Tint' });
  await expect(group).toBeVisible();
  await expect(group.getByRole('radio', { name: 'Graphite' })).toHaveCount(0);
  const names = ['Tide', 'Rose', 'Sage', 'Sand', 'Iris'];
  for (const name of names) await expect(group.getByRole('radio', { name })).toHaveCount(1);
  await expect(group.getByRole('radio', { name: 'Tide' })).toHaveAttribute('aria-checked', 'true');

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    const measured = await group.evaluate(el => {
      const row = el.querySelector('.menu-swatches');
      const squares = [...el.querySelectorAll<HTMLElement>('.menu-swatch')];
      const selected = el.querySelector<HTMLElement>('.menu-swatch[data-selected]');
      const menu = el.closest('.app-menu');
      if (!row || squares.length !== 5 || !selected || !menu) return null;
      const rowStyle = getComputedStyle(row);
      const menuStyle = getComputedStyle(menu);
      const boxes = squares.map(square => square.getBoundingClientRect());
      const menuBox = menu.getBoundingClientRect();
      const probe = document.createElement('span');
      probe.style.color = 'var(--places-swatch-tide)';
      el.appendChild(probe);
      const tide = getComputedStyle(probe).color;
      probe.remove();
      return {
        width: boxes[0].width,
        height: boxes[0].height,
        radius: getComputedStyle(squares[0]).borderTopLeftRadius,
        gap: boxes[1].left - boxes[0].right,
        padTop: rowStyle.paddingTop,
        padRight: rowStyle.paddingRight,
        padBottom: rowStyle.paddingBottom,
        padLeft: rowStyle.paddingLeft,
        inset: boxes[0].left - menuBox.left - parseFloat(menuStyle.paddingLeft),
        borderLeft: getComputedStyle(squares[0]).borderLeftWidth,
        shadow: getComputedStyle(selected).boxShadow,
        tide,
        selectedColor: getComputedStyle(selected).color,
      };
    });
    expect(measured, theme).toBeTruthy();
    expect(measured!.width, theme).toBeCloseTo(16, 0);
    expect(measured!.height, theme).toBeCloseTo(16, 0);
    expect(measured!.radius, theme).toBe('5px');
    expect(measured!.gap, theme).toBeCloseTo(6, 0);
    expect(measured!.padTop, theme).toBe('6px');
    expect(measured!.padRight, theme).toBe('10px');
    expect(measured!.padBottom, theme).toBe('8px');
    expect(measured!.padLeft, theme).toBe('34px');
    expect(measured!.inset, theme).toBeCloseTo(34, 0);
    expect(measured!.borderLeft, theme).toBe('0px');
    expect(measured!.shadow, theme).toContain('2px');
    expect(measured!.shadow, theme).toContain('3.5px');
    expect(measured!.selectedColor, theme).toBe(measured!.tide);
  }

  const tide = group.getByRole('radio', { name: 'Tide' });
  const box = await tide.boundingBox();
  expect(box).toBeTruthy();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
  await page.mouse.down();
  const pressed = await tide.evaluate(el => getComputedStyle(el).transitionDuration);
  expect(pressed).toMatch(/0\.08s/);
  await page.mouse.move(0, 0);
  await page.mouse.up();
  await expect(group).toBeVisible();

  for (let step = 0; step < 8 && await group.evaluate(el => el !== document.activeElement); step += 1) await page.keyboard.press('ArrowDown');
  await expect(group).toBeFocused();
  const labelFill = await group.locator('.menu-swatch-label').evaluate(el => {
    const probe = document.createElement('span');
    probe.style.background = 'var(--field-2)';
    el.appendChild(probe);
    const field = getComputedStyle(probe).backgroundColor;
    probe.remove();
    const hover = getComputedStyle(el).transitionDuration;
    const ring = getComputedStyle(el.closest('.menu-swatch-entry')!).boxShadow;
    // The suite clock is frozen, so a 120ms fill transition never leaves its first frame. Dropping it reveals the settled colour.
    el.style.transition = 'none';
    return { fill: getComputedStyle(el).backgroundColor, field, hover, ring };
  });
  expect(labelFill.fill).toBe(labelFill.field);
  expect(labelFill.hover).toMatch(/0\.12s/);
  expect(labelFill.ring).toBe('none');

  await page.keyboard.press('ArrowLeft');
  await expect(group.getByRole('radio', { name: 'Iris' })).toHaveAttribute('aria-checked', 'true');
  await page.keyboard.press('ArrowRight');
  await expect(tide).toHaveAttribute('aria-checked', 'true');
  await page.keyboard.press('ArrowRight');
  await expect(group.getByRole('radio', { name: 'Rose' })).toHaveAttribute('aria-checked', 'true');
  await page.keyboard.press('Enter');
  await expect(group).toHaveCount(0);
  await expect.poll(() => places.calls.filter(call => call.method === 'POST' && call.body.tint === 'rose')).toHaveLength(1);
  await expect(reading).toHaveAttribute('data-tint-name', 'rose');
});

/** One DataTransfer for the whole gesture, so the type written at dragstart is still there at the drop. */
async function drag(page: Page, source: Locator, target: Locator, alt = false, commit = true) {
  const data = await page.evaluateHandle(() => new DataTransfer());
  await source.dispatchEvent('dragstart', { dataTransfer: data, bubbles: true });
  await target.dispatchEvent('dragenter', { dataTransfer: data, altKey: alt, bubbles: true });
  await target.dispatchEvent('dragover', { dataTransfer: data, altKey: alt, bubbles: true });
  if (commit) await target.dispatchEvent('drop', { dataTransfer: data, altKey: alt, bubbles: true });
  await source.dispatchEvent('dragend', { dataTransfer: data, bubbles: true });
  return data;
}

test('d5-pl-test-org: dragging onto a tile says Add here, Option moves, and the menu adds without a drag', async ({ page }) => {
  test.setTimeout(60_000);
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, {
    places: [
      { name: 'Reading', tint: 'tide' },
      { name: 'Papers', parents: ['Reading'] },
      { name: 'Notes', parents: ['Reading'] },
      { name: 'Clips', parents: ['Reading'] },
    ],
    chats: [
      { id: 'c1', title: 'Essay one', places: ['Reading'] },
      { id: 'c2', title: 'Essay two', places: ['Reading'] },
      { id: 'c3', title: 'Shared note', places: ['Reading', 'Papers'] },
    ],
    live: ['mock-1'],
  });
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await page.keyboard.press(`${mac ? 'Meta' : 'Control'}+Shift+KeyP`);
  await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
  await tile(page, 'Reading').locator('.places-tile-main').click();
  await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
  await expect(page.getByText('Essay one')).toBeVisible();

  const notes = tile(page, 'Notes');
  const essay = page.locator('[data-chat-id="c1"]');
  const parentPosts = () => places.posts('/parents');
  const memberPosts = () => places.posts('/members');

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    const data = await page.evaluateHandle(() => new DataTransfer());
    await essay.dispatchEvent('dragstart', { dataTransfer: data, bubbles: true });
    await notes.dispatchEvent('dragenter', { dataTransfer: data, altKey: theme === 'dark', bubbles: true });
    await notes.dispatchEvent('dragover', { dataTransfer: data, altKey: theme === 'dark', bubbles: true });
    await expect(notes).toHaveAttribute('data-drop', 'true');
    await expect(essay).toHaveAttribute('data-dragging', 'true');
    const measured = await notes.evaluate((el, holding) => {
      const label = el.querySelector<HTMLElement>('.places-tile-drop');
      if (!label) return null;
      const tileBox = el.getBoundingClientRect();
      const labelBox = label.getBoundingClientRect();
      const dragging = el.ownerDocument.querySelector<HTMLElement>('[data-dragging]');
      // A previous theme left an inline transition. Clear it so this read is the stylesheet's 120ms.
      el.style.transition = '';
      if (dragging) dragging.style.transition = '';
      const transition = getComputedStyle(el).transitionDuration;
      // The suite clock is frozen, so a 120ms fill never leaves its first frame. Dropping the transition reveals the settled colour.
      el.style.transition = 'none';
      if (dragging) dragging.style.transition = 'none';
      const probe = document.createElement('span');
      el.appendChild(probe);
      const painted = (property: 'color' | 'backgroundColor', token: string) => {
        probe.style.color = '';
        probe.style.backgroundColor = '';
        probe.style[property] = `var(${token})`;
        return getComputedStyle(probe)[property];
      };
      probe.style.opacity = 'var(--opacity-subtle)';
      const subtle = getComputedStyle(probe).opacity;
      const accentSoft = painted('backgroundColor', '--accent-soft');
      const field = painted('backgroundColor', '--field');
      const accent = painted('color', '--accent');
      probe.remove();
      const tile = getComputedStyle(el);
      const words = getComputedStyle(label);
      return {
        text: label.textContent,
        height: tileBox.height,
        radius: tile.borderTopLeftRadius,
        background: tile.backgroundColor,
        accentSoft,
        field,
        shadow: tile.boxShadow,
        borderLeft: tile.borderLeftWidth,
        fontSize: words.fontSize,
        fontWeight: words.fontWeight,
        color: words.color,
        accent,
        right: tileBox.right - labelBox.right,
        bottom: tileBox.bottom - labelBox.bottom,
        transition,
        opacity: getComputedStyle(el.ownerDocument.querySelector('[data-dragging]')!).opacity,
        subtle,
        holding,
      };
    }, theme === 'dark');
    expect(measured, theme).toBeTruthy();
    expect(measured!.text, theme).toBe('Add here');
    expect(measured!.height, theme).toBeCloseTo(84, 0);
    expect(measured!.radius, theme).toBe('12px');
    expect(measured!.background, theme).toBe(measured!.accentSoft);
    expect(measured!.background, theme).not.toBe(measured!.field);
    expect(measured!.shadow, theme).toContain('1.5px');
    expect(measured!.borderLeft, theme).toBe('0px');
    expect(measured!.fontSize, theme).toBe('11px');
    expect(measured!.fontWeight, theme).toBe('500');
    expect(measured!.color, theme).toBe(measured!.accent);
    expect(measured!.right, theme).toBeCloseTo(12, 0);
    expect(measured!.bottom, theme).toBeCloseTo(12, 0);
    expect(measured!.transition, theme).toMatch(/0\.12s/);
    expect(measured!.opacity, theme).toBe(measured!.subtle);
    await notes.hover();
    const hovered = await notes.evaluate(el => {
      el.style.transition = 'none';
      const probe = document.createElement('span');
      probe.style.backgroundColor = 'var(--accent-soft)';
      el.appendChild(probe);
      const soft = getComputedStyle(probe).backgroundColor;
      probe.remove();
      return { background: getComputedStyle(el).backgroundColor, soft };
    });
    expect(hovered.background, theme).toBe(hovered.soft);
    // A synthetic drag reports dropEffect "none": the browser only keeps copy or move during a real drag. The write below is what Option changes.
    await essay.dispatchEvent('dragend', { dataTransfer: data, bubbles: true });
    await expect(notes).not.toHaveAttribute('data-drop');
  }
  expect(parentPosts()).toEqual([]);
  expect(memberPosts()).toEqual([]);
  expect(engine.calls.filter(call => call.method === 'POST' && /\/places\/[^/]+\/(members|parents)/.test(call.path))).toEqual([]);

  const clips = tile(page, 'Clips');
  const self = await page.evaluateHandle(() => new DataTransfer());
  await clips.locator('.places-tile-main').dispatchEvent('dragstart', { dataTransfer: self, bubbles: true });
  await clips.dispatchEvent('dragenter', { dataTransfer: self, bubbles: true });
  await clips.dispatchEvent('dragover', { dataTransfer: self, bubbles: true });
  await expect(clips).toHaveAttribute('data-dragging', 'true');
  await expect(clips).not.toHaveAttribute('data-drop');
  await clips.locator('.places-tile-main').dispatchEvent('dragend', { dataTransfer: self, bubbles: true });
  expect(parentPosts()).toEqual([]);

  await notes.locator('.places-tile-main').focus();
  await page.keyboard.press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Add to another place…' }).click();
  const addPlace = page.getByRole('dialog', { name: 'Add “Notes” to another place…' });
  await expect(addPlace).toBeVisible();
  await addPlace.getByRole('option', { name: /^Papers/ }).click();
  const papersId = places.id('Papers');
  const notesId = places.id('Notes');
  const readingId = places.id('Reading');
  await expect.poll(() => places.state().places.find(place => place.name === 'Notes')?.parents.slice().sort()).toEqual([papersId, readingId].sort());

  await page.locator('[data-chat-id="c1"] .places-chat-main').focus();
  await page.keyboard.press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Add to a place…' }).click();
  await page.getByRole('dialog', { name: 'Add “Essay one” to a place…' }).getByRole('option', { name: /^Notes/ }).click();
  await expect.poll(() => places.state().members.filter(row => row.chatId === 'c1').map(row => row.placeId).sort()).toEqual([notesId, readingId].sort());

  await drag(page, page.locator('[data-chat-id="c2"]'), tile(page, 'Papers'));
  await expect.poll(() => memberPosts().at(-1)?.body).toMatchObject({ chats: ['c2'] });
  expect(memberPosts().at(-1)?.body.moveFrom).toBeUndefined();
  await expect.poll(() => places.state().members.filter(row => row.chatId === 'c2').map(row => row.placeId).sort()).toEqual([papersId, readingId].sort());

  await drag(page, page.locator('[data-chat-id="c3"]'), notes, true);
  await expect.poll(() => memberPosts().at(-1)?.body).toMatchObject({ chats: ['c3'], moveFrom: readingId });
  await expect.poll(() => places.state().members.filter(row => row.chatId === 'c3').map(row => row.placeId).sort()).toEqual([notesId, papersId].sort());

  await drag(page, clips.locator('.places-tile-main'), notes);
  await expect.poll(() => parentPosts().at(-1)?.body).toMatchObject({ add: notesId });
  await expect.poll(() => places.state().places.find(place => place.name === 'Clips')?.parents.slice().sort()).toEqual([notesId, readingId].sort());

  await drag(page, clips.locator('.places-tile-main'), tile(page, 'Papers'), true);
  await expect.poll(() => parentPosts().at(-1)?.body).toMatchObject({ set: [papersId] });
  await expect.poll(() => places.state().places.find(place => place.name === 'Clips')?.parents).toEqual([papersId]);
});
