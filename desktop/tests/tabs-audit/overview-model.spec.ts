import { test, expect } from '@playwright/test';
import { installMockEngine, MODEL } from '../ui/support/mock-engine';

for (const theme of ['light', 'dark']) {
 test(`Overview footers use canonical chat/task models and truthful missing metadata ${theme}`, async ({ page }, info) => {
  const engine = await installMockEngine(page, { initial: { model: MODEL, updatedAt: '2026-10-09T20:00:00Z', tasks: [{ ID: '9', Title: 'Marker', Status: 'done', Model: 'fixture/worker-model' }], entries: [{ Role: 'user', Text: 'Inspect the marker.' }] } });
  const pane = (id: string, kind: string, extra = {}) => ({ id, kind, title: id, draft: '', pinned: false, titleSource: 'manual', ...extra });
  await page.addInitScript(({ theme, tabs }) => { localStorage.setItem('codeaf-theme', theme); localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs, groups: [], closed: [], activeId: 'blank', nextNumber: 8, recentIds: tabs.map(tab => tab.id) })); }, { theme, tabs: [
   pane('Chat', 'conversation', { sessionFile: 'mock-session-1.jsonl' }),
   pane('Task', 'task', { sessionFile: 'mock-session-1.jsonl', route: { taskId: '9', back: [''], forward: [] } }),
   pane('Missing task model', 'task', { sessionFile: 'mock-session-1.jsonl', route: { taskId: 'unknown', back: [''], forward: [] } }),
   pane('File', 'file'), pane('Web', 'web'), pane('blank', 'newtab'),
  ] });
  await page.goto('/');
  await expect.poll(() => engine.calls.filter(call => call.path.includes('/sessions')).length).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'All tabs overview' });
  const foot = (id: string) => dialog.locator(`.overview-card[data-card-id="${id}"] .overview-card-foot`);
  await expect(foot('Chat')).toContainText(' · DS Flash');
  await expect(foot('Task')).toContainText(' · fixture/worker-model');
  await expect(foot('Missing task model')).not.toContainText('DS Flash');
  await expect(foot('Missing task model')).not.toContainText(' · ');
  for (const id of ['File', 'Web', 'blank']) await expect(foot(id)).toHaveCount(0);
  expect(engine.calls.filter(call => call.method === 'PUT' && call.path.includes('/models'))).toHaveLength(0);
  if (process.env.CODEAF_OVERVIEW_EVIDENCE) await dialog.screenshot({ path: `${process.env.CODEAF_OVERVIEW_EVIDENCE}/${info.project.name}-${theme}-overview-models.png` });
  await dialog.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  await expect(foot('Task')).toContainText('fixture/worker-model');
  await expect(foot('Chat')).toContainText('DS Flash');
 });
}
