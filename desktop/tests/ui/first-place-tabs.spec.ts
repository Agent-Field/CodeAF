import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
const place = 'pl_0123456789abcdef';
for (const theme of ['light', 'dark']) test(`first place matching tabs move once, preserve contents, Undo and reload · ${theme}`, async ({ page }) => {
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  const engine = await installMockEngine(page, { initial: { entries: [], title: '' } });
  await installMockPlaces(page, { places: [{ id: place, name: 'Reading', lastOpenedAt: 'now' }], chats: [{ id: 'member', title: 'Saved reading', places: ['Reading'] }, { id: 'other', title: 'Unrelated' }] });
  const pane = (id: string) => ({ id, title: id, kind: 'conversation', draft: `draft-${id}`, sessionFile: sessionFileFor(id) });
  const moving = { ...pane('member'), pinned: false, groupId: 'g', route: { back: ['older'], forward: [], taskId: 'task' }, folded: { thought: true } };
  const home = (id: string, target: string) => ({ id, title: 'Home', kind: 'home', draft: '', pinned: true, place: target });
  const source = { schema: 1, tabs: [home('home', 'root'), moving, { ...pane('other'), pinned: false }, { ...pane('member-pin'), pinned: true }], groups: [{ id: 'g', title: 'Reading group', collapsed: true }], closed: [], nextNumber: 9 };
  const destination = { schema: 1, tabs: [home('place-home', place)], groups: [], closed: [], nextNumber: 2 };
  const docs = new Map<string, any>([['now', { key: 'now', revision: 1, workspace: source }], [place, { key: place, revision: 1, workspace: destination }]]);
  const intents = new Map<string, any>();
  const posts: any[] = [];
  await page.route('**/api/engine/workspaces/**', async route => {
    const url = new URL(route.request().url()), parts = url.pathname.split('/').filter(Boolean), key = parts[3];
    const method = route.request().method();
    const answer = (body: any, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (method === 'GET') {
      if (url.searchParams.has('wait')) await new Promise(resolve => setTimeout(resolve, 200));
      return answer(docs.get(key) ?? { key, revision: 0, workspace: null });
    }
    const ask = route.request().postDataJSON();
    if (parts[4] === 'transfer') {
      posts.push(ask);
      const previous = intents.get(ask.intent);
      if (previous) return answer({ ...previous, already: true });
      const from = docs.get(key), to = docs.get(ask.destination);
      if (from.revision !== ask.sourceRevision || to.revision !== ask.destinationRevision) return answer({ code: 'conflict', error: 'changed', source: from, destination: to }, 409);
      const source = { key, revision: from.revision + 1, workspace: ask.sourceWorkspace }, destination = { key: ask.destination, revision: to.revision + 1, workspace: ask.destinationWorkspace };
      docs.set(key, source); docs.set(ask.destination, destination);
      const result = { intent: ask.intent, source, destination, already: false }; intents.set(ask.intent, result); return answer(result);
    }
    const current = docs.get(key) ?? { key, revision: 0, workspace: null };
    if (ask.revision !== current.revision) return answer({ code: 'conflict', error: 'changed', current }, 409);
    const record = { key, revision: current.revision + 1, workspace: ask.workspace }; docs.set(key, record); return answer(record);
  });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'All places', exact: true })).toBeVisible();
  await page.getByRole('button', { name: /^Reading/ }).first().click();
  await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
  const line = page.getByLabel('Matching tabs suggestion', { exact: true });
  await expect(line).toContainText('1 matching tab');
  if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/first-place-offer-${theme}-${test.info().project.name}.png` });
  await line.getByRole('button', { name: 'Move matching tabs' }).click();
  await expect(line).toHaveCount(0);
  expect(posts).toHaveLength(1);
  expect(docs.get(place).workspace.tabs[1]).toEqual(moving);
  expect(docs.get(place).workspace.groups).toEqual(source.groups);
  expect(docs.get('now').workspace.tabs.map((t: any) => t.id)).toEqual(['home', 'other', 'member-pin']);
  await expect(page.locator('.workspace-group-name').filter({ hasText: 'Reading group' })).toBeVisible();
  if (process.env.CODEAF_UI_RESULTS) await page.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/first-place-moved-${theme}-${test.info().project.name}.png` });
  await page.locator('.toast').getByRole('button', { name: 'Undo', exact: true }).click();
  await expect.poll(() => docs.get('now').workspace.tabs.map((t: any) => t.id)).toEqual(source.tabs.map(t => t.id));
  expect(posts).toHaveLength(2);
  expect(docs.get(place).workspace.tabs.map((t: any) => t.id)).toEqual(['place-home']);
  await page.reload();
  await expect(page.getByRole('tab', { name: 'member', exact: true })).toHaveCount(0);
  // Now often has only one matching conversation: move it in one click, leaving the ordinary quiet new tab.
  const latest = docs.get('now');
  docs.set('now', { ...latest, revision: latest.revision + 1, workspace: { ...source, tabs: [moving] } });
  await expect(line).toContainText('1 matching tab');
  await line.getByRole('button', { name: 'Move matching tabs' }).click();
  await expect(line).toHaveCount(0);
  const placeholder = docs.get('now').workspace.tabs[0];
  expect(docs.get('now').workspace.tabs).toHaveLength(1);
  expect(placeholder.kind).toBe('conversation'); expect(placeholder.draft).toBe(''); expect(placeholder.sessionFile).toBeUndefined();
  await page.locator('.toast').filter({ hasText: 'Moved 1 matching tab' }).getByRole('button', { name: 'Undo', exact: true }).click();
  await expect.poll(() => docs.get('now').workspace.tabs.map((t: any) => t.id)).toEqual(['member']);
  expect(engine.calls.some(call => /stop|cancel|remove/.test(call.path))).toBe(false);
});
