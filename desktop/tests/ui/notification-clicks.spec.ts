import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { clickNotice, installDeepLinkMock, nativeCalls } from './support/deep-link-mock';
import { trayQuestions } from './support/scenarios-v2';

// Interactions "Notifications": clicking a system notification focuses that question in the tray. The IPC mock stands
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

async function open(page: Page) {
  await installDeepLinkMock(page);
  const engine = await installMockEngine(page, { history, initial: { sessionFile: SESSION, title: 'Fix the parser', entries: [], needsPerson: true, running: true, questions: trayQuestions() } });
  await seed(page, [{ id: 'a', title: 'Intro' }, { id: 'b', title: 'Fix the parser', sessionFile: SESSION }], 'a');
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Intro', exact: true })).toHaveAttribute('aria-selected', 'true');
  return engine;
}

test('a click on a question notification selects its conversation and brings that question forward', async ({ page }) => {
  const engine = await open(page);
  // The second question is a member of the batch card, so the tray must page past the first question to reach it.
  await clickNotice(page, [{ chatId: CHAT, question: { kind: 'consent', id: 2 } }]);
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(tray(page).getByRole('status')).toHaveText(/^2 of 2$/);
  await expect(tray(page).getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
  expect(await nativeCalls(page)).toContain('notify_claim');
  // The tab that already showed the conversation was selected; no second one was opened for it.
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveCount(1);
  expect(turns(engine)).toBe(0);
});

test('a click on a notification whose question was answered opens the conversation and moves nothing in its tray', async ({ page }) => {
  const engine = await open(page);
  await clickNotice(page, [{ chatId: CHAT, question: { kind: 'consent', id: 99 } }]);
  await expect(page.getByRole('tab', { name: 'Fix the parser', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(tray(page).getByRole('status')).toHaveText(/^1 of 2$/);
  await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
  expect(turns(engine)).toBe(0);
});
