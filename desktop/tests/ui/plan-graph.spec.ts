import { expect, test, type Page } from '@playwright/test';
import { graphFixture, capturedTaskPages } from '../../src/features/tasks/graph-fixture';
import { emptyDocument } from '../../src/features/chat/work-model';
import { dependencyStages } from '../../src/features/tasks/graph-model';
import { taskStanding, planForest } from '../../src/features/tasks/plan-model';
import { expectAccessible, expectNoUnstyledControls, tokenColor } from './contracts';

const hierarchy = (page: Page) => page.getByRole('complementary', { name: 'Plan hierarchy', exact: true });
const record = (page: Page) => page.getByRole('region', { name: 'Captured task conversation', exact: true });
const task = (page: Page, id: string) => {
 const row = graphFixture.rows.find(row => row.ID === id)!;
 return hierarchy(page).getByRole('button', { name: `${row.Title}, ${taskStanding(row).label}`, exact: true });
};
async function previewWorkspace(page: Page) {
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Preview plan workspace', exact: true }).click();
 return hierarchy(page);
}
function engineRequests(page: Page) {
 const requests: string[] = [];
 page.on('request', request => { if (request.url().includes('/api/engine/')) requests.push(request.url()); });
 return requests;
}

test('dependency stages use typed hard edges rather than containment or advisory suggestions', () => {
 const rows = [
  { ID: 'root', Title: 'Work', Status: 'running' },
  { ID: 'input', Title: 'Input', Status: 'done', Parent: 'root' },
  { ID: 'output', Title: 'Output', Status: 'pending', Parent: 'root' },
  { ID: 'advice', Title: 'Advice', Status: 'pending', Parent: 'root' },
  { ID: 'blocked', Title: 'Blocked', Status: 'pending', Parent: 'root' },
 ];
 const stages = dependencyStages(rows, [
  { from: 'input', to: 'output', kind: 'feeds_into' },
  { from: 'input', to: 'advice', kind: 'suggests' },
  { from: 'output', to: 'blocked', kind: 'blocks' },
 ]);
 expect(stages.get('root')).toBe(0);
 expect(stages.get('input')).toBe(0);
 expect(stages.get('output')).toBe(1);
 expect(stages.get('advice')).toBe(0);
 expect(stages.get('blocked')).toBe(2);
 expect(graphFixture.rows.some(row => row.Status === 'cancelled')).toBe(true);
 expect(graphFixture.dependencies).toHaveLength(5);
 expect(graphFixture.dependencies.every(edge => edge.kind === 'feeds_into')).toBe(true);
});

test('explicit capture opens a compact containment outline beside the root conversation without engine requests', async ({ page }) => {
 const requests = engineRequests(page);
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/');
 await expect(hierarchy(page)).not.toBeVisible();
 const outline = await previewWorkspace(page);
 await expect(outline).toBeVisible(); await expect(outline).toContainText('Captured plan · read-only');
 const { children } = planForest(graphFixture.rows);
 const leaves = graphFixture.rows.filter(row => !children.has(row.ID));
 await expect(outline).toContainText(`${leaves.filter(row => row.Status === 'done').length} of ${leaves.length} completed`);
 await expect(outline).toContainText(`${leaves.filter(row => row.Status === 'cancelled').length} cancelled`);
 await expect(outline.locator('.plan-outline-task')).toHaveCount(graphFixture.rows.length);
 await expect(outline.locator('svg path[data-kind]')).toHaveCount(0);
 await expect(record(page)).not.toBeVisible();
 const conversation = page.locator('.work-conversation > .work-document');
 const body = await conversation.boundingBox(); const rail = await outline.boundingBox();
 expect(body!.x + body!.width).toBeLessThanOrEqual(rail!.x + 1);
 await expect(page.getByRole('group', { name: 'Plan workspace views', exact: true })).not.toBeVisible();
 expect(requests).toEqual([]); await expectNoUnstyledControls(page);
});

test('four-level branches disclose containment, filter keeps ancestors and Escape restores search focus', async ({ page }) => {
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/'); const outline = await previewWorkspace(page);
 await outline.getByRole('button', { name: 'Collapse Email routing and urgency', exact: true }).click();
 await expect(task(page, 'z8okzz')).not.toBeVisible();
 await outline.getByRole('button', { name: 'Find tasks', exact: true }).click();
 const search = outline.getByRole('textbox', { name: 'Filter plan tasks', exact: true }); await expect(search).toBeFocused();
 await search.fill('Find email candidates');
 await expect(outline.locator('.plan-outline-task')).toHaveCount(4);
 for (const id of ['root', 'yy9bhh', '58utnd', 'z8okzz']) await expect(task(page, id)).toBeVisible();
 await expect(outline.getByRole('button', { name: 'Collapse Email routing and urgency', exact: true })).toBeDisabled();
 await search.fill('No task has this name'); await expect(outline).toContainText('No matching tasks.');
 await page.keyboard.press('Escape');
 await expect(outline.getByRole('button', { name: 'Find tasks', exact: true })).toBeFocused();
 await expect(search).not.toBeVisible();
 await outline.getByRole('button', { name: 'Expand Email routing and urgency', exact: true }).click();
 await task(page, 'z8okzz').click();
 await expect(outline).toContainText('No subtasks');
 await expect(task(page, '94pzii')).not.toBeVisible();
 await page.getByRole('navigation', { name: 'Task conversation navigation', exact: true }).getByRole('button', { name: 'Email routing and urgency', exact: true }).click();
 await expect(task(page, 'z8okzz')).toBeVisible();
 await expect(task(page, '94pzii')).not.toBeVisible();
});

test('same-tab task records preserve actual excerpts, route history and the root draft', async ({ page }) => {
 const requests = engineRequests(page);
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/'); await previewWorkspace(page);
 const draft = page.locator('.work-conversation[data-plan-workspace="true"] > .work-document > .work-root-conversation').getByRole('textbox', { name: /Draft for/ }); await draft.fill('Keep the parent instruction');
 const tabs = await page.getByRole('tab').count();
 await task(page, '94pzii').click();
 await expect(page.getByRole('tab')).toHaveCount(tabs);
 await expect(record(page)).toContainText(capturedTaskPages['94pzii'].Description!.split('`')[0].trim());
 await expect(record(page).locator('.work-ask-row')).toHaveCount(1);
 await expect(record(page).locator('.work-output .markdown')).toBeVisible();
 const instruction = record(page).getByRole('group', { name: 'Original instructions for Email sources', exact: true });
 const command = instruction.locator('code').first();
 await expect(command).toHaveText(capturedTaskPages['94pzii'].Description!.match(/`([^`]+)`/)![1]);
 const tokens = await page.evaluate(() => { const style = getComputedStyle(document.documentElement); return { code: style.getPropertyValue('--font-size-code').trim(), prose: style.getPropertyValue('--font-size-prose').trim(), mono: style.getPropertyValue('--font-mono').trim() }; });
 await expect(command).toHaveCSS('font-size', tokens.code);
 await expect(command).toHaveCSS('font-weight', '400');
 expect(await command.evaluate(el => getComputedStyle(el).fontFamily)).toContain('monospace');
 await expect(record(page).locator('.work-output .markdown p').first()).toHaveCSS('font-size', tokens.prose);
 await hierarchy(page).getByRole('button', { name: 'Close task hierarchy', exact: true }).click();
 await expect(hierarchy(page)).not.toBeVisible();
 await page.getByRole('button', { name: 'Show task hierarchy', exact: true }).click();
 await expect(hierarchy(page)).toBeVisible();
 const childDraft = record(page).getByRole('textbox', { name: /Draft for/ });
 await expect(childDraft).toBeVisible(); await childDraft.fill('Child email correction stays here');
 const fold = record(page).getByRole('button', { name: /^Fold / }).first();
 const foldTitle = (await fold.getAttribute('aria-label'))!.slice(5);
 await fold.click();
 await expect(record(page).getByRole('button', { name: `Open ${foldTitle}`, exact: true })).toHaveAttribute('aria-expanded', 'false');
 await expect(draft).not.toBeVisible();
 await page.getByRole('navigation', { name: 'Task conversation navigation', exact: true }).getByRole('button', { name: graphFixture.title, exact: true }).click();
 await task(page, 'z8okzz').click();
 await expect(record(page).getByRole('textbox', { name: /Draft for/ })).toHaveValue('');
 await record(page).getByRole('textbox', { name: /Draft for/ }).fill('Separate cancelled-task correction');
 await expect(record(page)).toContainText('No result was recorded in this capture.');
 const navigation = page.getByRole('navigation', { name: 'Task conversation navigation', exact: true });
 await expect(navigation).toContainText('Policy and moderation sources'); await expect(navigation).toContainText('Email routing and urgency');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click();
 await expect(draft).toHaveValue('Keep the parent instruction');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click();
 await expect(childDraft).toHaveValue('Child email correction stays here');
 await expect(record(page).getByRole('button', { name: `Open ${foldTitle}`, exact: true })).toHaveAttribute('aria-expanded', 'false');
 await page.getByRole('button', { name: 'Forward to next task', exact: true }).click();
 await expect(draft).toHaveValue('Keep the parent instruction');
 await page.getByRole('button', { name: 'Forward to next task', exact: true }).click();
 await expect(childDraft).toHaveValue('Separate cancelled-task correction');
 await navigation.getByRole('button', { name: graphFixture.title, exact: true }).click();
 await expect(record(page)).not.toBeVisible(); await expect(draft).toHaveValue('Keep the parent instruction');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click();
 await expect(childDraft).toHaveValue('Separate cancelled-task correction');
 await page.reload(); await expect(childDraft).toHaveValue('Separate cancelled-task correction');
 await childDraft.press('Enter'); await expect(childDraft).toHaveValue('');
 await expect(record(page)).toContainText('Separate cancelled-task correction');
 await expect(record(page)).toContainText('Staged locally · no request made');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click();
 await expect(draft).toHaveValue('Keep the parent instruction');
 expect(requests).toEqual([]);
});

test('typed connections stay secondary and navigate to their actual source without inventing parent edges', async ({ page }) => {
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/'); await previewWorkspace(page);
 await task(page, '7bs0sp').click();
 const connections = page.getByRole('button', { name: 'Connections · 5', exact: true });
 await expect(connections).toHaveAttribute('aria-expanded', 'false');
 await expect(page.getByRole('list', { name: 'Task dependencies' })).not.toBeVisible();
 await connections.click();
 const relations = page.getByRole('list', { name: 'Task dependencies' });
 await expect(relations.getByRole('listitem')).toHaveCount(5); await expect(relations).toContainText('Receives from');
 await expect(relations.getByRole('button', { name: graphFixture.title, exact: true })).toHaveCount(0);
 await relations.getByRole('button', { name: 'Email sources', exact: true }).click();
 await expect(record(page)).toContainText(capturedTaskPages['94pzii'].Description!.split('`')[0].trim());
});

test('modifier click and context menu open background task tabs with isolated routes and drafts', async ({ page }) => {
 const requests = engineRequests(page);
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/'); await previewWorkspace(page);
 const original = page.getByRole('tab', { selected: true }); const originalId = await original.getAttribute('id');
 const draft = page.locator('.work-conversation[data-plan-workspace="true"] > .work-document > .work-root-conversation').getByRole('textbox', { name: /Draft for/ }); await draft.fill('Parent draft stays here');
 const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' as const : 'Control' as const);
 await task(page, '94pzii').click({ modifiers: [modifier] });
 await expect(page.getByRole('tab')).toHaveCount(2); await expect(original).toHaveAttribute('aria-selected', 'true');
 await expect(draft).toHaveValue('Parent draft stays here'); await expect(record(page)).not.toBeVisible();
 await task(page, 'z8okzz').click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Open task in new tab', exact: true }).click();
 await expect(page.getByRole('tab')).toHaveCount(3); await expect(original).toHaveAttribute('aria-selected', 'true');
 const cancelledTab = page.getByRole('tab', { name: 'Find email candidates', exact: true });
 const cancelledState = cancelledTab.locator('.work-state-indicator');
 await expect(cancelledState).toHaveAttribute('data-phase', 'stopped');
 await expect(cancelledState).toHaveCSS('color', await tokenColor(page, 'muted'));
 await page.getByRole('tab', { name: /Email sources/ }).click(); await expect(record(page)).toContainText('Email sources');
 const childDraft = record(page).getByRole('textbox', { name: /Draft for/ });
 await childDraft.fill('Shared canonical child draft');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click(); await expect(draft).toHaveValue('');
 await page.locator(`#${originalId}`).click(); await expect(draft).toHaveValue('Parent draft stays here');
 await task(page, '94pzii').click(); await expect(childDraft).toHaveValue('Shared canonical child draft');
 await page.getByRole('button', { name: 'Back to previous task', exact: true }).click();
 await expect(draft).toHaveValue('Parent draft stays here');
 expect(requests).toEqual([]);
});

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: narrow hierarchy preserves current task across view changes and reload without overflow`, async ({ page }) => {
  await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/'); await previewWorkspace(page);
  const views = page.getByRole('group', { name: 'Plan workspace views', exact: true });
  const draft = page.locator('.work-conversation[data-plan-workspace="true"] > .work-document > .work-root-conversation').getByRole('textbox', { name: /Draft for/ }); await draft.fill('A narrow parent draft');
  await views.getByRole('button', { name: 'Plan', exact: true }).click();
  await expect(hierarchy(page)).toBeVisible(); await expect(hierarchy(page)).toHaveCSS('color', await tokenColor(page, 'text'));
  await expect(page.locator('.work-conversation > .work-document')).not.toBeVisible();
  const node = task(page, 'z8okzz');
  expect(await node.evaluate(el => getComputedStyle(el).transitionDuration.split(',').every(duration => parseFloat(duration) === 0))).toBe(true);
  await node.click(); await expect(views.getByRole('button', { name: 'Conversation', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(record(page)).toContainText('Find email candidates');
  const childDraft = record(page).getByRole('textbox', { name: /Draft for/ });
  await childDraft.fill('Narrow child correction');
  await expect(record(page).locator('.work-composer')).toBeVisible();
  const navigation = page.getByRole('navigation', { name: 'Task conversation navigation', exact: true });
  const current = navigation.locator('[aria-current="page"]');
  await expect(current).toBeVisible();
  const bounds = await navigation.boundingBox(); const crumb = await current.boundingBox();
  expect(crumb!.x).toBeGreaterThanOrEqual(bounds!.x);
  expect(crumb!.x + crumb!.width).toBeLessThanOrEqual(bounds!.x + bounds!.width + 1);
  await navigation.getByRole('button', { name: 'Conversation ancestors', exact: true }).click();
  await expect(page.getByRole('menuitem', { name: 'Policy and moderation sources', exact: true })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Email routing and urgency', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await views.getByRole('button', { name: 'Plan', exact: true }).click(); await expect(node).toHaveAttribute('aria-current', 'page');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expectAccessible(page); await expectNoUnstyledControls(page);
  await views.getByRole('button', { name: 'Conversation', exact: true }).click();
  await page.reload(); await expect(record(page)).toContainText('Find email candidates');
  await expect(childDraft).toHaveValue('Narrow child correction');
  await navigation.getByRole('button', { name: 'Conversation ancestors', exact: true }).click();
  await page.getByRole('menuitem', { name: graphFixture.title, exact: true }).click();
  await expect(draft).toHaveValue('A narrow parent draft');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 });
}

test('plan deeplink opens a separate preview and preserves existing engine bindings and drafts', async ({ page }) => {
 const binding = { sessionFile: 'saved-engine.jsonl', workspace: '/test-workspace', model: 'deepseek/deepseek-v4.1-flash' };
 const saved = { ...emptyDocument(), engine: binding };
 await page.addInitScript(saved => {
  localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'existing-engine', title: 'Existing engine work', draft: 'Do not replace my draft', pinned: false }], groups: [], activeId: 'existing-engine', closed: [], nextNumber: 2, recentIds: ['existing-engine'] }));
  localStorage.setItem('codeaf.desktop.work-documents.v1', JSON.stringify({ 'existing-engine': saved }));
 }, saved);
 const turns: string[] = []; page.on('request', request => { if (request.url().includes('/turn')) turns.push(request.url()); });
 // Existing attachments may reattach in the background; the test never starts a provider.
 await page.route('**/api/engine/**', route => route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'The saved test engine is not running' }) }));
 await page.setViewportSize({ width: 1400, height: 900 }); await page.goto('/?preview=plan');
 await expect(hierarchy(page)).toBeVisible();
 await expect(page.getByRole('tab')).toHaveCount(2);
 await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('codeaf.desktop.work-documents.v1')!)['existing-engine'].engine)).toEqual(binding);
 expect(await page.evaluate(() => JSON.parse(localStorage.getItem('codeaf.desktop.workspace.v1')!).tabs.find((tab: { id: string }) => tab.id === 'existing-engine').draft)).toBe('Do not replace my draft');
 await page.getByRole('button', { name: 'New tab', exact: true }).click();
 await expect(hierarchy(page)).not.toBeVisible();
 await expect(page.getByRole('textbox', { name: /Draft for/ })).toHaveValue('');
 await expect(page.locator('.work-document')).toHaveAttribute('data-fresh', 'true');
 expect(turns).toEqual([]);
});
