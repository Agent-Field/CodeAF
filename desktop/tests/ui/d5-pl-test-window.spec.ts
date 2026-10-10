import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import type { SharedWorkspace } from '../../src/features/workspace-sync/shared';

const tabs = (page: Page) => page.getByRole('tab', { name: /^(?!Inbox$).*/ });

const place = 'pl_0123456789abcdef';
const other = 'pl_fedcba9876543210';
const conversation = (id: string) => ({ id, kind: 'conversation' as const, title: id, titleSource: 'manual' as const, draft: `draft-${id}`, pinned: false });

for (const theme of ['light', 'dark']) {
  test(`window place restores its own tabs and keeps running work · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    const engine = await installMockEngine(page, { initial: { running: true, sessionFile: sessionFileFor('running') } });
    await installMockPlaces(page, { places: [{ id: place, name: 'Reading', pinned: true }, { id: other, name: 'Writing', pinned: true }], chats: [{ id: 'running', live: true }] });
    await page.addInitScript(state => {
      if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
    }, { tabs: [{ ...conversation('Running chat'), draft: '', sessionFile: sessionFileFor('running') }], groups: [], closed: [], activeId: 'Running chat', recentIds: ['Running chat'], nextNumber: 2 });
    await page.goto('/');
    await expect(tabs(page)).toHaveCount(1);
    await expect(page.getByRole('tab', { name: /^(Reading|Writing)$/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Keep this while running');
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(tabs(page).first()).toHaveAccessibleName('Reading');
    await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    await expect(tabs(page)).toHaveCount(2);
    await page.getByRole('button', { name: /^Writing/ }).first().click();
    await expect(tabs(page)).toHaveCount(1);
    await expect(tabs(page).first()).toHaveAccessibleName('Writing');
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(tabs(page)).toHaveCount(2);
    await expect(tabs(page).first()).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('button', { name: /^Now/ }).first().click();
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep this while running');
    await expect(page.getByRole('button', { name: 'Steer', exact: true })).toBeVisible();
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toEqual([]);
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep this while running');
  });

  test(`canonical place tabs gain Home first and Home composer inserts after it · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await installMockEngine(page, { initial: { sessionFile: sessionFileFor('created') } });
    await installMockPlaces(page, { places: [{ id: place, name: 'Reading', pinned: true }], live: ['created'] });
    // The canonical document predates Home; the local import seed already has one, so only the engine read exposes this case.
    let workspace: SharedWorkspace = { schema: 1, tabs: [conversation('Saved chat')], groups: [], closed: [], nextNumber: 2 };
    let revision = 1;
    await page.route(`**/api/engine/workspaces/${place}*`, async route => {
      if (route.request().method() === 'PUT') {
        const body = route.request().postDataJSON();
        if (body.revision !== revision) return route.fulfill({ status: 409, json: { code: 'conflict', current: { key: place, revision, workspace } } });
        workspace = body.workspace;
        revision++;
      } else if (new URL(route.request().url()).searchParams.has('wait')) {
        await new Promise(resolve => setTimeout(resolve, 200));
      }
      await route.fulfill({ json: { key: place, revision, workspace } });
    });
    await page.goto('/');
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(page.getByRole('tab', { name: 'Saved chat', exact: true })).toBeVisible();
    await expect(tabs(page)).toHaveCount(2);
    await expect(tabs(page).first()).toHaveAccessibleName('Reading');
    await tabs(page).first().click();
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Start from Home');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(tabs(page)).toHaveCount(3);
    await expect(tabs(page).nth(1)).toHaveAccessibleName('Start from Home');
    await expect(tabs(page).nth(1)).toHaveAttribute('aria-selected', 'true');
    await expect(tabs(page).nth(2)).toHaveAccessibleName('Saved chat');
    await expect.poll(() => workspace.tabs.filter(tab => tab.kind !== 'inbox').map(tab => tab.title)).toEqual(['Reading', 'Start from Home', 'Saved chat']);
    // A later canonical replacement needs the same Home repair without taking focus from the conversation.
    workspace = { ...workspace, tabs: workspace.tabs.filter(tab => tab.kind !== 'home') };
    revision++;
    await expect(tabs(page).first()).toHaveAccessibleName('Reading');
    await expect.poll(() => workspace.tabs[0].kind).toBe('home');
    await expect(page.getByRole('tab', { name: 'Start from Home', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.reload();
    await expect(page.getByRole('tab', { name: 'Start from Home', exact: true })).toHaveAttribute('aria-selected', 'true');
  });
}
