import { test, expect, type Page } from '@playwright/test';
import type { AttentionItem, WorldRow } from '../../src/features/chat/world-client';
import type { EngineSnapshot } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import { pendingQuestion } from './support/scenarios';

const origin = '1111111111111111', first = '2222222222222222', second = '3333333333333333';
const marketing = 'pl_0000000000000001', software = 'pl_0000000000000002';
const key = (chat: string) => `${chat}:choice:1`;

async function open(page: Page, theme: string, banner = false) {
 const snapshots = new Map<string, EngineSnapshot>();
 const engine = await installMockEngine(page, pendingQuestion());
 for (const [chat, title] of [[origin, 'Config stack'], [first, 'Brand copy'], [second, 'Build parser']]) {
  snapshots.set(chat, { ...engine.snapshot(), id: chat, sessionFile: sessionFileFor(chat), title,
   questions: chat === origin ? [] : engine.snapshot().questions, needsPerson: chat !== origin,
   entries: [{ Role: 'user', Text: 'Read this conversation' }, { Role: 'assistant', Answer: true, Text: Array.from({ length: 80 }, (_, i) => `Paragraph ${i}: the real conversation context.`).join('\n\n') }] });
 }
 await installMockPlaces(page, { places: [{ id: marketing, name: 'Marketing', tint: 'rose', pinned: true }, { id: software, name: 'Software', tint: 'tide', pinned: true }] });
 const q = engine.snapshot().questions![0];
 let items: AttentionItem[] = [first, second].map((chat, i) => ({ key: key(chat), session: chat, kind: q.kind, id: q.id, text: q.head, title: snapshots.get(chat)!.title, sourceFolders: [], answerable: true, placeIds: [i ? software : marketing], placeNames: [i ? 'Software' : 'Marketing'] }));
 const arrival = items[1];
 if (banner) items = [items[0]];
 const rows = [...snapshots].map(([chat, snapshot]) => ({ session: chat, title: snapshot.title, sessionFile: snapshot.sessionFile, project: '', sourceFolders: [], state: 'waiting on you', live: true, open: true, running: false, needsYou: chat !== origin, failed: 0, tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 } })) as WorldRow[];
 let seq = 1;
 const answers: unknown[] = [];
 let refusing = false;
 await page.route('**/api/engine/world', route => route.fulfill({ json: { seq, rows, items } }));
 await page.route('**/api/engine/events?*', async route => {
  const after = Number(new URL(route.request().url()).searchParams.get('after'));
  if (after >= seq) await new Promise(resolve => setTimeout(resolve, 100));
  await route.fulfill({ contentType: 'text/event-stream', body: after >= seq ? ': connected\n\n' : `id: ${seq}\ndata: ${JSON.stringify({ seq, type: 'reset', payload: { rows, items } })}\n\n` });
 });
 await page.route('**/api/engine/sessions**', async route => {
  const path = new URL(route.request().url()).pathname.split('/').slice(3);
  const [, chat, action] = path;
  const body = route.request().method() === 'POST' ? route.request().postDataJSON() : {};
  if (!chat) { const saved = [...snapshots.values()].find(snapshot => snapshot.sessionFile === body.sessionFile); await route.fulfill({ json: saved }); return; }
  const snapshot = snapshots.get(chat);
  if (!snapshot) { await route.fallback(); return; }
  if (!action) { await route.fulfill({ json: snapshot }); return; }
  if (action === 'events') { await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' }); return; }
  if (action === 'answer') {
   if (refusing) { await route.fulfill({ status: 409, json: { error: 'Answer refused' } }); return; }
   answers.push(body);
   snapshot.questions = [];
   snapshot.needsPerson = false;
   items = items.filter(item => item.session !== chat); seq++;
   await route.fulfill({ json: { accepted: true } }); return;
  }
  await route.fallback();
 });
 await page.addInitScript(({ theme, file }) => {
  localStorage.setItem('codeaf-theme', theme);
  localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'origin', kind: 'conversation', title: 'Config stack', draft: 'Keep my draft', sessionFile: file, pinned: false, titleSource: 'manual' }], activeId: 'origin', groups: [], closed: [], nextNumber: 2, recentIds: ['origin'] }));
 }, { theme, file: sessionFileFor(origin) });
 await page.goto('/');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
 const scroll = page.locator('.conversation-scroll');
 await expect.poll(() => scroll.evaluate(el => el.scrollHeight)).toBeGreaterThan(1000);
 await scroll.evaluate(el => { el.scrollTop = 120; el.dispatchEvent(new Event('scroll')); });
 return { answers, refuse: () => { refusing = true; }, arrive: () => { items = [...items, arrival]; seq++; }, drop: (chat: string) => { items = items.filter(item => item.session !== chat); seq++; } };
}

for (const theme of ['light', 'dark']) test(`walk answers across places, matches design controls and returns scroll/draft in ${theme}`, async ({ page }) => {
 const fixture = await open(page, theme);
 await page.getByRole('button', { name: /2 need you elsewhere/ }).click();
 await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
 await expect(page.getByRole('button', { name: 'Marketing', exact: true })).toHaveAttribute('aria-current', 'page');
 await expect(page.getByRole('tab', { name: 'Brand copy', exact: true })).toHaveAttribute('aria-selected', 'true');
 const tray = page.getByRole('region', { name: 'Waiting on you' });
 await expect(tray).toBeVisible();
 await expect(page.locator('.next-up-chip')).toHaveText('Next up1 of 2');
 const skip = tray.getByRole('button', { name: 'Skip', exact: true });
 const measured = await skip.evaluate(el => ({ height: el.getBoundingClientRect().height, radius: getComputedStyle(el).borderRadius, padding: getComputedStyle(el).padding, size: getComputedStyle(el).fontSize }));
 await expect.poll(() => skip.evaluate(el => el.getBoundingClientRect().height)).toBeCloseTo(30, 2);
 expect(measured).toMatchObject({ radius: '8px', padding: '0px 10px', size: '12px' });
 expect(await page.locator('.app-shell').evaluate(el => getComputedStyle(el).transitionDuration.split(',')[0].trim())).toBe('0.32s');
 await skip.click();
 await expect(page.locator('body')).toHaveAttribute('data-tint', 'tide');
 await expect(page.getByRole('button', { name: 'Software', exact: true })).toHaveAttribute('aria-current', 'page');
 await expect(page.getByRole('tab', { name: 'Build parser', exact: true })).toHaveAttribute('aria-selected', 'true');
 await tray.getByRole('radio', { name: /SQLite/ }).focus();
 await page.keyboard.press('Space');
 await expect(tray.getByRole('radio', { name: /SQLite/ })).toBeChecked();
 await tray.getByRole('button', { name: 'Choose', exact: true }).click();
 await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
 await expect(page.getByRole('button', { name: 'Marketing', exact: true })).toHaveAttribute('aria-current', 'page');
 await tray.getByRole('radio', { name: /SQLite/ }).focus();
 await page.keyboard.press('Space');
 await expect(tray.getByRole('radio', { name: /SQLite/ })).toBeChecked();
 await tray.getByRole('button', { name: 'Choose', exact: true }).click();
 await expect(page.getByRole('region', { name: "You're clear" })).toBeVisible();
 await expect(page.getByRole('region', { name: "You're clear" }).getByRole('status')).toContainText('2 answered.');
 expect(fixture.answers).toHaveLength(2);
 await page.getByRole('button', { name: 'Back to Config stack' }).last().click();
 await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
 await expect.poll(() => page.locator('.conversation-scroll').evaluate(el => el.scrollTop)).toBe(120);
});

for (const entry of ['shortcut', 'popover', 'notification', 'banner']) test(`${entry} targets its item; disappearance advances; Escape restores origin`, async ({ page }) => {
 const fixture = await open(page, 'light', entry === 'banner');
 if (entry === 'shortcut') await page.keyboard.press('Control+j');
 if (entry === 'popover') {
  await page.getByRole('button', { name: /2 need you elsewhere/ }).hover();
  await page.locator('.queue-popover-row').last().click();
 } else if (entry === 'notification') await page.evaluate(itemId => window.dispatchEvent(new CustomEvent('codeaf:focus-attention', { detail: { itemId } })), key(second));
 else if (entry === 'banner') { fixture.arrive(); await page.locator('.nextup-banner').click(); }
 const chat = entry === 'shortcut' ? first : second;
 await expect(page.locator('.next-up-chip')).toBeVisible();
 fixture.drop(chat);
 await expect(page.getByRole('tab', { name: entry === 'shortcut' ? 'Build parser' : 'Brand copy', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.getByRole('region', { name: "You're clear" })).toHaveCount(0);
 await page.keyboard.press(entry === 'shortcut' ? 'Alt+ArrowLeft' : 'Escape');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
 expect(fixture.answers).toHaveLength(0);
});

for (const theme of ['light', 'dark']) test(`walk controls remain reachable at every width with reduced motion in ${theme}`, async ({ page }) => {
 await page.emulateMedia({ reducedMotion: 'reduce' });
 await open(page, theme);
 await page.keyboard.press('Control+j');
 const skip = page.getByRole('button', { name: 'Skip', exact: true });
 await expect(skip).toBeVisible();
 for (const width of [320, 480, 600, 800, 1200]) {
  await page.setViewportSize({ width, height: 560 });
  await expect(skip).toBeVisible();
  const bounds = await skip.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
  const overflow = await page.evaluate(() => [...document.querySelectorAll('main, .workspace-tabbar, .workspace-tabstrip, .workspace-back-slot, .workspace-frame-slot, .conversation-bar, .conversation-bar-counts')].map(el => ({ name: el.className, right: el.getBoundingClientRect().right, width: el.getBoundingClientRect().width })));
  expect(await page.evaluate(() => document.documentElement.scrollWidth), JSON.stringify(overflow)).toBe(width);
  expect(await page.locator('.app-shell').evaluate(el => getComputedStyle(el).transitionDuration.split(',')[0].trim())).toBe('0s');
 }
 await page.keyboard.press('Escape');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
});

test('a refused answer keeps the item and does not claim completion', async ({ page }) => {
 const fixture = await open(page, 'light');
 fixture.refuse();
 await page.keyboard.press('Control+j');
 const tray = page.getByRole('region', { name: 'Waiting on you' });
 await tray.getByRole('radio', { name: /SQLite/ }).focus();
 await page.keyboard.press('Space');
 await tray.getByRole('button', { name: 'Choose', exact: true }).click();
 await expect(tray.getByRole('status')).toHaveText('That did not go through. Try again.');
 await expect(page.locator('.next-up-chip')).toHaveText('Next up1 of 2');
 await expect(page.getByRole('region', { name: "You're clear" })).toHaveCount(0);
 expect(fixture.answers).toHaveLength(0);
});
