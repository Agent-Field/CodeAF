import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';

const tabs = ['a', 'b', 'c'].map(id => ({ id, kind: 'conversation', title: `Chat ${id}`, draft: '', pinned: false, sessionFile: `/chats/${id}/transcript.jsonl` }));

for (const theme of ['light', 'dark']) test(`workspace folder suggestion and undo (${theme})`, async ({ page }) => {
  await installMockEngine(page, {});
  await page.addInitScript(({ tabs, theme }) => {
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs, groups: [], closed: [], activeId: 'a', recentIds: ['a', 'b', 'c'], nextNumber: 4 }));
    localStorage.setItem('codeaf-theme', theme);
  }, { tabs, theme });
  const rows = tabs.map(tab => ({ chatId: tab.id, sessionFile: tab.sessionFile, workspace: '/work/bench', running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false }));
  await page.route('**/api/engine/events?*', route => route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify({ epoch: 'workspace-test', seq: 1, type: 'reset', payload: { rows, items: [] } })}\n\n` }));
  await page.goto('/');
  const pill = page.locator('.suggest-pill');
  await expect(pill).toContainText('Group the 3 bench tabs as bench?');
  const measured = await pill.evaluate(element => {
    const rect = element.getBoundingClientRect(), parent = element.parentElement!.getBoundingClientRect();
    const css = getComputedStyle(element);
    return { height: rect.height, top: rect.top - parent.top, centre: rect.left + rect.width / 2 - parent.left - parent.width / 2, gap: css.gap, padding: css.padding, radius: css.borderRadius };
  });
  expect(Math.abs(measured.centre)).toBeLessThan(0.1);
  expect({ ...measured, centre: 0 }).toEqual({ height: 34, top: 10, centre: 0, gap: '10px', padding: '0px 6px 0px 14px', radius: '99px' });
  await expect(pill.getByRole('button', { name: 'Group', exact: true })).toHaveCSS('height', '24px');
  await pill.getByRole('button', { name: 'Group', exact: true }).click();
  await expect(pill).toHaveCount(0);
  await expect(page.locator('.workspace-tab-group')).toHaveCount(1);
  await page.getByRole('tabpanel').focus();
  await page.keyboard.press('ControlOrMeta+z');
  await expect(page.locator('.workspace-tab-group')).toHaveCount(0);
  await expect(pill).toHaveCount(0);
  await expect(page.getByRole('status', { name: 'Notifications' })).toHaveCount(1);
});

test('mock workspace CAS preserves writer and rejects stale revisions, keys and methods', async ({ page }) => {
  await installMockEngine(page, {});
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const path = '/api/engine/workspaces/pl_0000000000000001';
    const request = async (url: string, method = 'GET', body?: unknown) => {
      const response = await fetch(url, { method, headers: { 'Content-Type': 'application/json' }, ...(body ? { body: JSON.stringify(body) } : {}) });
      return { status: response.status, body: await response.json() };
    };
    const workspace = { schema: 1, tabs: [{ id: 'tab', kind: 'newtab', title: 'New tab', draft: '', pinned: false }], groups: [], closed: [], nextNumber: 2 };
    return { initial: await request(path), saved: await request(path, 'PUT', { revision: 0, writer: 'window-a', workspace }), conflict: await request(path, 'PUT', { revision: 0, writer: 'window-b', workspace }), invalid: await request('/api/engine/workspaces/invalid'), method: await request(path, 'DELETE') };
  });
  expect(result.initial).toEqual({ status: 200, body: { key: 'pl_0000000000000001', revision: 0, workspace: null } });
  expect(result.saved.status).toBe(200);
  expect(result.saved.body).toMatchObject({ revision: 1, writer: 'window-a' });
  expect(result.conflict.status).toBe(409);
  expect(result.conflict.body).toMatchObject({ code: 'conflict', current: result.saved.body });
  expect(result.invalid.status).toBe(404);
  expect(result.method.status).toBe(405);
});
