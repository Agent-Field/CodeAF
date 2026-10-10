import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion, EngineTaskRow } from '../../src/features/chat/engine-client';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// Shell 3h card body: "N running" when the engine sent a count, the live command it sent, and Allow all / Review
// on a card that needs you. Nothing is drawn when the engine sent none of that.
const permission = (id: number, head: string, tasks: string[]): EngineQuestion => ({
  id, kind: 'consent', ask: 'permission', head, batch: 'step:1',
  options: [{ key: '1', label: 'allow once' }, { key: '3', label: 'deny', safe: true }],
  blocking: { turn: true, tasks },
});
const running = (id: string, live?: EngineTaskRow['Live']): EngineTaskRow => ({ ID: id, Title: id, Status: 'running', Live: live });
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, titleSource: 'manual', kind: 'conversation', draft: '', pinned: false, ...over });

async function seed(page: Page, tabs: Record<string, unknown>[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id) };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const overview = (page: Page) => page.getByRole('dialog', { name: 'All tabs overview', exact: true });
const card = (page: Page, id: string) => overview(page).locator(`.overview-card[data-card-id="${id}"]`);

test('a conversation card says N running and a task card quotes the one live command', async ({ page }) => {
  const base = plainReply();
  await installMockEngine(page, { ...base, initial: { ...base.initial, running: true, entries: [{ Role: 'assistant', Text: 'The fixtures run now.' }], tasks: [running('t1'), running('t2'), running('t7', { Step: 7, Command: 'go test ./internal/parse/...' }), running('t8')] } });
  await seed(page, [
    tab('a', 'Config stack', { sessionFile: 'stack.jsonl' }),
    tab('b', 'Update fixtures', { kind: 'task', sessionFile: 'stack.jsonl', route: { taskId: 't7', back: [''], forward: [] } }),
    tab('c', 'Quiet', { draft: '' }),
  ], 'a');
  await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const conversation = card(page, 'a');
  await expect(conversation.locator('.overview-card-state')).toHaveText('4 running');
  const dot = conversation.locator('.overview-dot');
  await expect(dot).toHaveCSS('width', design.foundation['mark-dot']);
  await expect(dot).toHaveCSS('height', design.foundation['mark-dot']);
  await expect(dot).toHaveCSS('background-color', await tokenColor(page, 'accent'));
  await expect(conversation.locator('.preview-text')).toHaveText('Last reply: “The fixtures run now.”');
  await expect(card(page, 'c')).not.toContainText('No work yet');
  await expect(card(page, 'c').locator('.overview-card-state')).toHaveCount(0);
  await overview(page).getByRole('button', { name: 'Done', exact: true }).click();

  await page.getByRole('tab', { name: 'Update fixtures', exact: true }).click();
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const task = card(page, 'b');
  await expect(task.locator('.overview-card-state')).toHaveText('step 7');
  await expect(task.getByRole('group', { name: 'Live command' })).toHaveText('$ go test ./internal/parse/...');
  await expect(task.locator('.preview-line')).toHaveCSS('color', await tokenColor(page, 'ink'));
});

test('Allow all on a needs-you card answers on the engine; Review opens the tab', async ({ page }) => {
  const base = plainReply();
  const questions = [permission(1, 'Run git step 1', ['t3']), permission(2, 'Run git step 2', ['t3']), permission(3, 'Run git step 3', ['t3'])];
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, needsPerson: true, questions, tasks: [{ ID: 't3', Title: 'Port', Status: 'running' }] } });
  await seed(page, [
    tab('a', 'Config stack', { sessionFile: 'stack.jsonl' }),
    tab('b', 'Port fix', { kind: 'task', sessionFile: 'fix.jsonl', route: { taskId: 't3', back: [''], forward: [] } }),
  ], 'b');
  await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const task = card(page, 'b');
  await expect(task.locator('.overview-card-state')).toHaveText('Needs you');
  await expect(task.locator('.overview-dot')).toHaveCSS('background-color', await tokenColor(page, 'amber'));
  await expect(task).toContainText('Allow 3 actions?');
  const allow = task.getByRole('button', { name: 'Allow all', exact: true });
  await expect(allow).toBeVisible();
  await expect(allow).toHaveCSS('height', design.foundation['preview-action-height']);
  await allow.click();
  await expect.poll(() => engine.calls.filter(call => call.path.endsWith('/answer')).length).toBe(3);
  expect(engine.calls.filter(call => call.path.endsWith('/answer')).map(call => call.body.id)).toEqual([1, 2, 3]);
  await expect(overview(page)).toBeVisible();
  await expect(task.getByRole('button', { name: 'Allow all', exact: true })).toHaveCount(0);

  await engine.update({ needsPerson: true, questions: [permission(9, 'Run once more', ['t3'])] });
  await expect(task.getByRole('button', { name: 'Allow', exact: true })).toBeVisible({ timeout: 6000 });
  await task.getByRole('button', { name: 'Review', exact: true }).click();
  await expect(overview(page)).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toHaveAttribute('aria-selected', 'true');
});
