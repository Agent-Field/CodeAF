import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { manyConversations, withHistory } from './support/scenarios-history';

// Scroll restoration per pane (design Interactions, Relaunch: "scroll positions restore"). Only the active tab is mounted,
// so every return remounts the pane; each scroller inside it must come back where the reader left it, per pane, and a
// conversation nobody has scrolled yet must still open at its end. The engine is a mock; the controls are the real ones.
const STORE = 'codeaf.desktop.tabScroll.v1';
const WORKSPACE = 'codeaf.desktop.workspace.v1';

const lines = (name: string, count: number) => Array.from({ length: count }, (_, i) => `${name} line ${i + 1}`).join('\n');
// One long answer, as conversation-scroll.spec does: the transcript windows older turns, so length has to come from the last reply.
const entries = (name: string) => [
  { Role: 'user' as const, Text: `${name} question` },
  { Role: 'assistant' as const, Text: Array.from({ length: 90 }, (_, i) => `${name} paragraph ${i + 1} of a long reply that fills the reading column.`).join('\n\n'), Answer: true },
];
const b64 = (text: string) => Buffer.from(text).toString('base64');

const pane = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', kind: 'conversation', titleSource: 'manual', ...over });
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ ...pane(id, title, over), pinned: false });
const chat = (id: string, title: string, file: string) => tab(id, title, { sessionFile: file });
const fileTab = (id: string, title: string, path: string) => tab(id, title, { kind: 'file', sessionFile: 'chat-a.jsonl', file: { path, view: 'file' } });

async function seed(page: Page, tabs: unknown[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(t => (t as { id: string }).id) };
  await page.addInitScript(([key, value]) => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } }, [WORKSPACE, JSON.stringify(state)]);
}

type Seen = { method: string; path: string };

/** The mock engine, with a conversation per saved session file so two chats really hold different words. */
async function open(page: Page, tabs: unknown[], activeId: string, scenario = withHistory([])): Promise<{ engine: MockEngine; seen: Seen[] }> {
  const engine = await installMockEngine(page, { ...scenario, files: { 'a.txt': { mime: 'text/plain', dataBase64: b64(lines('Alpha', 400)) }, 'b.txt': { mime: 'text/plain', dataBase64: b64(lines('Beta', 400)) } } });
  const seen: Seen[] = [];
  const own = (file: string) => ({ ...engine.snapshot(), id: `s-${file}`, sessionFile: file, entries: entries(file.startsWith('chat-a') ? 'Alpha' : file.startsWith('chat-b') ? 'Beta' : 'Gamma'), title: file });
  await page.route('**/api/engine/sessions**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    const [, id, action] = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean);
    seen.push({ method: request.method(), path: url.pathname });
    const body = request.postData() ? JSON.parse(request.postData()!) as { sessionFile?: string } : {};
    const json = (value: unknown) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(value) });
    if (!id && body.sessionFile?.endsWith('.jsonl')) return json(own(body.sessionFile));
    if (id?.startsWith('s-') && !action) return json(own(id.slice(2)));
    if (id?.startsWith('s-') && action === 'events') return; // an idle stream: open and quiet
    // File reads go through the session id; the mock holds one session, so they are asked of that one.
    if (id?.startsWith('s-')) return route.fallback({ url: request.url().replace(id, engine.snapshot().id) });
    return route.fallback();
  });
  await seed(page, tabs, activeId);
  await page.goto('/');
  return { engine, seen };
}

const tabButton = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const select = async (page: Page, name: string) => { await tabButton(page, name).click(); await expect(tabButton(page, name)).toHaveAttribute('aria-selected', 'true'); };
const top = (scroller: Locator) => scroller.evaluate(el => el.scrollTop);
const max = (scroller: Locator) => scroller.evaluate(el => el.scrollHeight - el.clientHeight);

/** Scrolls with the wheel over the scroller, the way a reader does, and waits for the offset to stop moving. */
async function wheel(page: Page, scroller: Locator, dy: number): Promise<number> {
  const box = (await scroller.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, dy);
  let last = -1;
  await expect.poll(async () => { const now = await top(scroller); const still = now === last; last = now; return still; }, { timeout: 5000 }).toBe(true);
  return top(scroller);
}
const near = (actual: number, expected: number, slack = 3) => expect(Math.abs(actual - expected)).toBeLessThanOrEqual(slack);
const chatScroller = (page: Page, at = 0) => page.locator('.workspace-pane').nth(at).locator('.conversation-scroll');

test('two chats keep their own place; a chat nobody scrolled opens at its end', async ({ page }) => {
  const { engine, seen } = await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  const a = chatScroller(page);
  await expect(page.getByText('Alpha paragraph 90 of a long reply', { exact: false })).toBeVisible();
  // First visit: at the end.
  near(await top(a), await max(a), 2);
  const aTop = await wheel(page, a, -1400);
  expect(aTop).toBeGreaterThan(0);

  await select(page, 'Beta chat');
  await expect(page.getByText('Beta paragraph 90 of a long reply', { exact: false })).toBeVisible();
  const b = chatScroller(page);
  // Never visited: sticks to its end, whatever Alpha did.
  near(await top(b), await max(b), 2);
  const bTop = await wheel(page, b, -600);
  expect(Math.abs(bTop - aTop)).toBeGreaterThan(100);

  const before = seen.length;
  await select(page, 'Alpha chat');
  await expect(page.getByText('Alpha question', { exact: true })).toBeAttached();
  await expect.poll(async () => Math.abs((await top(chatScroller(page))) - aTop)).toBeLessThanOrEqual(3);
  await select(page, 'Beta chat');
  await expect.poll(async () => Math.abs((await top(chatScroller(page))) - bTop)).toBeLessThanOrEqual(3);
  await select(page, 'Alpha chat');
  await expect.poll(async () => Math.abs((await top(chatScroller(page))) - aTop)).toBeLessThanOrEqual(3);

  // Returning never starts work: no turn, no stop. A remount re-attaches its session (twice in dev, where StrictMode runs effects twice); that is the only traffic of three switches.
  expect(engine.calls.filter(c => /\/(turn|stop|answer)$/.test(c.path))).toHaveLength(0);
  expect(seen.slice(before).filter(c => c.method === 'POST' && c.path.endsWith('/sessions')).length).toBeLessThanOrEqual(3 * 2);
  expect(engine.calls.filter(c => c.path.endsWith('/terminals') && c.method === 'POST')).toHaveLength(0);
});

test('two file tabs keep their own place', async ({ page }) => {
  await open(page, [fileTab('fa', 'a.txt', 'a.txt'), fileTab('fb', 'b.txt', 'b.txt')], 'fa');
  await expect(page.getByText('Alpha line 1', { exact: true })).toBeVisible();
  const body = page.locator('.file-body');
  const aTop = await wheel(page, body, 900);
  expect(aTop).toBeGreaterThan(300);
  await select(page, 'b.txt');
  await expect(page.getByText('Beta line 1', { exact: true })).toBeVisible();
  expect(await top(page.locator('.file-body'))).toBe(0);
  const bTop = await wheel(page, page.locator('.file-body'), 2400);
  await select(page, 'a.txt');
  await expect(page.getByText('Alpha line', { exact: false }).first()).toBeVisible();
  await expect.poll(async () => Math.abs((await top(page.locator('.file-body'))) - aTop)).toBeLessThanOrEqual(3);
  await select(page, 'b.txt');
  await expect.poll(async () => Math.abs((await top(page.locator('.file-body'))) - bTop)).toBeLessThanOrEqual(3);
});

test('split panes each keep their place across tab switches and maximize and restore', async ({ page }) => {
  const split = { ...tab('sp', 'Split'), split: { layout: '1x2', focus: 0, panes: [pane('p1', 'Alpha chat', { sessionFile: 'chat-a.jsonl' }), pane('p2', 'Beta chat', { sessionFile: 'chat-b.jsonl' })] } };
  await open(page, [split, chat('c', 'Gamma chat', 'chat-c.jsonl')], 'sp');
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  await expect(page.getByText('Beta paragraph 90 of a long reply', { exact: false })).toBeVisible();
  const left = await wheel(page, chatScroller(page, 0), -1500);
  const right = await wheel(page, chatScroller(page, 1), -500);
  expect(Math.abs(left - right)).toBeGreaterThan(100);

  await select(page, 'Gamma chat');
  await expect(page.getByText('Gamma paragraph 90 of a long reply', { exact: false })).toBeVisible();
  await page.getByRole('tab', { name: /Alpha chat/ }).first().click();
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  await expect.poll(async () => Math.abs((await top(chatScroller(page, 0))) - left)).toBeLessThanOrEqual(3);
  await expect.poll(async () => Math.abs((await top(chatScroller(page, 1))) - right)).toBeLessThanOrEqual(3);

  // Maximize the right pane, then restore: both keep their offsets.
  await page.getByRole('button', { name: 'Pane menu Beta chat' }).click({ force: true });
  await page.getByRole('menuitem', { name: 'Maximize' }).click();
  await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(1);
  await expect.poll(async () => Math.abs((await top(chatScroller(page, 1))) - right)).toBeLessThanOrEqual(3);
  await page.getByRole('button', { name: 'Pane menu Beta chat' }).click({ force: true });
  await page.getByRole('menuitem', { name: 'Restore panes' }).click();
  await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(0);
  await expect.poll(async () => Math.abs((await top(chatScroller(page, 0))) - left)).toBeLessThanOrEqual(3);
  await expect.poll(async () => Math.abs((await top(chatScroller(page, 1))) - right)).toBeLessThanOrEqual(3);
});

test('History keeps its list place', async ({ page }) => {
  await page.clock.setFixedTime(new Date(2026, 9, 9, 15, 0));
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl')], 'a', withHistory(manyConversations(300)));
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  const list = page.locator('.history-list');
  await expect(list).toBeVisible();
  const at = await wheel(page, list, 1800);
  expect(at).toBeGreaterThan(500);
  await select(page, 'Alpha chat');
  await expect(page.getByText('Alpha paragraph 90 of a long reply', { exact: false })).toBeVisible();
  await page.getByRole('tab', { name: /History/ }).click();
  await expect(list).toBeVisible();
  await expect.poll(async () => Math.abs((await top(page.locator('.history-list'))) - at)).toBeLessThanOrEqual(3);
});

test('the reader wins over a restore that is still waiting for content', async ({ page }) => {
  let slow = false;
  await page.route('**/api/engine/sessions/s-chat-a.jsonl', async route => { if (slow) await new Promise(r => setTimeout(r, 1200)); await route.fallback(); });
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  await expect(page.getByText('Alpha paragraph 90 of a long reply', { exact: false })).toBeVisible();
  const saved = await wheel(page, chatScroller(page), -1400);
  await select(page, 'Beta chat');
  await expect(page.getByText('Beta paragraph 90 of a long reply', { exact: false })).toBeVisible();
  slow = true;
  await select(page, 'Alpha chat');
  const scroller = chatScroller(page);
  await expect(page.getByText('Alpha question', { exact: true })).toBeAttached();
  const after = await wheel(page, scroller, 300);
  await page.waitForTimeout(800);
  // Wherever the restore was when the wheel turned, the reader's 300px is the last word.
  near(await top(scroller), after, 2);
  expect(after).toBeGreaterThanOrEqual(saved);
});

test('a reload puts each tab back, and the memory holds only tabs that exist', async ({ page }) => {
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  await expect(page.getByText('Alpha paragraph 90 of a long reply', { exact: false })).toBeVisible();
  const aTop = await wheel(page, chatScroller(page), -1300);
  await select(page, 'Beta chat');
  await expect(page.getByText('Beta paragraph 90 of a long reply', { exact: false })).toBeVisible();
  await wheel(page, chatScroller(page), -400);
  await expect.poll(() => page.evaluate(key => Object.keys(JSON.parse(localStorage.getItem(key) ?? '{}')).sort().join(), STORE)).toBe('a,b');
  await select(page, 'Alpha chat');
  await page.reload();
  await expect(page.getByText('Alpha question', { exact: true })).toBeAttached();
  await expect.poll(async () => Math.abs((await top(chatScroller(page))) - aTop), { timeout: 8000 }).toBeLessThanOrEqual(3);

  // Closing a tab forgets its place.
  await tabButton(page, 'Beta chat').hover();
  await page.getByRole('button', { name: /^Close Beta chat/ }).click();
  await expect(tabButton(page, 'Beta chat')).toHaveCount(0);
  await expect.poll(() => page.evaluate(key => Object.keys(JSON.parse(localStorage.getItem(key) ?? '{}')).join(), STORE)).toBe('a');
});
