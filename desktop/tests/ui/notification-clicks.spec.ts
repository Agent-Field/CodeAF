import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { clickNotice, installDeepLinkMock, nativeCalls } from './support/deep-link-mock';
import { trayQuestions } from './support/scenarios-v2';

// Interactions "Notifications": clicking a system notification starts Next up at that item. The IPC mock stands
// in for src-tauri/src/activation.rs, which queues a click's target only when the platform reports a click on a
// notification codeaf posted; the target is the conversation and question the renderer named when it was posted.
// It is NOT native proof that the operating system delivered a click. Nothing here calls a model.
const CHAT = '9446cc2627f3deae';
const SESSION = `/mock/places/${CHAT}/transcript.jsonl`;
const at = new Date(Date.now() - 3_600_000).toISOString();
const history = { conversations: [{ id: CHAT, title: 'Fix the parser', at }] };
const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });
const turns = (engine: MockEngine) => engine.calls.filter(call => call.path.endsWith('/turn')).length;

async function seed(page: Page, tabs: { id: string; title: string; sessionFile?: string }[], active: string) {
  const state = {
    tabs: tabs.map(t => ({ draft: '', titleSource: 'manual', kind: 'conversation', pinned: false, ...t })),
    groups: [], closed: [], activeId: active, nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id),
  };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

async function open(page: Page, world?: Parameters<typeof installMockEngine>[1]['world'], epoch?: string) {
  await installDeepLinkMock(page);
  const engine = await installMockEngine(page, { history, ...(world ? { world } : {}), initial: { sessionFile: SESSION, title: 'Fix the parser', entries: [], needsPerson: true, running: true, questions: trayQuestions() } });
  if (epoch && world) await page.route('**/api/engine/events?**', route => route.fulfill({ status: 200, contentType: 'text/event-stream', body: `id: 42\ndata: ${JSON.stringify({ epoch, seq: 42, type: 'reset', at: 'x', payload: world })}\n\n` }));
  await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'a');
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Intro', exact: true })).toHaveAttribute('aria-selected', 'true');
  return engine;
}

test('a notification starts Next up at its item and advances after withdrawal', async ({ page }) => {
  const engine = await open(page, { rows: [], items: [
    { key: `${CHAT}:consent:2`, session: CHAT, kind: 'consent', id: 2, text: 'Run it?', sourceFolders: [], answerable: true },
    { key: `${CHAT}:ask:7`, session: CHAT, kind: 'ask', id: 7, text: 'Choose?', sourceFolders: [], answerable: true },
  ] });
  // The second question is a member of the batch card, so the tray must page past the first question to reach it.
  await clickNotice(page, [{ itemId: `${CHAT}:consent:2`, chatId: CHAT, question: { kind: 'consent', id: 2 } }]);
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(tray(page).getByRole('status')).toHaveText(/^2 of 2$/);
  await expect(tray(page).getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
  await expect(page.locator('.next-up-chip')).toHaveText('Next up1 of 2');
  expect(await nativeCalls(page)).toContain('notify_claim');
  // The tab that already showed the conversation was selected; no second one was opened for it.
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveCount(1);
  await engine.setWorld({ items: [{ key: `${CHAT}:ask:7`, session: CHAT, kind: 'ask', id: 7, text: 'Choose?', sourceFolders: [], answerable: true }] });
  await expect(page.locator('.next-up-chip')).toHaveText('Next up2 of 2');
  await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
  expect(turns(engine)).toBe(0);
});

test('a click on a notification whose question was answered opens the conversation and moves nothing in its tray', async ({ page }) => {
  const engine = await open(page);
  await clickNotice(page, [{ chatId: CHAT, question: { kind: 'consent', id: 99 } }]);
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(tray(page).getByRole('status')).toHaveText(/^1 of 2$/);
  await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
  await expect(page.locator('.next-up-chip')).toHaveCount(0);
  expect(turns(engine)).toBe(0);
});

test('the attention list and the badge name the world-feed reading they came from', async ({ page }) => {
  const landed = new Date(Date.now() - 60_000).toISOString();
  await open(page, {
    rows: [{ session: CHAT, title: 'Fix the parser', project: 'p', sourceFolders: [], state: 'idle', live: false, open: false, running: false, needsYou: true, failed: 1, unseenFailed: 1, failure: { task: 't1', at: landed }, tasks: { running: 0, incomplete: 0, done: 0, failed: 1, total: 1 }, at: landed }],
    items: [
      { key: `${CHAT}:consent:2`, session: CHAT, kind: 'consent', id: 2, text: 'Run it?', sourceFolders: [], answerable: true },
      { key: `${CHAT}:choice:3`, session: CHAT, kind: 'choice', id: 3, text: 'Suggestion?', sourceFolders: [], answerable: true, blocking: { turn: false, tasks: [] } },
      { key: `${CHAT}:choice:4`, session: CHAT, kind: 'choice', id: 4, text: 'Decided', sourceFolders: [], answerable: true, decidedBy: 'Release' },
    ],
  }, 'fixture-engine-a');
  // Rust lets only the newest reading any window reported say what is pending; without the sequence it cannot tell.
  const posted = () => page.evaluate(() => (window as unknown as { __linkMock: { calls: { cmd: string; args: Record<string, unknown> }[] } }).__linkMock.calls
    .filter(call => call.cmd === 'notify_attention' || call.cmd === 'badge_set').map(call => ({ cmd: call.cmd, seq: call.args.seq, epoch: call.args.epoch, count: call.args.count, ids: (call.args.items as { id: string }[] | undefined)?.map(item => item.id) })));
  await expect.poll(async () => (await posted()).map(call => call.cmd)).toEqual(expect.arrayContaining(['notify_attention', 'badge_set']));
  const calls = await posted();
  for (const call of calls) expect(call.epoch).toBe('fixture-engine-a');
  for (const call of calls) expect(Number.isSafeInteger(call.seq) && (call.seq as number) > 0, `${call.cmd} ${String(call.seq)}`).toBe(true);
  const last = (cmd: string) => calls.filter(call => call.cmd === cmd).at(-1);
  expect(last('badge_set')?.count).toBe(2);
  expect(last('badge_set')?.seq).toBe(last('notify_attention')?.seq);
  // The failure is named by the engine's own landing instant, the same in every window.
  expect(last('notify_attention')?.ids).toEqual([`${CHAT}:consent:2`, `failed:${CHAT}:${landed}`]);
});
