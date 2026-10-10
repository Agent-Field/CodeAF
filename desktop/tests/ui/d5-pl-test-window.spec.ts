import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';
import type { SharedWorkspace } from '../../src/features/workspace-sync/shared';

const tabs = (page: Page) => page.getByRole('tab');

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
    await expect.poll(() => workspace.tabs.map(tab => tab.title)).toEqual(['Reading', 'Start from Home', 'Saved chat']);
    // A later canonical replacement needs the same Home repair without taking focus from the conversation.
    workspace = { ...workspace, tabs: workspace.tabs.filter(tab => tab.kind !== 'home') };
    revision++;
    await expect(tabs(page).first()).toHaveAccessibleName('Reading');
    await expect.poll(() => workspace.tabs[0].kind).toBe('home');
    await expect(page.getByRole('tab', { name: 'Start from Home', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.reload();
    await expect(page.getByRole('tab', { name: 'Start from Home', exact: true })).toHaveAttribute('aria-selected', 'true');
  });

  test(`collapsed Home opens the place switcher and the second slot jumps · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await installMockEngine(page, { initial: {} });
    const config = 'pl_0011223344556677';
    await installMockPlaces(page, {
      places: [
        { id: place, name: 'Reading', pinned: true, tint: 'tide' },
        { id: other, name: 'Writing', pinned: true, tint: 'iris' },
        { id: config, name: 'Config parser', parents: ['Reading'], lastOpenedAt: 'now', tint: 'rose' },
      ],
      chats: [{ id: 'need', title: 'Allow the push', places: ['Config parser'], needsYou: true, reason: 'Allow the push?' }],
    });
    await page.goto('/');
    const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
    const rail = page.locator('.app-shell .place-rail').first();
    await rail.getByRole('button', { name: /^Reading/ }).click();
    const home = page.locator('.workspace-tab.is-place-home');
    await expect(home).toContainText('Reading');
    await rail.getByRole('button', { name: 'Hide sidebar' }).click();
    await expect(page.locator('.app-shell.sidebar-collapsed')).toHaveCount(1);
    await expect(home.getByRole('tab')).toHaveAttribute('aria-description', '1 needs you in Config parser');
    await home.getByRole('tab').click();
    const menu = page.getByRole('menu', { name: 'Place switcher' });
    await expect(menu).toBeVisible();
    await menu.evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished)));
    await expect(menu.getByText('Inbox')).toHaveCount(0);
    await expect(menu.getByText('Pinned', { exact: true })).toBeVisible();
    await expect(menu.getByText('Open', { exact: true })).toBeVisible();
    const current = menu.getByRole('menuitemcheckbox', { name: /^Reading/ });
    await expect(current).toHaveAttribute('aria-checked', 'true');
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Writing/ })).toContainText(mac ? '⌃2' : 'Alt+2');
    await expect(menu.getByRole('menuitem', { name: /^All places/ })).toBeVisible();
    const parent = menu.locator('.rail-place-path');
    await expect(parent).toHaveText(' · Reading');
    await expect(menu.getByRole('img', { name: '1 needs you in Config parser' })).toHaveCount(2);
    const geometry = await current.evaluate(el => {
      const row = getComputedStyle(el);
      const box = el.getBoundingClientRect();
      const pop = el.closest('[role="menu"]') as HTMLElement;
      const menuStyle = getComputedStyle(pop);
      const menuBox = pop.getBoundingClientRect();
      const probe = document.createElement('div');
      probe.style.background = 'var(--field-2)';
      probe.style.color = 'var(--ink-3)';
      document.body.append(probe);
      const field = getComputedStyle(probe).backgroundColor;
      const quiet = getComputedStyle(probe).color;
      probe.remove();
      const path = el.closest('[role="menu"]')!.querySelector('.rail-place-path') as HTMLElement;
      const tab = document.querySelector('.home-tab-select') as HTMLElement;
      const tabBox = tab.getBoundingClientRect();
      return {
        row: [box.width, box.height, row.fontSize, row.gap, row.borderRadius],
        fill: row.backgroundColor === field,
        parent: getComputedStyle(path).color === quiet,
        menu: [menuBox.width, menuStyle.borderRadius, menuStyle.padding],
        under: menuBox.y >= tabBox.bottom - 1 && Math.abs(menuBox.x - tabBox.x) < 8,
      };
    });
    expect(geometry.row).toEqual([280, 32, '13px', '10px', '7px']);
    expect(geometry.fill).toBe(true);
    expect(geometry.parent).toBe(true);
    expect(geometry.menu).toEqual([290, '12px', '5px']);
    expect(geometry.under).toBe(true);
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(current).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toBeFocused();
    await page.keyboard.press(mac ? 'Control+Digit2' : 'Alt+Digit2');
    await expect(home).toContainText('Writing');
    await expect(page.getByRole('heading', { name: 'Writing', exact: true })).toBeVisible();
    if (await menu.isVisible()) await page.keyboard.press('Escape');
    await expect(menu).toBeHidden();
    await home.getByRole('tab').click();
    await expect(menu).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(menu).toBeHidden();
    await expect(home.getByRole('tab')).toBeFocused();
  });
}
