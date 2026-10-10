import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { AddressInfo, Socket } from 'node:net';
import { test, expect, type Page } from '@playwright/test';
import { ENGINE_REQUEST_TIMEOUT_MS, type EngineSnapshot } from '../../src/features/chat/engine-client';
import { RECONNECT_WINDOW_MS } from '../../src/features/conversation/offline/reconnect';
import { MODEL } from './support/mock-engine';
import { message } from './support/conversation';

// Saved tabs share one engine: each sessionFile is its own conversation and an
// empty POST /sessions creates a new one, the way the canonical bridge does.
// The engine is a real loopback server, not a page.route fulfilment, so an
// idle event stream holds a real browser connection exactly as the live engine's
// does. Browsers allow six connections per host. Only the open tab holds a
// conversation stream; a tab in the background reads the world stream and does
// not GET /sessions/{id}.
type Call = { method: string; path: string; body: Record<string, unknown> };

const SAVED = 6;

function snapshot(id: string, sessionFile: string, entries: EngineSnapshot['entries'] = []): EngineSnapshot {
 return { id, sessionFile, workspace: '/mock-workspace', model: MODEL, persistent: true, running: false, needsPerson: false, entries, tasks: [], usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: '', seq: 1, questions: [] };
}

async function readBody(request: IncomingMessage): Promise<Record<string, unknown>> {
 let text = '';
 for await (const chunk of request) text += chunk;
 return text ? JSON.parse(text) as Record<string, unknown> : {};
}

async function startEngine() {
 const calls: Call[] = [];
 const byFile = new Map<string, EngineSnapshot>();
 const byId = new Map<string, EngineSnapshot>();
 const sockets = new Set<Socket>();
 let created = 0;
 let stalled = false;
 const open = (sessionFile?: string) => {
  const known = sessionFile && byFile.get(sessionFile);
  if (known) return known;
  const value = sessionFile
   ? snapshot(`session-${sessionFile.replace(/\.jsonl$/, '')}`, sessionFile, [{ Role: 'user', Text: `Saved ${sessionFile}` }, { Role: 'assistant', Text: `Reply in ${sessionFile}` }])
   : snapshot(`session-new-${++created}`, `new-${created}.jsonl`);
  byFile.set(value.sessionFile, value); byId.set(value.id, value);
  return value;
 };
 const json = (response: ServerResponse, value: unknown, status = 200) => {
  response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  response.end(JSON.stringify(value));
 };
 const server = createServer(async (request, response) => {
  const url = new URL(request.url ?? '/', 'http://engine');
  const body = request.method === 'POST' ? await readBody(request) : {};
  calls.push({ method: request.method ?? '', path: url.pathname, body });
  const [root, id, action] = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean).map(decodeURIComponent);
  if (root !== 'sessions') return json(response, { error: 'unknown route' }, 404);
  // A stalled engine accepts the connection and never answers.
  if (!id && stalled && !body.sessionFile) return;
  if (!id) return json(response, open(typeof body.sessionFile === 'string' ? body.sessionFile : undefined));
  const session = byId.get(id);
  if (!session) return json(response, { error: 'reattach this conversation' }, 404);
  if (!action) return json(response, session);
  if (action === 'turn') {
   const text = String(body.text ?? '');
   const next = { ...session, seq: session.seq + 1, entries: [...session.entries, { Role: 'user' as const, Text: text }, { Role: 'assistant' as const, Text: `Answered: ${text}` }] };
   byId.set(id, next); byFile.set(next.sessionFile, next);
   return json(response, { accepted: true });
  }
  if (action === 'events') {
   // An idle stream: open, quiet, and holding its connection until the reader leaves.
   response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
   response.write(': connected\n\n');
   return;
  }
  return json(response, { error: 'unknown action' }, 404);
 });
 server.on('connection', socket => { sockets.add(socket); socket.on('close', () => sockets.delete(socket)); });
 await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
 const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
 return {
  calls,
  creates: () => calls.filter(c => c.method === 'POST' && /\/sessions$/.test(c.path)),
  stallNewSessions: () => { stalled = true; },
  /**
   * Chromium sends the page's engine requests over its own sockets to this
   * server, so the six-connection limit is real. WebKit cannot redirect a
   * continued request to another origin, so there the test runner relays them.
   */
  async attach(page: Page, browserName: string) {
   await page.route('**/api/engine/**', async route => {
    const url = new URL(route.request().url());
    const target = `${origin}${url.pathname}${url.search}`;
    if (browserName === 'chromium') return route.continue({ url: target });
    try { await route.fulfill({ response: await route.fetch({ url: target }) }); }
    catch { await route.abort().catch(() => undefined); }
   });
  },
  close: () => new Promise<void>(resolve => { sockets.forEach(socket => socket.destroy()); server.close(() => resolve()); }),
 };
}

type Engine = Awaited<ReturnType<typeof startEngine>>;
let engine: Engine;
test.beforeEach(async ({ page, browserName }) => { engine = await startEngine(); await engine.attach(page, browserName); });
test.afterEach(async ({ page }) => { await page.close(); await engine.close(); });

async function seedSavedTabs(page: Page) {
 const tabs = Array.from({ length: SAVED }, (_, index) => ({ id: `saved-${index + 1}`, title: `Saved ${index + 1}`, titleSource: 'manual', pinned: false, draft: '', sessionFile: `saved-${index + 1}.jsonl` }));
 const state = { tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: SAVED + 1, recentIds: tabs.map(tab => tab.id) };
 await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const savedLine = (page: Page, index: number) => page.getByText(`Saved saved-${index}.jsonl`, { exact: true });

// The sent words, as the transcript shows them. A text query over the whole panel also matches the composer: its textarea mirrors
// the draft as text content, and the draft is cleared only once the engine has accepted the send (so a refusal keeps it).
const sentWords = (page: Page, text: string) => page.locator('.user-message-text').filter({ hasText: text });

test('a new tab beside six saved tabs sends to a new session of its own', async ({ page }) => {
 await seedSavedTabs(page);
 await page.goto('/');
 await expect(savedLine(page, 1)).toBeVisible();
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
 // The field makes no engine call until a row is chosen; Enter on its first row starts the conversation and sends.
 expect(engine.creates().filter(call => !call.body.sessionFile)).toHaveLength(0);
 const field = page.getByRole('combobox', { name: 'Search or start' });
 await field.fill('Hello from the seventh tab');
 await field.press('Enter');
 await expect(page.getByRole('tab', { name: 'Hello from the seventh tab', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(sentWords(page, 'Hello from the seventh tab')).toBeVisible();
 await expect(message(page)).toHaveValue('');
 expect(engine.creates().filter(call => !call.body.sessionFile)).toHaveLength(1);
 const turns = engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/turn'));
 expect(turns.map(call => call.path)).toEqual(['/api/engine/sessions/session-new-1/turn']);
 // The saved conversation keeps its own history.
 await page.getByRole('tab', { name: 'Saved 1', exact: true }).click();
 await expect(savedLine(page, 1)).toBeVisible();
 await expect(sentWords(page, 'Hello from the seventh tab')).toHaveCount(0);
});

test('reload attaches the open saved tab exactly once', async ({ page }) => {
 await seedSavedTabs(page);
 // The open tab is Saved 1. The other five stay in the background and do not attach.
 const attachedOnce = async () => {
  await expect(savedLine(page, 1)).toBeVisible();
  await expect.poll(() => engine.creates().map(call => String(call.body.sessionFile ?? ''))).toEqual(['saved-1.jsonl']);
  await page.waitForTimeout(500);
  expect(engine.creates().map(call => String(call.body.sessionFile ?? ''))).toEqual(['saved-1.jsonl']);
 };
 await page.goto('/');
 await attachedOnce();
 engine.calls.length = 0;
 await page.reload();
 await attachedOnce();
});

const sessionGets = (calls: Call[]) => calls.filter(call => call.method === 'GET' && /^\/api\/engine\/sessions\/[^/]+$/.test(call.path));

test('a background tab does not repeat GET /sessions/{id}', async ({ page }) => {
 await page.clock.install();
 await seedSavedTabs(page);
 await page.goto('/');
 await expect(savedLine(page, 1)).toBeVisible();
 const before = sessionGets(engine.calls).length;
 // The old background read was every 2s. Ten seconds of timers must not add one.
 await page.clock.fastForward(10_000);
 expect(sessionGets(engine.calls).length).toBe(before);
 expect(before).toBe(0);
});

test('a send the engine never answers is held in the pane and the composer stays editable', async ({ page }) => {
 await page.clock.install();
 engine.stallNewSessions();
 await page.goto('/');
 await message(page).fill('Still here after the wait');
 await message(page).press('Enter');
 await expect.poll(() => engine.creates().length).toBe(1);
 await page.clock.runFor(ENGINE_REQUEST_TIMEOUT_MS + 1000);
 await expect(page.getByRole('status').filter({ hasText: 'Reconnecting to the engine…' })).toBeVisible();
 await expect(page.getByRole('tabpanel').getByRole('button', { name: 'Retry', exact: true })).toHaveCount(0);
 await expect(page.locator('.user-message[data-sending]')).toHaveText('Still here after the wait');
 await expect(message(page)).toHaveValue('');
 await expect(message(page)).toBeEnabled();
 await page.clock.fastForward(RECONNECT_WINDOW_MS);
 await expect(page.getByRole('status').filter({ hasText: "Can't reach the engine" })).toBeVisible();
 await expect(page.getByRole('tabpanel').getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
 await expect(page.getByText('codeaf engine is not running')).toHaveCount(0);
 await expect(message(page)).toBeEnabled();
});
