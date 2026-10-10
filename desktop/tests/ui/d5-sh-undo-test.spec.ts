import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installWorldEngine } from './support/world-engine';

// SH-153, SH-154, SH-274, SH-310. ⌘Z (Ctrl Z off a Mac) takes back this window's last structural
// step — a close, a move into a group, a pin — and stops at twenty. Typing in the composer keeps
// the key, because the composer is a text field. Light and dark are not drawn here; the key is
// the same in both. Width does not change the key either.

const KEY = 'codeaf.desktop.workspace.v1';
const CAP = 20;
type Seed = { tabs: unknown[]; groups?: unknown[]; closed?: unknown[]; activeId: string };
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });

async function seed(page: Page, value: Seed) {
  const state = { groups: [], closed: [], nextNumber: value.tabs.length + 1, recentIds: value.tabs.map(item => (item as { id: string }).id), ...value };
  await page.addInitScript(([key, json]) => {
    if (!sessionStorage.getItem('undo-spec-seeded')) {
      localStorage.setItem(key, json);
      sessionStorage.setItem('undo-spec-seeded', '1');
    }
  }, [KEY, JSON.stringify(state)] as const);
}

/** The modifier this page treats as ⌘. Spoofed before load, so both chords are the ones the shell matches. */
async function usePlatform(page: Page, mac: boolean) {
  await page.addInitScript(platform => {
    Object.defineProperty(navigator, 'platform', { value: platform, configurable: true });
  }, mac ? 'MacIntel' : 'Linux x86_64');
}

const primary = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control'));
const named = (page: Page, name: string) => page.getByRole('tab', { name, exact: true });
const message = (page: Page) => page.getByRole('textbox', { name: 'Message', exact: true });
const pinned = (page: Page, name: string) => page.locator('.workspace-tab.is-pinned', { has: page.getByRole('tab', { name, exact: true }) });

/** The strip as a person reads it: "*Title" when pinned, "[Label: members]" for a group. */
const strip = (page: Page) => page.locator('.workspace-tabstrip').evaluate(root => Array.from(root.children).flatMap(el => {
  if (el.classList.contains('workspace-tab-group')) {
    const label = el.querySelector('.workspace-group-name')?.textContent;
    const members = Array.from(el.querySelectorAll('.workspace-group-tab-slot:not([data-hidden="true"]) [role="tab"]')).map(node => node.getAttribute('aria-label')).join(' ');
    return [`[${label}: ${members}]`];
  }
  if (!el.classList.contains('workspace-tab')) return [];
  const title = el.querySelector('[role="tab"]')?.getAttribute('aria-label') ?? '';
  return [`${el.classList.contains('is-pinned') ? '*' : ''}${title}`];
}));

async function boot(page: Page, value: Seed, mac?: boolean) {
  await page.route('**/api/engine/**', route => route.abort());
  if (mac !== undefined) await usePlatform(page, mac);
  await seed(page, value);
  await page.goto('/');
  await expect(named(page, 'Alpha')).toBeVisible();
}

/** ⌘Z while a tab, not the composer, has focus. The composer takes focus by itself, and that would keep the key. */
async function undo(page: Page) {
  await page.getByRole('tab', { selected: true }).press(`${await primary(page)}+z`);
}

async function openTabMenu(page: Page, name: string) {
  await named(page, name).focus();
  await page.keyboard.press('Shift+F10');
  const menu = page.getByRole('menu', { name: `Actions for ${name}`, exact: true });
  await expect(menu).toBeVisible();
  return menu;
}

test.describe('structural undo from the keyboard', () => {
  const three = { tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { draft: 'beta draft' }), tab('c', 'Gamma')], activeId: 'b' };

  for (const mac of [false, true]) {
    test(`closing a tab then ${mac ? '⌘' : 'Ctrl'} Z puts it back where it stood, draft included`, async ({ page }) => {
      await boot(page, three, mac);
      const before = await strip(page);
      expect(before).toEqual(['Alpha', 'Beta', 'Gamma']);
      // WebKit treats ⌘W as closing its own window. The menu's Close tab is the same close, and ⌘Z is what this test proves.
      if (mac) {
        const menu = await openTabMenu(page, 'Beta');
        await menu.getByRole('menuitem', { name: /^Close tab\b/ }).press('Enter');
      } else await named(page, 'Beta').press(`${await primary(page)}+w`);
      await expect(named(page, 'Beta')).toHaveCount(0);
      await undo(page);
      await expect.poll(() => strip(page)).toEqual(before);
      await expect(named(page, 'Beta')).toHaveAttribute('aria-selected', 'true');
      await expect(message(page)).toHaveValue('beta draft');
      if (!mac) await expectAccessible(page);
    });
  }

  test('moving a tab into a group then ⌘Z takes it back out', async ({ page }) => {
    await boot(page, {
      tabs: [tab('a', 'Alpha'), tab('b', 'Beta', { groupId: 'g' }), tab('c', 'Gamma')],
      groups: [{ id: 'g', title: 'Bench', collapsed: false }],
      activeId: 'c',
    });
    await expect.poll(() => strip(page)).toEqual(['Alpha', '[Bench: Beta]', 'Gamma']);
    const menu = await openTabMenu(page, 'Gamma');
    await menu.getByRole('menuitem', { name: 'Add to group', exact: true }).focus();
    await page.keyboard.press('ArrowRight');
    const submenu = page.getByRole('menu', { name: /Add to group/ });
    await expect(submenu.getByRole('menuitemcheckbox', { name: 'Bench', exact: true })).toBeVisible();
    await submenu.getByRole('menuitemcheckbox', { name: 'Bench', exact: true }).press('Enter');
    await expect.poll(() => strip(page)).toEqual(['Alpha', '[Bench: Beta Gamma]']);
    await undo(page);
    await expect.poll(() => strip(page)).toEqual(['Alpha', '[Bench: Beta]', 'Gamma']);
  });

  test('pinning a tab from the keyboard then ⌘Z unpins it', async ({ page }) => {
    await boot(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'b' });
    const menu = await openTabMenu(page, 'Beta');
    await menu.getByRole('menuitem', { name: 'Pin tab', exact: true }).press('Enter');
    await expect.poll(() => strip(page)).toEqual(['*Beta', 'Alpha']);
    await undo(page);
    await expect.poll(() => strip(page)).toEqual(['Alpha', 'Beta']);
    await expect(pinned(page, 'Beta')).toHaveCount(0);
  });

  test('⌘Z in the composer undoes the words and leaves the pin', async ({ page }) => {
    await boot(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
    const menu = await openTabMenu(page, 'Beta');
    await menu.getByRole('menuitem', { name: 'Pin tab', exact: true }).press('Enter');
    await expect.poll(() => strip(page)).toEqual(['*Beta', 'Alpha']);
    await named(page, 'Alpha').click();
    const field = message(page);
    await field.click();
    await page.keyboard.type('hello');
    await expect(field).toHaveValue('hello');
    await field.press(`${await primary(page)}+z`);
    await expect(field).not.toHaveValue('hello');
    expect((await field.inputValue()).length).toBeLessThan('hello'.length);
    await expect.poll(() => strip(page)).toEqual(['*Beta', 'Alpha']);
    await undo(page);
    await expect.poll(() => strip(page)).toEqual(['Alpha', 'Beta']);
  });

  test('the window forgets the oldest step past twenty', async ({ page }) => {
    test.setTimeout(90_000);
    await boot(page, { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' });
    const mark = pinned(page, 'Alpha');
    for (let step = 0; step < CAP + 1; step += 1) {
      const on = (await mark.count()) > 0;
      const menu = await openTabMenu(page, 'Alpha');
      await menu.getByRole('menuitem', { name: on ? 'Unpin tab' : 'Pin tab', exact: true }).press('Enter');
      await expect(mark).toHaveCount(on ? 0 : 1);
    }
    await expect(mark).toHaveCount(1);
    await undo(page);
    await expect(mark).toHaveCount(0);
    for (let step = 0; step < CAP - 1; step += 1) await undo(page);
    // Twenty-one toggles, twenty undone: the forgotten first pin stays done.
    await expect(mark).toHaveCount(1);
    const kept = await strip(page);
    await undo(page);
    expect(await strip(page)).toEqual(kept);
    await expect(mark).toHaveCount(1);
  });
});

test('each window undoes only the pin it made', async ({ page, context }) => {
  const other = await context.newPage();
  // Unknown engine routes must not hang on a live proxy. The world mock is registered after, so it wins its own paths.
  for (const view of [page, other]) await view.route('**/api/engine/**', route => route.abort());
  await installWorldEngine(page, 'empty', [other]);
  const value = { tabs: [tab('a', 'Alpha'), tab('b', 'Beta')], activeId: 'a' };
  await seed(page, value);
  await seed(other, value);
  await page.goto('/');
  await expect(named(page, 'Alpha')).toBeVisible();
  await expect.poll(() => page.evaluate(async () => ((await (await fetch('/api/engine/workspaces/now')).json()) as { revision: number }).revision)).toBeGreaterThan(0);
  await other.goto('/');
  await expect(named(other, 'Beta')).toBeVisible();

  const menu = await openTabMenu(page, 'Alpha');
  await menu.getByRole('menuitem', { name: 'Pin tab', exact: true }).press('Enter');
  await expect(pinned(page, 'Alpha')).toHaveCount(1);
  await expect(pinned(other, 'Alpha')).toHaveCount(1);

  await named(other, 'Alpha').focus();
  await expect(named(other, 'Alpha')).toBeFocused();
  await other.keyboard.press(`${await primary(other)}+z`);
  await expect(pinned(other, 'Alpha')).toHaveCount(1);

  const there = await openTabMenu(other, 'Beta');
  await there.getByRole('menuitem', { name: 'Pin tab', exact: true }).press('Enter');
  await expect(pinned(other, 'Beta')).toHaveCount(1);
  await expect(pinned(page, 'Beta')).toHaveCount(1);
  await expect(pinned(page, 'Alpha')).toHaveCount(1);

  await undo(page);
  await expect(pinned(page, 'Alpha')).toHaveCount(0);
  await expect(pinned(page, 'Beta')).toHaveCount(1);
  await expect(pinned(other, 'Beta')).toHaveCount(1);
  await expect(page.locator('.toast').filter({ hasText: 'Could not undo' })).toHaveCount(0);
  // One page for the scan: a second WebKit page still holding the workspace poll has crashed the check.
  await other.close();
  await expectAccessible(page);
});
