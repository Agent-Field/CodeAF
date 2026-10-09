import { expect, test, type Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';

// Two (or three) real windows on one place, through the real hook and client, against the real bridge and its
// real store on disk. Every test uses its own place key, so the engines and runs never see each other's tabs.
const freshKey = () => `pl_${randomBytes(8).toString('hex')}`;

type Row = { id: string; title: string; active: boolean; pinned: boolean; group: string; draft: string; panes: string };
const strip = (page: Page) => page.getByTestId('tab').evaluateAll(els => els.map(el => {
  const d = (el as HTMLElement).dataset;
  return { id: d.id!, title: el.textContent ?? '', active: d.active === 'true', pinned: d.pinned === 'true', group: d.group ?? '', draft: d.draft ?? '', panes: d.panes ?? '' };
}));
/** The shared part of a strip: everything but which tab this window shows. */
const shared = async (page: Page) => (await strip(page)).map(({ active: _a, ...row }: Row) => row);
const settled = async (page: Page) => {
  await expect(page.getByTestId('status')).toHaveAttribute('data-phase', 'saved', { timeout: 15_000 });
  await expect(page.getByTestId('status')).toHaveAttribute('data-unsaved', '0');
};
const act = (page: Page, action: unknown) => page.evaluate(a => window.__ws!.dispatch(a as never), action);
const tab = (id: string, title = id) => ({ id, title, draft: '', pinned: false, kind: 'conversation' });
const puts = (page: Page) => { const seen: string[] = []; page.on('request', r => { if (r.method() === 'PUT' && r.url().includes('/api/engine/workspaces/')) seen.push(r.url()); }); return seen; };

async function twoWindows(browser: import('@playwright/test').Browser, key: string) {
  const context = await browser.newContext();
  const a = await context.newPage();
  const b = await context.newPage();
  await a.goto(`/?key=${key}`);
  await settled(a);
  await b.goto(`/?key=${key}`);
  await settled(b);
  return { context, a, b };
}

test('two windows editing at the same moment keep every change, show one tab set, and come back after a reload', async ({ browser }) => {
  const key = freshKey();
  const { context, a, b } = await twoWindows(browser, key);
  await expect.poll(() => shared(b)).toEqual(await shared(a));

  // Both windows open tabs at once.
  await Promise.all([
    act(a, { type: 'open', tab: tab('a1', 'Alpha'), background: true }).then(() => act(a, { type: 'open', tab: tab('a2', 'Beta'), background: true })),
    act(b, { type: 'open', tab: tab('b1', 'Gamma'), background: true }),
  ]);
  await settled(a); await settled(b);
  await expect.poll(async () => (await shared(a)).map(r => r.id).sort()).toEqual([`${key}-home`, 'a1', 'a2', 'b1'].sort());
  await expect.poll(() => shared(b)).toEqual(await shared(a));

  // Reorder in A, group in B, drafts typed into both, and a close in A of the tab B is typing into — all at once.
  await a.evaluate(() => window.__ws!.dispatch({ type: 'select', id: 'a1' } as never));
  await b.evaluate(() => window.__ws!.dispatch({ type: 'select', id: 'a2' } as never));
  await Promise.all([
    act(a, { type: 'reorder', id: 'b1', targetId: 'a1' }),
    act(b, { type: 'group', id: 'a1', ids: ['b1'], title: 'Release' }),
    a.getByLabel('Draft').pressSequentially('typed in A', { delay: 5 }),
    b.getByLabel('Draft').pressSequentially('kept after close', { delay: 5 }),
    act(a, { type: 'close', id: 'a2' }),
  ]);
  await settled(a); await settled(b);
  await expect.poll(() => shared(b), { timeout: 15_000 }).toEqual(await shared(a));
  const rows = await shared(a);
  expect(rows.map(r => r.id)).not.toContain('a2');
  expect(rows.find(r => r.id === 'a1')).toMatchObject({ group: 'Release', draft: 'typed in A' });
  expect(rows.find(r => r.id === 'b1')).toMatchObject({ group: 'Release' });
  await expect(a.getByTestId('closed')).toHaveText('a2');
  // The words typed into the tab A closed are on the closed tab: Reopen brings them back.
  await act(b, { type: 'reopen' });
  await settled(b);
  await expect.poll(async () => (await shared(a)).find(r => r.id === 'a2')?.draft).toBe('kept after close');
  expect(Number(await a.getByTestId('status').getAttribute('data-overtaken'))).toBe(0);

  // The engine's copy carries no window's own focus.
  const record = await a.evaluate(async k => (await fetch(`/api/engine/workspaces/${k}`)).json(), key);
  expect(JSON.stringify(record.workspace)).not.toMatch(/activeId|recentIds|"focus"/);

  // Reload both: the tab set, A's own focus and its scroll come back.
  await a.getByTestId('scroller').evaluate(el => { el.scrollTop = 640; el.dispatchEvent(new Event('scroll')); });
  const before = await shared(a);
  const activeA = (await strip(a)).find(r => r.active)!.id;
  await a.waitForTimeout(300);
  await Promise.all([a.reload(), b.reload()]);
  await settled(a); await settled(b);
  expect(await shared(a)).toEqual(before);
  expect(await shared(b)).toEqual(before);
  expect((await strip(a)).find(r => r.active)!.id).toBe(activeA);
  await expect.poll(() => a.getByTestId('scroller').evaluate(el => el.scrollTop)).toBe(640);
  await context.close();
});

test('choosing a tab in one window writes nothing and does not move the other window', async ({ browser }) => {
  const key = freshKey();
  const { context, a, b } = await twoWindows(browser, key);
  await act(a, { type: 'open', tab: tab('x1'), background: true });
  await settled(a);
  await expect.poll(async () => (await shared(b)).length).toBe(2);
  const writes = [...puts(a), ...puts(b)];
  const bActive = (await strip(b)).find(r => r.active)!.id;
  await act(a, { type: 'select', id: 'x1' });
  await a.waitForTimeout(1_000);
  expect((await strip(a)).find(r => r.active)!.id).toBe('x1');
  expect((await strip(b)).find(r => r.active)!.id).toBe(bActive);
  expect(writes).toEqual([]);
  await context.close();
});

test('offline, a window keeps its changes, says why, does not hammer the engine, and catches up — even across a reload', async ({ browser }) => {
  const key = freshKey();
  const { context, a, b } = await twoWindows(browser, key);
  let attempts = 0;
  await a.route('**/api/engine/workspaces/**', route => { attempts++; return route.abort('connectionrefused'); });
  await act(a, { type: 'open', tab: tab('offline-1', 'Written offline'), background: true });
  await expect(a.getByTestId('status')).toHaveAttribute('data-phase', 'offline');
  await expect(a.getByTestId('status')).toContainText('codeaf engine is not running');
  attempts = 0;
  await a.waitForTimeout(3_000);
  expect(attempts, 'retries back off instead of spinning').toBeLessThanOrEqual(4);
  // A reload while offline keeps the unsaved change on screen.
  await a.reload();
  await expect(a.getByTestId('tab').filter({ hasText: 'Written offline' })).toHaveCount(1);
  await expect(a.getByTestId('status')).not.toHaveAttribute('data-unsaved', '0');
  await a.unroute('**/api/engine/workspaces/**');
  await a.evaluate(() => window.__ws!.retry());
  await settled(a);
  await expect.poll(async () => (await shared(b)).map(r => r.id)).toContain('offline-1');
  await context.close();
});

test('Move to new window on the same place hands over focus and leaves the tab in every window', async ({ browser }) => {
  const key = freshKey();
  const { context, a, b } = await twoWindows(browser, key);
  await act(a, { type: 'open', tab: tab('m1', 'Moving'), background: false });
  await act(a, { type: 'open', tab: tab('m2', 'Staying'), background: true });
  await settled(a);
  const writes = puts(a);
  const handed = await a.evaluate(() => window.__ws!.handoff('m1')) as { key: string; tabId: string };
  expect(handed).toEqual({ key, tabId: 'm1' });
  expect((await strip(a)).find(r => r.active)!.id).not.toBe('m1');
  const c = await context.newPage();
  await c.goto(`/?key=${key}&focus=${handed.tabId}`);
  await settled(c);
  expect((await strip(c)).find(r => r.active)!.id).toBe('m1');
  for (const page of [a, b, c]) await expect.poll(async () => (await shared(page)).map(r => r.id)).toContain('m1');
  expect(writes).toEqual([]);
  await context.close();
});

test('Now imports the tabs a person had in localStorage once, and a second window does not import again', async ({ browser, browserName }) => {
  // The bridge's store outlives one engine's run; only one engine may own the first import of `now`.
  test.skip(browserName !== 'chromium', 'the import of now happens once per store');
  const context = await browser.newContext();
  await context.addInitScript(() => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'v1-a', title: 'From v1', draft: 'old draft', pinned: false }], groups: [], closed: [], activeId: 'v1-a', nextNumber: 2, recentIds: ['v1-a'] }));
  });
  const a = await context.newPage();
  await a.goto('/?key=now');
  await settled(a);
  expect((await shared(a)).map(r => [r.id, r.draft])).toEqual([['v1-a', 'old draft']]);
  await a.evaluate(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'v1-b', title: 'Changed later', draft: '', pinned: false }], groups: [], closed: [], activeId: 'v1-b', nextNumber: 2, recentIds: ['v1-b'] })));
  const b = await context.newPage();
  await b.goto('/?key=now');
  await settled(b);
  expect((await shared(b)).map(r => r.id)).toEqual(['v1-a']);
  expect(await a.evaluate(() => localStorage.getItem('codeaf.desktop.workspace.v1'))).toContain('v1-b');
  await context.close();
});

test('a save the engine refuses because the other window saved first is replayed over it, not lost', async ({ browser }) => {
  const key = freshKey();
  const { context, a, b } = await twoWindows(browser, key);
  // Hold A's write in flight until B has saved: A's write then carries a stale revision, for certain.
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  let heldOnce = false;
  await a.route('**/api/engine/workspaces/**', async route => {
    if (route.request().method() === 'PUT' && !heldOnce) { heldOnce = true; await held; }
    await route.continue();
  });
  const refused: number[] = [];
  a.on('response', r => { if (r.request().method() === 'PUT' && r.status() === 409) refused.push(r.status()); });
  await act(a, { type: 'open', tab: tab('race-a', 'From A'), background: true });
  await expect.poll(() => heldOnce).toBe(true);
  await act(b, { type: 'open', tab: tab('race-b', 'From B'), background: true });
  await act(b, { type: 'rename', id: `${key}-home`, title: 'Renamed in B' });
  await settled(b);
  release();
  await settled(a);
  expect(refused.length, 'the engine refused the stale write').toBeGreaterThan(0);
  await expect.poll(() => shared(b)).toEqual(await shared(a));
  const rows = await shared(a);
  expect(rows.map(r => r.id).sort()).toEqual([`${key}-home`, 'race-a', 'race-b'].sort());
  expect(rows.find(r => r.id === `${key}-home`)?.title).toBe('Renamed in B');
  expect(Number(await a.getByTestId('status').getAttribute('data-overtaken'))).toBe(0);
  await context.close();
});
