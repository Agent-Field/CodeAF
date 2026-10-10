import { test, expect, type Page } from '@playwright/test';
import type { AttentionItem, WorldRow } from '../../src/features/chat/world-client';
import type { EngineSnapshot } from '../../src/features/chat/engine-client';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import { pendingQuestion, withTasks } from './support/scenarios';

// I2.4–I2.5. The window records a tab change, a place switch and a drill-in. A background open is not a step.
// ⌘[ and ⌘] put back the place, the tab, the scroll and the draft. The back chip shows after a Next up jump
// and folds on the next action. There is no permanent Back or Forward button.
const FOCUS_KEY = 'codeaf.desktop.focus.v1.main';
const SESSION = 'mock-session-1.jsonl';
const back = 'Meta+BracketLeft';
const forward = 'Meta+BracketRight';

type FocusSaved = {
  cursor: number;
  entries: { windowPlace: string; tabId: string; drillPath: string[]; scroll: { key: string; top: number }[]; draftKey: string | null }[];
};

const saved = (page: Page) => page.evaluate(key => JSON.parse(localStorage.getItem(key) || 'null') as FocusSaved, FOCUS_KEY);
const noPermanentButtons = async (page: Page) => {
  await expect(page.getByRole('button', { name: 'Back', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Forward', exact: true })).toHaveCount(0);
};

const paragraphs = Array.from({ length: 80 }, (_, i) => `Paragraph ${i}: the real conversation context.`).join('\n\n');

function visited(): Scenario {
  const base = withTasks();
  base.initial.entries = [
    { Role: 'user', Text: 'Read this conversation' },
    { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] },
    { Role: 'assistant', Text: paragraphs, Answer: true },
  ];
  base.initial.title = 'Config stack';
  return base;
}

/** Mac chords, one saved conversation, and a pinned place to switch to. */
async function boot(page: Page) {
  await page.addInitScript(() => { Object.defineProperty(navigator, 'platform', { value: 'MacIntel' }); });
  await installMockEngine(page, visited());
  const places = await installMockPlaces(page, { places: [{ name: 'Marketing', tint: 'rose', pinned: true }] });
  await page.addInitScript(file => {
    localStorage.setItem('codeaf-theme', 'light');
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [{ id: 'origin', kind: 'conversation', title: 'Config stack', draft: '', sessionFile: file, pinned: false, titleSource: 'manual' }],
      activeId: 'origin', groups: [], closed: [], nextNumber: 2, recentIds: ['origin'],
    }));
  }, SESSION);
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  return places;
}

test('records a tab change and a place switch, skips a background open, and ⌘[ / ⌘] restore tab, place, scroll and draft', async ({ page }) => {
  const places = await boot(page);
  await noPermanentButtons(page);
  await expect.poll(async () => (await saved(page)).entries.map(entry => entry.tabId)).toEqual(['origin']);

  const notice = page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ });
  await expect(notice).toBeVisible();
  await notice.click({ modifiers: ['Meta'] });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveCount(0);
  await expect.poll(async () => (await saved(page)).entries).toHaveLength(1);

  const scroller = page.locator('.conversation-scroll');
  await expect.poll(() => scroller.evaluate(el => el.scrollHeight)).toBeGreaterThan(1000);
  await scroller.evaluate(el => { el.scrollTop = 120; el.dispatchEvent(new Event('scroll')); });
  await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Keep my draft');
  await expect.poll(async () => {
    const origin = (await saved(page)).entries[0];
    return { top: origin.scroll.find(spot => spot.key === 'conversation')?.top, draft: origin.draftKey };
  }).toEqual({ top: 120, draft: 'origin' });

  await page.keyboard.press('Meta+KeyT');
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect.poll(async () => (await saved(page)).entries.length).toBe(2);
  const afterTab = await saved(page);
  expect(afterTab.entries).toHaveLength(2);
  expect(afterTab.entries[1].tabId).not.toBe('origin');
  expect(afterTab.entries[1].windowPlace).toBe('now');
  expect(afterTab.cursor).toBe(1);

  const marketing = places.id('Marketing');
  await page.locator('.app-shell .place-rail').first().locator('.rail-row').filter({ has: page.locator('.rail-place-name', { hasText: /^Marketing/ }) }).locator('.rail-place').click();
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
  await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Marketing');
  await expect.poll(async () => (await saved(page)).entries.map(entry => entry.windowPlace)).toEqual(['now', 'now', marketing]);
  const left = await saved(page);
  expect(left.entries[0].scroll.find(spot => spot.key === 'conversation')?.top).toBe(120);
  expect(left.entries[0].draftKey).toBe('origin');

  await page.keyboard.press(back);
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
  await page.keyboard.press(back);
  await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep my draft');
  await expect.poll(() => page.locator('.conversation-scroll').evaluate(el => el.scrollTop)).toBe(120);
  await expect.poll(async () => (await saved(page)).cursor).toBe(0);
  expect((await saved(page)).entries).toHaveLength(3);

  await page.keyboard.press(forward);
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press(forward);
  await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
  await expect(page.locator('.workspace-tab.is-place-home')).toContainText('Marketing');
  await noPermanentButtons(page);
});

test('a drill-in is a step, and the in-tab parent returns that one step', async ({ page }) => {
  await boot(page);
  await noPermanentButtons(page);
  await page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ }).click();
  const crumbs = page.getByRole('navigation', { name: 'Breadcrumb' });
  await expect(crumbs).toContainText('Config stack');
  await expect(crumbs).toContainText('Migrate the settings screen');
  await expect.poll(async () => {
    const history = await saved(page);
    return { steps: history.entries.length, drill: history.entries.at(-1)?.drillPath.join('/'), cursor: history.cursor };
  }).toEqual({ steps: 2, drill: '2', cursor: 1 });
  await noPermanentButtons(page);

  await crumbs.getByRole('button', { name: 'Config stack' }).click();
  await expect(crumbs).toHaveCount(0);
  await expect.poll(async () => (await saved(page)).cursor).toBe(0);
  expect((await saved(page)).entries).toHaveLength(2);

  await page.keyboard.press(forward);
  await expect(crumbs).toContainText('Migrate the settings screen');
  await expect.poll(async () => (await saved(page)).cursor).toBe(1);
  await page.keyboard.press(back);
  await expect(crumbs).toHaveCount(0);
  expect((await saved(page)).entries).toHaveLength(2);
});

test('the back chip appears after a Next up jump and folds on the next action', async ({ page }) => {
  await page.addInitScript(() => { Object.defineProperty(navigator, 'platform', { value: 'MacIntel' }); });
  const origin = '1111111111111111';
  const other = '2222222222222222';
  const marketing = 'pl_0000000000000001';
  const engine = await installMockEngine(page, pendingQuestion());
  const snapshots = new Map<string, EngineSnapshot>();
  for (const [chat, title, waiting] of [[origin, 'Config stack', false], [other, 'Brand copy', true]] as const) {
    snapshots.set(chat, {
      ...engine.snapshot(), id: chat, sessionFile: sessionFileFor(chat), title,
      questions: waiting ? engine.snapshot().questions : [], needsPerson: waiting,
      entries: [{ Role: 'user', Text: 'Read this conversation' }, { Role: 'assistant', Answer: true, Text: 'The other conversation.' }],
    });
  }
  await installMockPlaces(page, { places: [{ id: marketing, name: 'Marketing', tint: 'rose', pinned: true }] });
  const question = engine.snapshot().questions![0];
  const items: AttentionItem[] = [{ key: `${other}:choice:1`, session: other, kind: question.kind, id: question.id, text: question.head, title: 'Brand copy', sourceFolders: [], answerable: true, placeIds: [marketing], placeNames: ['Marketing'] }];
  const rows = [...snapshots].map(([chat, snapshot]) => ({ session: chat, title: snapshot.title, sessionFile: snapshot.sessionFile, project: '', sourceFolders: [], state: 'waiting on you', live: true, open: true, running: false, needsYou: chat !== origin, failed: 0, tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 } })) as WorldRow[];
  await page.route('**/api/engine/world', route => route.fulfill({ json: { seq: 1, rows, items } }));
  await page.route('**/api/engine/events?*', route => route.fulfill({ contentType: 'text/event-stream', body: `id: 1\ndata: ${JSON.stringify({ seq: 1, type: 'reset', payload: { rows, items } })}\n\n` }));
  await page.route('**/api/engine/sessions**', async route => {
    const path = new URL(route.request().url()).pathname.split('/').slice(3);
    const [, chat, action] = path;
    const body = route.request().method() === 'POST' ? route.request().postDataJSON() : {};
    if (!chat) {
      const found = [...snapshots.values()].find(snapshot => snapshot.sessionFile === body.sessionFile);
      await route.fulfill({ json: found });
      return;
    }
    const snapshot = snapshots.get(chat);
    if (!snapshot) { await route.fallback(); return; }
    if (!action) { await route.fulfill({ json: snapshot }); return; }
    if (action === 'events') { await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' }); return; }
    await route.fallback();
  });
  await page.addInitScript(file => {
    localStorage.setItem('codeaf-theme', 'light');
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [{ id: 'origin', kind: 'conversation', title: 'Config stack', draft: 'Keep my draft', sessionFile: file, pinned: false, titleSource: 'manual' }],
      activeId: 'origin', groups: [], closed: [], nextNumber: 2, recentIds: ['origin'],
    }));
  }, sessionFileFor(origin));
  await page.goto('/');
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep my draft');
  await noPermanentButtons(page);
  await expect(page.locator('.back-chip')).toHaveCount(0);

  await page.keyboard.press('Meta+KeyJ');
  const chip = page.locator('.back-chip');
  await expect(chip).toBeVisible();
  await expect(chip).toContainText('Back to Config stack');
  await expect(page.getByRole('tab', { name: 'Brand copy', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.getByText('The other conversation.').click();
  await expect(chip).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Brand copy', exact: true })).toHaveAttribute('aria-selected', 'true');
  await noPermanentButtons(page);
});
