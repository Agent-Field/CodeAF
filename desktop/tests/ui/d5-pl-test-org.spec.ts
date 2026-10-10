import { test, expect, type Page } from '@playwright/test';
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
