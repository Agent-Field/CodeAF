import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine, type Scenario } from './support/mock-engine';
import { manyConversations, withHistory } from './support/scenarios-history';
import { designConversations } from './support/scenarios-history';

// Scroll restoration per pane (design Interactions, Relaunch: "scroll positions restore"). Only the active tab is mounted, so every
// return remounts the pane; each scroller inside it must come back where the reader left it, per pane, and a conversation nobody has
// scrolled yet must still open at its end. The engine is a mock; the controls are the real ones (wheel, click, menu).
const STORE_PREFIX = 'codeaf.desktop.tabScroll.v2.';
const WORKSPACE = 'codeaf.desktop.workspace.v1';
const SESSION = 'mock-session-1.jsonl';

const lines = (name: string, count: number) => Array.from({ length: count }, (_, i) => `${name} line ${i + 1}`).join('\n');
const wide = (name: string, count: number) => Array.from({ length: count }, (_, i) => `${name} line ${i + 1} ${'· wide text that runs far past the right edge '.repeat(14)}end of ${name} ${i + 1}`).join('\n');
const paragraphs = (name: string, count: number) => Array.from({ length: count }, (_, i) => `${name} paragraph ${i + 1} of a long reply that fills the reading column.`).join('\n\n');
// One long answer: the transcript windows older turns, so length has to come from the last reply.
const entries = (name: string) => [{ Role: 'user' as const, Text: `${name} question` }, { Role: 'assistant' as const, Text: paragraphs(name, 90), Answer: true }];
const b64 = (text: string) => Buffer.from(text).toString('base64');

const pane = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', kind: 'conversation', titleSource: 'manual', ...over });
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ ...pane(id, title, over), pinned: false });
const chat = (id: string, title: string, file: string) => tab(id, title, { sessionFile: file });
const fileTab = (id: string, title: string, path: string, kind: 'file' | 'diff' = 'file') => tab(id, title, { kind, sessionFile: 'chat-a.jsonl', file: { path, view: kind === 'diff' ? 'changes' : 'file' } });

async function seed(page: Page, tabs: unknown[], activeId: string, extra: Record<string, string> = {}) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(t => (t as { id: string }).id) };
  await page.addInitScript(([key, value, more]) => {
    if (sessionStorage.getItem('seeded')) return;
    localStorage.setItem(key, value);
    for (const [name, text] of Object.entries(more)) localStorage.setItem(name, text);
    sessionStorage.setItem('seeded', '1');
  }, [WORKSPACE, JSON.stringify(state), extra] as const);
}

type Seen = { method: string; path: string };
type Options = { scenario?: Scenario; extra?: Record<string, string>; viewport?: { width: number; height: number }; url?: string };

const taskRow = { ID: '1.1', Title: 'Read the current screen', Status: 'done', Waits: [], Seat: 'worker', Steps: 0, Started: '2026-10-09T10:00:00Z', Ended: '2026-10-09T10:00:00Z' };
const taskPage = { Row: taskRow, Description: 'Read the screen and list its form fields.', Result: '', Checks: [], Steps: [], Notes: Array.from({ length: 80 }, (_, i) => ({ Author: '1.1', Body: `Brief note ${i + 1}: what was found on the screen and why it matters for the port.`, At: '2026-10-09T10:00:00Z' })), Children: [], Folder: '/mock-workspace' };

/** The mock engine, with a conversation per saved session file so two chats really hold different words. */
async function open(page: Page, tabs: unknown[], activeId: string, options: Options = {}): Promise<{ engine: MockEngine; seen: Seen[] }> {
  if (options.viewport) await page.setViewportSize(options.viewport);
  const base = options.scenario ?? withHistory([]);
  const engine = await installMockEngine(page, {
    ...base,
    taskPages: { '1.1': taskPage as never, ...base.taskPages },
    files: {
      'a.txt': { mime: 'text/plain', dataBase64: b64(lines('Alpha', 400)) }, 'b.txt': { mime: 'text/plain', dataBase64: b64(lines('Beta', 400)) },
      'wide.txt': { mime: 'text/plain', dataBase64: b64(wide('Wide', 120)) }, ...base.files,
    },
    diffs: { 'wide.go': { lines: 90, hunks: [{ header: '@@ -1,90 +1,90 @@', oldStart: 1, oldLines: 90, newStart: 1, newLines: 90, section: '', lines: Array.from({ length: 90 }, (_, i) => ({ kind: 'context' as const, old: i + 1, new: i + 1, text: `Diff line ${i + 1} ${'· wide diff text that runs far past the edge '.repeat(14)}end ${i + 1}` })) }] }, ...base.diffs },
  });
  const seen: Seen[] = [];
  const own = (file: string) => ({ ...engine.snapshot(), id: `s-${file}`, sessionFile: file, entries: entries(file.startsWith('chat-a') ? 'Alpha' : file.startsWith('chat-b') ? 'Beta' : 'Gamma'), tasks: [taskRow], title: file });
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
    // File, task and terminal reads go through the session id; the mock holds one session, so they are asked of that one.
    if (id?.startsWith('s-')) return route.fallback({ url: request.url().replace(id, engine.snapshot().id) });
    return route.fallback();
  });
  await seed(page, tabs, activeId, options.extra);
  await page.goto(options.url ?? '/');
  return { engine, seen };
}

const tabButton = (page: Page, name: string | RegExp) => page.getByRole('tab', { name, exact: typeof name === 'string' });
const select = async (page: Page, name: string | RegExp) => { await tabButton(page, name).first().click(); await expect(tabButton(page, name).first()).toHaveAttribute('aria-selected', 'true'); };
type Place = { top: number; left: number };
const place = (scroller: Locator): Promise<Place> => scroller.evaluate(el => ({ top: el.scrollTop, left: el.scrollLeft }));
const top = async (scroller: Locator) => (await place(scroller)).top;
const max = (scroller: Locator) => scroller.evaluate(el => el.scrollHeight - el.clientHeight);

/** Scrolls with the wheel over the scroller, the way a reader does, and waits for the offsets to stop moving. */
async function wheel(page: Page, scroller: Locator, dy: number, dx = 0): Promise<Place> {
  const box = (await scroller.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(dx, dy);
  let last = '';
  await expect.poll(async () => { const now = JSON.stringify(await place(scroller)); const still = now === last; last = now; return still; }, { timeout: 5000 }).toBe(true);
  return place(scroller);
}
const near = (actual: number, expected: number, slack = 3) => expect(Math.abs(actual - expected)).toBeLessThanOrEqual(slack);
/** Waits for the scroller (re-queried each time, since the pane remounts) to be back at the place. */
async function expectPlace(scroller: () => Locator, expected: Place, timeout = 6000) {
  let now: Place = { top: -1, left: -1 };
  try {
    await expect.poll(async () => { now = await place(scroller()); return Math.abs(now.top - expected.top) <= 3 && Math.abs(now.left - expected.left) <= 3; }, { timeout }).toBe(true);
  } catch (error) {
    throw new Error(`expected ${JSON.stringify(expected)}, last saw ${JSON.stringify(now)}`, { cause: error });
  }
}
const chatScroller = (page: Page, at = 0) => page.locator('.workspace-pane').nth(at).locator('.conversation-scroll');
const ready = (page: Page, name: string) => expect(page.getByText(`${name} paragraph 90 of a long reply`, { exact: false })).toBeVisible();

test('two chats keep their own place; a chat nobody scrolled opens at its end', async ({ page }) => {
  const { engine, seen } = await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  const a = chatScroller(page);
  await ready(page, 'Alpha');
  near(await top(a), await max(a), 2);
  const aTop = await wheel(page, a, -1400);
  expect(aTop.top).toBeGreaterThan(0);

  await select(page, 'Beta chat');
  await ready(page, 'Beta');
  const b = chatScroller(page);
  // Never visited: sticks to its end, whatever Alpha did.
  near(await top(b), await max(b), 2);
  const bTop = await wheel(page, b, -600);
  expect(Math.abs(bTop.top - aTop.top)).toBeGreaterThan(100);

  const before = seen.length;
  await select(page, 'Alpha chat');
  await expectPlace(() => chatScroller(page), aTop);
  await select(page, 'Beta chat');
  await expectPlace(() => chatScroller(page), bTop);
  await select(page, 'Alpha chat');
  await expectPlace(() => chatScroller(page), aTop);

  // Returning never starts work: no turn, no stop. A remount re-attaches its session (twice in dev, where StrictMode runs effects twice); that is the only traffic of three switches.
  expect(engine.calls.filter(c => /\/(turn|stop|answer)$/.test(c.path))).toHaveLength(0);
  expect(seen.slice(before).filter(c => c.method === 'POST' && c.path.endsWith('/sessions')).length).toBeLessThanOrEqual(3 * 2);
  expect(engine.calls.filter(c => c.path.endsWith('/terminals') && c.method === 'POST')).toHaveLength(0);
});

test('a task page keeps its place beside the chat it came from', async ({ page }) => {
  await open(page, [tab('t', 'Read the current screen', { kind: 'task', sessionFile: 'chat-a.jsonl', route: { taskId: '1.1', back: [''], forward: [] } }), chat('b', 'Beta chat', 'chat-b.jsonl')], 't');
  const scroller = () => page.locator('.conversation-scroll[data-scroll-key="task-page"]');
  await expect(page.getByText('Brief note 80:', { exact: false })).toBeAttached();
  await expect(scroller()).toBeVisible();
  const at = await wheel(page, scroller(), 900);
  expect(at.top).toBeGreaterThan(300);
  await select(page, 'Beta chat');
  await ready(page, 'Beta');
  await select(page, 'Read the current screen');
  await expectPlace(scroller, at);
});

test('two file tabs keep their own place', async ({ page }) => {
  await open(page, [fileTab('fa', 'a.txt', 'a.txt'), fileTab('fb', 'b.txt', 'b.txt')], 'fa');
  await expect(page.getByText('Alpha line 1', { exact: true })).toBeVisible();
  const body = () => page.locator('.file-body');
  const aTop = await wheel(page, body(), 900);
  expect(aTop.top).toBeGreaterThan(300);
  await select(page, 'b.txt');
  await expect(page.getByText('Beta line 1', { exact: true })).toBeVisible();
  expect(await top(body())).toBe(0);
  const bTop = await wheel(page, body(), 2400);
  await select(page, 'a.txt');
  await expectPlace(body, aTop);
  await select(page, 'b.txt');
  await expectPlace(body, bTop);
});

test('a wide file and a wide diff keep their horizontal place as well as their vertical one', async ({ page }) => {
  await open(page, [fileTab('fw', 'wide.txt', 'wide.txt'), fileTab('dw', 'wide.go', 'wide.go', 'diff'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'fw');
  const body = () => page.locator('.file-body');
  await expect(page.getByText('Wide line 1 ', { exact: false }).first()).toBeVisible();
  const file = await wheel(page, body(), 700, 900);
  expect(file.left).toBeGreaterThan(300);
  expect(file.top).toBeGreaterThan(200);
  await select(page, 'wide.go');
  await expect(page.getByText('Diff line 1 ', { exact: false }).first()).toBeVisible();
  const diff = await wheel(page, body(), 500, 1300);
  expect(diff.left).toBeGreaterThan(300);
  await select(page, 'Beta chat');
  await ready(page, 'Beta');
  await select(page, 'wide.txt');
  await expectPlace(body, file);
  await select(page, 'wide.go');
  await expectPlace(body, diff);
});

test('split panes each keep their place across tab switches and maximize and restore', async ({ page }) => {
  const split = { ...tab('sp', 'Split'), split: { layout: '1x2', focus: 0, panes: [pane('p1', 'Alpha chat', { sessionFile: 'chat-a.jsonl' }), pane('p2', 'Beta chat', { sessionFile: 'chat-b.jsonl' })] } };
  await open(page, [split, chat('c', 'Gamma chat', 'chat-c.jsonl')], 'sp');
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  await ready(page, 'Beta');
  const left = await wheel(page, chatScroller(page, 0), -1500);
  const right = await wheel(page, chatScroller(page, 1), -500);
  expect(Math.abs(left.top - right.top)).toBeGreaterThan(100);

  await select(page, 'Gamma chat');
  await ready(page, 'Gamma');
  await page.getByRole('tab', { name: /Alpha chat/ }).first().click();
  await expect(page.locator('.workspace-pane')).toHaveCount(2);
  await expectPlace(() => chatScroller(page, 0), left);
  await expectPlace(() => chatScroller(page, 1), right);

  // Maximize the right pane, then restore: both keep their offsets.
  await page.getByRole('button', { name: 'Pane menu Beta chat' }).click({ force: true });
  await page.getByRole('menuitem', { name: 'Maximize' }).click();
  await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(1);
  // The maximized pane is wider, so its text reflows and the browser clamps its offset; the places held for the restore are what matter.
  await page.waitForTimeout(400);
  await page.getByRole('button', { name: 'Pane menu Beta chat' }).click({ force: true });
  await page.getByRole('menuitem', { name: 'Restore panes' }).click();
  await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(0);
  await expectPlace(() => chatScroller(page, 0), left);
  await expectPlace(() => chatScroller(page, 1), right);
});

const longTalk = { id: 'long', title: 'A very long discussion', at: new Date(2026, 9, 9, 12, 0).toISOString(), messages: Array.from({ length: 70 }, (_, i) => ({ role: i % 2 ? 'assistant' as const : 'user' as const, text: `Message ${i + 1} in the long discussion about topic ${i + 1}. ${'More words to make this row tall. '.repeat(4)}` })) };
const historyScenario = () => withHistory([longTalk, ...designConversations(), ...manyConversations(300)]);

test('History keeps its list place when its tab returns', async ({ page }) => {
  await page.clock.setFixedTime(new Date(2026, 9, 9, 15, 0));
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl')], 'a', { scenario: historyScenario() });
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  const list = () => page.locator('.history-list');
  await expect(list()).toBeVisible();
  const listAt = await wheel(page, list(), 1800);
  expect(listAt.top).toBeGreaterThan(500);
  await select(page, 'Alpha chat');
  await ready(page, 'Alpha');
  await page.getByRole('tab', { name: /History/ }).click();
  await expect(list()).toBeVisible();
  await expectPlace(list, listAt);
});

// History keeps what it is showing (a search, a conversation being read) in the pane's own state, which a tab switch drops: the pane returns on its list.
// A pane that merely hides (another pane in the split is maximized) keeps that state, so its search results and read view are proven there.
test('History keeps its search results and the conversation it is reading while a split neighbour is maximized', async ({ page }) => {
  await page.clock.setFixedTime(new Date(2026, 9, 9, 15, 0));
  const split = { ...tab('sp', 'Split'), split: { layout: '1x2', focus: 0, panes: [pane('h', 'History', { kind: 'history' }), pane('c', 'Alpha chat', { sessionFile: 'chat-a.jsonl' })] } };
  await open(page, [split], 'sp', { scenario: historyScenario(), viewport: { width: 1500, height: 800 } });
  const history = page.locator('.workspace-pane').nth(0);
  await expect(history.getByRole('heading', { name: 'History' })).toBeVisible();

  // Search results.
  await history.getByRole('textbox', { name: 'Search history' }).fill('topic');
  const results = history.locator('.history-results-scroll');
  await expect(results).toBeVisible();
  expect(await max(results)).toBeGreaterThan(200);
  const resultsAt = await wheel(page, results, 700);
  expect(resultsAt.top).toBeGreaterThan(150);
  const maximizeOther = async () => {
    await page.getByRole('button', { name: 'Pane menu Alpha chat' }).click({ force: true });
    await page.getByRole('menuitem', { name: 'Maximize' }).click();
    await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(1);
    await page.getByRole('button', { name: 'Pane menu Alpha chat' }).click({ force: true });
    await page.getByRole('menuitem', { name: 'Restore panes' }).click();
    await expect(page.locator('.workspace-pane[hidden]')).toHaveCount(0);
  };
  await maximizeOther();
  await expectPlace(() => page.locator('.workspace-pane').nth(0).locator('.history-results-scroll'), resultsAt);

  // The conversation being read.
  await history.getByRole('textbox', { name: 'Search history' }).fill('');
  await history.getByRole('option', { name: /A very long discussion/ }).click();
  await history.getByRole('button', { name: 'Read conversation' }).click();
  const read = () => page.locator('.workspace-pane').nth(0).locator('.history-read-scroll');
  await expect(read()).toBeVisible();
  await expect(page.locator('.history-message').first()).toBeVisible();
  const readAt = await wheel(page, read(), 1500);
  expect(readAt.top).toBeGreaterThan(400);
  await maximizeOther();
  await expectPlace(read, readAt);
});

test('Settings and the new-tab field keep their place on a short window', async ({ page }) => {
  await open(page, [tab('s', 'Settings', { kind: 'settings' }), tab('n', 'New tab', { kind: 'newtab' }), chat('a', 'Alpha chat', 'chat-a.jsonl')], 's', { viewport: { width: 900, height: 300 } });
  const settings = () => page.locator('.settings-scroll');
  await expect(settings()).toBeVisible();
  expect(await max(settings())).toBeGreaterThan(150);
  const sAt = await wheel(page, settings(), 400);
  expect(sAt.top).toBeGreaterThan(100);
  await select(page, 'New tab');
  await expect(page.locator('.newtab')).toBeVisible();
  const field = page.getByRole('combobox', { name: 'Search or start' });
  await expect(field).toBeVisible();
  expect(await max(page.locator('.newtab'))).toBeGreaterThan(20);
  const nAt = await wheel(page, page.locator('.newtab'), 300);
  expect(nAt.top).toBeGreaterThan(10);
  await select(page, 'Alpha chat');
  await ready(page, 'Alpha');
  await select(page, 'Settings');
  await expectPlace(settings, sAt);
  await select(page, 'New tab');
  await expectPlace(() => page.locator('.newtab'), nAt);
});

test('a terminal returns to the scrollback line it was on, without sending the program a byte', async ({ page }) => {
  const output = Array.from({ length: 400 }, (_, i) => `out line ${String(i + 1).padStart(3, '0')}`).join('\r\n') + '\r\n';
  const { engine } = await open(page, [tab('term', 'long-job', { kind: 'terminal' }), chat('a', 'Alpha chat', 'chat-a.jsonl')], 'term', {
    scenario: { ...withHistory([]), terminals: [{ id: 'job-1', command: 'long-job', title: 'long-job', output }] },
    extra: { 'codeaf.desktop.terminals.v1': JSON.stringify({ term: { sessionFile: SESSION, terminalId: 'job-1' } }) },
  });
  const rows = () => page.locator('.terminal-pane .xterm-rows > div');
  await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('out line 400');
  const firstRow = async () => (await rows().first().innerText()).trim();
  const screen = page.locator('.terminal-pane .xterm-screen');
  const box = (await screen.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -2600);
  let last = '';
  await expect.poll(async () => { const now = await firstRow(); const still = now === last; last = now; return still; }, { timeout: 5000 }).toBe(true);
  const row = await firstRow();
  expect(row).toMatch(/out line \d{3}/);
  expect(row).not.toContain('out line 400');
  const wasAt = Number(row.slice(-3));
  expect(wasAt).toBeLessThan(380);

  await select(page, 'Alpha chat');
  await ready(page, 'Alpha');
  await select(page, 'long-job');
  await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('out line');
  await expect.poll(firstRow, { timeout: 8000 }).toBe(row);
  // Restoring a place is a view change: nothing was typed into the program.
  expect(engine.calls.filter(c => c.method === 'POST' && c.path.endsWith('/input'))).toHaveLength(0);

  // A terminal left at its end comes back at its end, and keeps following new output.
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, 9000);
  await expect.poll(async () => (await firstRow()).slice(-3) >= '370' || (await rows().last().innerText()).includes('400')).toBe(true);
});

test('the reader wins over a restore that is still waiting for content that has not arrived', async ({ page }) => {
  // The saved place (900) is deeper than the file is when the pane returns, so the restore waits. The reader scrolls meanwhile; when the
  // file then grows and is re-read, the restore must not drag them to 900. Without the cancellation it does, inside its six-second window.
  const { engine } = await open(page, [fileTab('fa', 'a.txt', 'a.txt'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'fa');
  const body = () => page.locator('.file-body');
  await expect(page.getByText('Alpha line 1', { exact: true })).toBeVisible();
  const saved = await wheel(page, body(), 900);
  expect(saved.top).toBeGreaterThan(600);
  await select(page, 'Beta chat');
  await ready(page, 'Beta');
  engine.replaceFile('a.txt', { mime: 'text/plain', dataBase64: b64(lines('Alpha', 45)) });
  await select(page, 'a.txt');
  await expect(page.getByText('Alpha line 1', { exact: true })).toBeVisible();
  const shortMax = await max(body());
  expect(shortMax).toBeLessThan(saved.top);
  expect(shortMax).toBeGreaterThan(60);
  // Let the file's first stat set its baseline, then grow the file and scroll as the reader.
  await page.waitForTimeout(600);
  const mine = await wheel(page, body(), 80);
  engine.replaceFile('a.txt', { mime: 'text/plain', dataBase64: b64(lines('Alpha', 400) + '\n') });
  await expect.poll(() => max(body()), { timeout: 8000 }).toBeGreaterThan(saved.top + 100);
  await page.waitForTimeout(1500);
  near(await top(body()), mine.top, 4);
});

test('a reload restores each tab, and another window of the same browser cannot move its places', async ({ page, context }) => {
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  await ready(page, 'Alpha');
  const aTop = await wheel(page, chatScroller(page), -1300);
  await select(page, 'Beta chat');
  await ready(page, 'Beta');
  await wheel(page, chatScroller(page), -400);
  const keys = () => page.evaluate(prefix => Object.keys(localStorage).filter(k => k.startsWith(prefix) && !k.endsWith('.index')).length, STORE_PREFIX);
  await expect.poll(keys).toBe(1);
  await select(page, 'Alpha chat');
  // A second window shares localStorage but has its own place memory: it scrolls Alpha somewhere else entirely.
  const other = await context.newPage();
  await other.route('**/api/engine/**', async route => route.fallback());
  const second = await open(other, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'a');
  expect(second.seen.length).toBeGreaterThanOrEqual(0);
  await ready(other, 'Alpha');
  await wheel(other, other.locator('.conversation-scroll'), -200);
  await expect.poll(() => other.evaluate(prefix => Object.keys(localStorage).filter(k => k.startsWith(prefix) && !k.endsWith('.index')).length, STORE_PREFIX)).toBe(2);
  await other.close();
  await page.reload();
  await expect(page.getByText('Alpha question', { exact: true })).toBeAttached();
  await expectPlace(() => chatScroller(page), aTop);
});

test('closing a tab keeps its place for Reopen, and a place nobody can reopen is forgotten', async ({ page }) => {
  await open(page, [chat('a', 'Alpha chat', 'chat-a.jsonl'), chat('b', 'Beta chat', 'chat-b.jsonl')], 'b');
  await ready(page, 'Beta');
  const bTop = await wheel(page, chatScroller(page), -900);
  const stored = () => page.evaluate(prefix => { const key = Object.keys(localStorage).find(k => k.startsWith(prefix) && !k.endsWith('.index')); return key ? JSON.parse(localStorage.getItem(key)!) as { panes: Record<string, unknown>; retired: string[] } : null; }, STORE_PREFIX);
  await expect.poll(async () => Object.keys((await stored())?.panes ?? {}).join()).toContain('b');
  await tabButton(page, 'Beta chat').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Close tab/ }).click();
  await expect(tabButton(page, 'Beta chat')).toHaveCount(0);
  await expect.poll(async () => ((await stored())?.retired ?? []).join()).toBe('b');
  await tabButton(page, 'Alpha chat').click({ button: 'right' });
  await page.getByRole('menuitem', { name: /^Reopen closed tab/ }).click();
  await expect(tabButton(page, 'Beta chat')).toHaveAttribute('aria-selected', 'true');
  await ready(page, 'Beta');
  await expectPlace(() => chatScroller(page), bTop);
  await expect.poll(async () => ((await stored())?.retired ?? []).length).toBe(0);
});
