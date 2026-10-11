import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';

const tabs = (page: Page) => page.getByRole('tab');

const place = 'pl_0123456789abcdef';
const other = 'pl_fedcba9876543210';
const conversation = (id: string) => ({ id, kind: 'conversation' as const, title: id, titleSource: 'manual' as const, draft: `draft-${id}`, pinned: false });

for (const theme of ['light', 'dark']) {
  test(`a rail click adds a Home tab and leaves the tab set and running work alone · ${theme}`, async ({ page }) => {
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
    // Places are libraries (2026-10-11): a rail click adds the Home as a tab after the one in front and replaces nothing.
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(tabs(page)).toHaveCount(2);
    await expect(tabs(page).nth(1)).toHaveAccessibleName('Reading');
    await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    await expect(tabs(page)).toHaveCount(3);
    await page.getByRole('button', { name: /^Writing/ }).first().click();
    await expect(tabs(page)).toHaveCount(4);
    await expect(tabs(page).filter({ hasText: 'Writing' })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(tabs(page)).toHaveCount(4);
    await expect(tabs(page).filter({ hasText: 'Reading' })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('button', { name: /^Now/ }).first().click();
    await expect(tabs(page)).toHaveCount(4);
    await tabs(page).first().click();
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep this while running');
    await expect(page.getByRole('button', { name: 'Steer', exact: true })).toBeVisible();
    expect(engine.calls.filter(call => call.path.endsWith('/stop'))).toEqual([]);
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('Keep this while running');
  });

  test(`Home composer inserts its chat right after that Home, in the one shared tab set · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await installMockEngine(page, { initial: { sessionFile: sessionFileFor('created') } });
    await installMockPlaces(page, { places: [{ id: place, name: 'Reading', pinned: true }], live: ['created'] });
    await page.addInitScript(state => {
      if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
    }, { tabs: [conversation('Saved chat')], groups: [], closed: [], activeId: 'Saved chat', recentIds: ['Saved chat'], nextNumber: 2 });
    await page.goto('/');
    await page.getByRole('button', { name: /^Reading/ }).first().click();
    await expect(page.getByRole('tab', { name: 'Saved chat', exact: true })).toBeVisible();
    await expect(tabs(page)).toHaveCount(2);
    await expect(tabs(page).nth(1)).toHaveAccessibleName('Reading');
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Start from Home');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(tabs(page)).toHaveCount(3);
    await expect(tabs(page).nth(2)).toHaveAccessibleName('Start from Home');
    await expect(tabs(page).nth(2)).toHaveAttribute('aria-selected', 'true');
    await expect(tabs(page).nth(0)).toHaveAccessibleName('Saved chat');
    await page.reload();
    await expect(tabs(page)).toHaveCount(3);
    await expect(page.getByRole('tab', { name: 'Start from Home', exact: true })).toBeVisible();
  });

  test(`with the rail hidden the second slot key opens that place's Home as a tab · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    await installMockEngine(page, { initial: {} });
    await installMockPlaces(page, {
      places: [{ id: place, name: 'Reading', pinned: true, tint: 'tide' }, { id: other, name: 'Writing', pinned: true, tint: 'iris' }],
      chats: [],
    });
    await page.goto('/');
    const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
    const rail = page.locator('.app-shell .place-rail').first();
    await rail.getByRole('button', { name: /^Reading/ }).click();
    await expect(page.locator('.workspace-tab.is-place-home').getByRole('tab')).toHaveAccessibleName('Reading');
    await rail.getByRole('button', { name: 'Hide sidebar' }).click();
    await expect(page.locator('.app-shell.sidebar-collapsed')).toHaveCount(1);
    // The Home tab is an ordinary tab now: clicking it focuses it and opens no switcher.
    await page.locator('.workspace-tab.is-place-home').getByRole('tab').click();
    await expect(page.getByRole('menu', { name: 'Place switcher' })).toHaveCount(0);
    await page.keyboard.press(mac ? 'Control+Digit2' : 'Alt+Digit2');
    await expect(tabs(page).filter({ hasText: 'Writing' })).toHaveAttribute('aria-selected', 'true');
    await expect(tabs(page).filter({ hasText: 'Reading' })).toHaveCount(1);
    await expect(page.getByRole('heading', { name: 'Writing', exact: true })).toBeVisible();
  });
}
