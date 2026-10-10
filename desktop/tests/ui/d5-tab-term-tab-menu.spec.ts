import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// C-EDGE-6: a finished job's tab right-click carries Run again (and Remove job just under Close tab).
const SESSION = 'mock-session-1.jsonl';

async function openOn(page: Page, terminalId: string, title: string) {
  await page.addInitScript(([id, name, terminal, session]) => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id, kind: 'terminal', title: name, draft: '', pinned: false }], groups: [], activeId: id, closed: [], nextNumber: 2, recentIds: [id] }));
    localStorage.setItem('codeaf.desktop.terminals.v1', JSON.stringify({ [id]: { sessionFile: session, terminalId: terminal } }));
  }, ['term-tab', title, terminalId, SESSION] as const);
  await page.goto('/');
  await expect(page.locator('.terminal-header')).toBeVisible();
}

test('d5-tab-term-test: right-clicking a finished job tab offers Run again', async ({ page }) => {
  const engine = await installMockEngine(page, {
    ...plainReply(),
    terminals: [{ id: 'done-1', command: 'make test', title: 'make test', state: 'exited', exitCode: 0, endedAt: new Date(Date.now() - 120_000).toISOString(), output: 'all green\r\n' }],
  });
  await openOn(page, 'done-1', 'make test');
  await expect(page.locator('.terminal-header .terminal-state')).toHaveText(/^exit 0/);
  await page.getByRole('tab', { name: 'make test', exact: true }).click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Actions for make test', exact: true });
  const labels = await menu.locator('.menu-label').allTextContents();
  const close = labels.indexOf('Close tab');
  expect(labels[close - 1]).toBe('Run again');
  expect(labels[close + 1]).toBe('Remove job');
  await expect(menu.locator('.menu-item-danger')).toHaveText('Remove job');
  await menu.getByRole('menuitem', { name: 'Run again', exact: true }).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  const start = engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/terminals'));
  expect(start).toHaveLength(1);
  expect(start[0].body).toMatchObject({ command: 'make test' });
});
