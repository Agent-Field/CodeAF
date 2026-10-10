import { expect, test, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import { trayQuestions } from './support/scenarios-v2';
import { message, openApp } from './support/conversation';
import { expectAccessible } from './contracts';
import { freshWorkspace, workspaceKey } from '../../src/features/tabs/model';
import { newTab } from '../../src/features/tabs/helpers';

// CV-207: an empty conversation takes the window place's tint. Now is graphite.
// CV-289: a notification click for a question already in this conversation brings the
// end of the transcript back and pages the tray to that question.
// Widths are the ones where the column and the tray can reflow. A removed tint or a
// click that no longer scrolls or pages fails these assertions.

const WIDTHS = [320, 600, 1200] as const;
const CHAT = 'c0ffee00c0ffee00';
const SESSION = sessionFileFor(CHAT);
const TARGET = `${CHAT}:consent:2`;
const SLACK = 48;

const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });
const scroller = (page: Page) => page.locator('.conversation-scroll');
const distance = (page: Page) => scroller(page).evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop);

async function themeOf(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
}

/** Canvas and ink of the empty start, read from the tokens the element actually resolves. */
async function tintOf(start: Locator) {
  return start.evaluate(el => {
    const probe = document.createElement('span');
    probe.style.backgroundColor = 'var(--canvas)';
    probe.style.color = 'var(--ink-2)';
    probe.style.fontSize = 'var(--type-h2-size)';
    probe.style.fontWeight = 'var(--font-weight-medium)';
    el.append(probe);
    const probeStyle = getComputedStyle(probe);
    const title = el.querySelector('.empty-start-title')!;
    const titleStyle = getComputedStyle(title);
    const composer = el.querySelector('.composer')!;
    const rect = el.getBoundingClientRect();
    const box = composer.getBoundingClientRect();
    const reading = { canvas: probeStyle.backgroundColor, ink: probeStyle.color, size: probeStyle.fontSize, weight: probeStyle.fontWeight, hue: probeStyle.getPropertyValue('--h').trim() };
    probe.remove();
    return {
      reading,
      canvas: getComputedStyle(el).backgroundColor,
      ink: titleStyle.color,
      size: titleStyle.fontSize,
      weight: titleStyle.fontWeight,
      hue: getComputedStyle(el).getPropertyValue('--h').trim(),
      bodyHue: getComputedStyle(document.body).getPropertyValue('--h').trim(),
      centre: box.x + box.width / 2,
      expectedCentre: rect.x + rect.width / 2,
      middle: box.y + box.height / 2,
      top: rect.top,
      bottom: rect.bottom,
    };
  });
}

async function expectEmptyTint(page: Page, hue: string) {
  const start = page.locator('.conversation-main[data-empty]');
  await expect(start).toBeVisible();
  await expect(start.getByText('What are we building?', { exact: true })).toBeVisible();
  const measured = await tintOf(start);
  // The start paints the place canvas and ink-2. A hardcoded colour, or a hue that is not this place, fails here.
  expect(measured.canvas).toBe(measured.reading.canvas);
  expect(measured.ink).toBe(measured.reading.ink);
  expect(measured.size).toBe(measured.reading.size);
  expect(measured.weight).toBe(measured.reading.weight);
  expect(measured.hue).toBe(hue);
  expect(measured.bodyHue).toBe(hue);
  expect(Math.abs(measured.centre - measured.expectedCentre)).toBeLessThan(1);
  expect(measured.middle).toBeGreaterThan(measured.top + (measured.bottom - measured.top) * 0.25);
  expect(measured.middle).toBeLessThan(measured.top + (measured.bottom - measured.top) * 0.75);
  await expect(start.locator('.suggestions, .splash')).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
}

/** Leave the reader above the end, still close enough that the full tray stays (not the compact bar). */
async function parkAboveEnd(page: Page) {
  const parked = await scroller(page).evaluate(el => {
    const max = el.scrollHeight - el.clientHeight;
    el.scrollTop = Math.max(0, max - 200);
    el.dispatchEvent(new Event('scroll'));
    return { top: el.scrollTop, distance: el.scrollHeight - el.clientHeight - el.scrollTop };
  });
  expect(parked.distance).toBeGreaterThan(SLACK);
  return parked.top;
}

for (const theme of ['light', 'dark'] as const) {
  test(`CV-207 empty start uses the place tint in ${theme}`, async ({ page }) => {
    test.setTimeout(90_000);
    await themeOf(page, theme);
    await installMockEngine(page, { initial: { entries: [], title: 'Quiet chat' } });
    const places = await installMockPlaces(page, {
      places: [{ name: 'Garden', tint: 'rose', pinned: true }],
      chats: [{ id: 'empty-chat', title: 'Quiet chat', places: ['Garden'] }],
    });
    const place = places.id('Garden');
    const workspace = freshWorkspace({ id: place, title: 'Garden' });
    workspace.tabs.push(newTab({ id: 'empty-chat-tab', title: 'Quiet chat', sessionFile: sessionFileFor('empty-chat') }));
    await page.addInitScript(({ key, workspace: saved }) => localStorage.setItem(key, JSON.stringify(saved)), { key: workspaceKey(place), workspace });
    await page.setViewportSize({ width: 1200, height: 800 });
    await openApp(page);
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    // Now scopes graphite onto the empty start itself, so the start stays graphite even if the shell's hue moved.
    await expect(page.locator('.conversation-main[data-empty]')).toHaveAttribute('data-tint', 'graphite');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await expectEmptyTint(page, '250');
    }
    // Shift+Tab from Attach is the keyboard path onto the empty field. The ring is the 2px accent plus the 4px halo.
    await page.setViewportSize({ width: 1200, height: 800 });
    const field = message(page);
    const ring = () => field.evaluate(el => getComputedStyle(el).boxShadow);
    await page.getByRole('button', { name: 'Attach files' }).focus();
    const resting = await ring();
    await page.keyboard.press('Shift+Tab');
    await expect(field).toBeFocused();
    const keyed = await ring();
    expect(keyed).not.toBe(resting);
    expect(keyed).toContain('2px');
    expect(keyed).toContain('4px');
    await page.setViewportSize({ width: 320, height: 800 });
    await page.getByRole('button', { name: 'Attach files' }).focus();
    await page.keyboard.press('Shift+Tab');
    await expect(field).toBeFocused();
    await expectAccessible(page, '.conversation-main');

    await page.setViewportSize({ width: 1200, height: 800 });
    await page.locator('.place-rail').getByRole('button', { name: /Garden/ }).first().click();
    await expect(page.getByRole('tab', { name: 'Garden', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('tab', { name: 'Quiet chat', exact: true }).click();
    const placed = page.locator('.conversation-main[data-empty]');
    await expect(placed).toBeVisible();
    // A place's empty start inherits the frame tint. It does not pin graphite over the place.
    await expect(placed).not.toHaveAttribute('data-tint');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await expectEmptyTint(page, '12');
    }
  });

  test(`CV-289 a notification click for this session's question scrolls and pages the tray in ${theme}`, async ({ page }) => {
    test.setTimeout(90_000);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await themeOf(page, theme);
    const notes = Array.from({ length: 40 }, (_, i) => `Paragraph ${i}: the conversation keeps going so the reader can leave the bottom.`).join('\n\n');
    await installMockEngine(page, {
      world: {
        rows: [{
          session: CHAT, title: 'Notes', sessionFile: SESSION, project: '', sourceFolders: [],
          state: 'waiting on you', live: true, open: true, running: true, needsYou: true, failed: 0,
          tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 },
        }],
        items: [
          { key: `${CHAT}:ask:7`, session: CHAT, kind: 'ask', id: 7, text: 'Pick a database', sourceFolders: [], answerable: true },
          { key: TARGET, session: CHAT, kind: 'consent', id: 2, text: 'Edit main.go', sourceFolders: [], answerable: true },
        ],
      },
      initial: {
        sessionFile: SESSION, title: 'Notes', needsPerson: true, running: true, questions: trayQuestions(),
        entries: [{ Role: 'user', Text: 'Read the long notes' }, { Role: 'assistant', Answer: true, Text: notes }],
      },
    });
    const workspace = {
      tabs: [{ id: 'notes', kind: 'conversation', title: 'Notes', draft: '', titleSource: 'manual', pinned: false, sessionFile: SESSION }],
      groups: [], closed: [], activeId: 'notes', nextNumber: 2, recentIds: ['notes'],
    };
    await page.addInitScript(saved => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(saved)), workspace);
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const card = tray(page);
    await expect(card.getByRole('status')).toHaveText('1 of 2');
    await expect(card.getByRole('region', { name: 'Pick a database' })).toBeVisible();
    await expect(card.getByRole('region', { name: 'Allow 3 actions?' })).toHaveCount(0);

    const before = await parkAboveEnd(page);
    // The world feed may still be connecting. Repeat the click until this session's question is the one on the card.
    await expect(async () => {
      await page.evaluate(itemId => window.dispatchEvent(new CustomEvent('codeaf:focus-attention', { detail: { itemId } })), TARGET);
      await expect(card.getByRole('status')).toHaveText('2 of 2', { timeout: 1000 });
    }).toPass({ timeout: 15_000 });
    await expect.poll(() => distance(page)).toBeLessThanOrEqual(SLACK);
    expect(await scroller(page).evaluate(el => el.scrollTop)).toBeGreaterThan(before);
    await expect(card.getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
    await expect(card.getByRole('region', { name: 'Pick a database' })).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'Notes', exact: true })).toHaveCount(1);

    // Resizing restores the reader's earlier place. The question the click opened stays on the card at every width.
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await expect(card.getByRole('status')).toHaveText('2 of 2');
      await expect(card.getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
      const box = await card.boundingBox();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    }

    await page.setViewportSize({ width: 320, height: 800 });
    await expectAccessible(page, '.decision-tray');
    // ← → page the tray the notification opened. Focus is placed on the arrow first: a disabled arrow does not take keys.
    await card.getByRole('button', { name: 'Previous question' }).focus();
    await page.keyboard.press('ArrowLeft');
    await expect(card.getByRole('status')).toHaveText('1 of 2');
    await expect(card.getByRole('region', { name: 'Pick a database' })).toBeVisible();
    await card.getByRole('button', { name: 'Next question' }).focus();
    await page.keyboard.press('ArrowRight');
    await expect(card.getByRole('status')).toHaveText('2 of 2');
    await expect(card.getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
  });
}
