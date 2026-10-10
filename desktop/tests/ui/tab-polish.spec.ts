import { savedWorkspace } from './support/synced-workspace';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Tab polish: TA-STRIP-07 (full title in the shared 500ms tooltip), TA-KEY-12 (⌘/Ctrl O on the new-tab field),
// TA-OV-05 (⌘-click / middle-click on an overview card is the background press) and TA-OV-06 (the card's menu is the
// strip's tab menu). Real pointer and keyboard input only; the engine is away unless a test needs a running session.
const delay = design.interaction.tooltipOpenDelay;
const SESSION = 'mock-session-1.jsonl';
const LONG = 'A long conversation title that the strip can only show part of';
const running = { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Trailing commas across the config stack' }] } };
const mac = (page: Page) => page.evaluate(() => /Mac/.test(navigator.platform));
const primary = async (page: Page) => ((await mac(page)) ? 'Meta' : 'Control');

type Seed = { id: string; title: string; pinned?: boolean; running?: boolean; groupId?: string; kind?: string };
async function seed(page: Page, tabs: unknown[], activeId: string) {
  const state = { tabs, groups: [], closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(t => (t as { id: string }).id) };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const make = (t: Seed) => ({ id: t.id, title: t.title, draft: `${t.title} draft`, titleSource: 'manual', kind: t.kind ?? 'conversation', pinned: !!t.pinned, ...(t.running ? { sessionFile: SESSION } : {}) });
const tabsOf = (...tabs: Seed[]) => tabs.map(make);
const stripTab = (page: Page, title: string) => page.getByRole('tab', { name: title, exact: true });
const tooltip = (page: Page) => page.getByRole('tooltip');
const preview = (page: Page, title: string) => page.getByRole('group', { name: `Preview of ${title}`, exact: true });
const saved = savedWorkspace;
const stops = (engine: { calls: { method: string; path: string }[] }) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop')).length;

/** Samples the page for `ms` and reports whether a tooltip and a preview card were ever on screen together. */
const overlapDuring = (page: Page, ms: number) => page.evaluate(duration => new Promise<boolean>(resolve => {
  let overlapped = false;
  const timer = window.setInterval(() => {
    if (document.querySelector('[role="tooltip"]') && document.querySelector('.tab-preview')) overlapped = true;
  }, 20);
  window.setTimeout(() => { window.clearInterval(timer); resolve(overlapped); }, duration);
}), ms);

test.describe('the title tooltip on the tab strip (TA-STRIP-07)', () => {
  test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

  test('the active tab shows its full title after 500ms, and a press, Escape or leaving puts it away', async ({ page }) => {
    expect(delay).toBe(500);
    await seed(page, tabsOf({ id: 'a', title: LONG }, { id: 'b', title: 'Other' }), 'a');
    await page.goto('/');
    const active = stripTab(page, LONG);
    await expect(active).toBeVisible();
    await active.hover();
    const started = Date.now();
    await page.waitForTimeout(delay * 0.4);
    await expect(tooltip(page)).toHaveCount(0);
    await expect(tooltip(page)).toHaveText(LONG);
    expect(Date.now() - started).toBeGreaterThanOrEqual(delay * 0.9);
    // The trigger keeps its own accessible name; the tooltip adds nothing a screen reader hears twice.
    await expect(active).toHaveAccessibleName(LONG);
    await page.keyboard.press('Escape');
    await expect(tooltip(page)).toHaveCount(0);
    await page.mouse.move(0, 0);
    await active.hover();
    await expect(tooltip(page)).toHaveText(LONG);
    await page.mouse.down();
    await expect(tooltip(page)).toHaveCount(0);
    await page.mouse.up();
    await page.mouse.move(0, 0);
    await expect(tooltip(page)).toHaveCount(0);
  });

  test('keyboard focus shows the title too, but only for keyboard focus', async ({ page }) => {
    await seed(page, tabsOf({ id: 'a', title: LONG }, { id: 'b', title: 'Other' }), 'a');
    await page.goto('/');
    await expect(stripTab(page, LONG)).toBeVisible();
    await page.keyboard.press('Tab');
    for (let step = 0; step < 12 && !(await stripTab(page, LONG).evaluate(el => el === document.activeElement)); step += 1) await page.keyboard.press('Tab');
    await expect(stripTab(page, LONG)).toBeFocused();
    await expect(tooltip(page)).toHaveText(LONG);
    await page.keyboard.press('Escape');
    await expect(tooltip(page)).toHaveCount(0);
  });

  test('a pinned active tab is icon-only, so the tooltip is the only place its title is read', async ({ page }) => {
    await seed(page, tabsOf({ id: 'p', title: 'Pinned notes', pinned: true }, { id: 'b', title: 'Other' }), 'p');
    await page.goto('/');
    await expect(stripTab(page, 'Pinned notes').locator('.workspace-tab-title')).toHaveCount(0);
    await stripTab(page, 'Pinned notes').hover();
    await expect(tooltip(page)).toHaveText('Pinned notes');
  });

  test('an inactive tab opens its preview, which already holds the title: no tooltip, never both', async ({ page }) => {
    await seed(page, tabsOf({ id: 'a', title: 'Active tab' }, { id: 'b', title: LONG }, { id: 'p', title: 'Pinned notes', pinned: true }), 'a');
    await page.goto('/');
    for (const title of [LONG, 'Pinned notes']) {
      await stripTab(page, title).hover();
      await expect(preview(page, title)).toBeVisible();
      await expect(preview(page, title)).toContainText(title);
      expect(await overlapDuring(page, delay * 1.2)).toBe(false);
      await expect(tooltip(page)).toHaveCount(0);
      await page.mouse.move(0, 0);
      await expect(preview(page, title)).toHaveCount(0);
    }
  });

  test('moving from a tab with a card open onto the active tab never stacks a tooltip on the card', async ({ page }) => {
    await seed(page, tabsOf({ id: 'a', title: LONG }, { id: 'b', title: 'Neighbour' }), 'a');
    await page.goto('/');
    await stripTab(page, 'Neighbour').hover();
    await expect(preview(page, 'Neighbour')).toBeVisible();
    const overlap = overlapDuring(page, delay * 3);
    await stripTab(page, LONG).hover();
    expect(await overlap).toBe(false);
  });

  test('a tap never opens a tooltip or a card (touch is not hover)', async ({ browser }) => {
    const context = await browser.newContext({ hasTouch: true, viewport: { width: 600, height: 800 } });
    const page = await context.newPage();
    await page.route('**/api/engine/**', route => route.abort());
    await seed(page, tabsOf({ id: 'a', title: LONG }, { id: 'b', title: 'Neighbour' }), 'a');
    await page.goto('/');
    await page.getByRole('tab', { name: LONG, exact: true }).tap();
    await page.waitForTimeout(delay * 1.4);
    await expect(tooltip(page)).toHaveCount(0);
    await page.getByRole('tab', { name: 'Neighbour', exact: true }).tap();
    await page.waitForTimeout(delay * 1.4);
    await expect(tooltip(page)).toHaveCount(0);
    await expect(preview(page, 'Neighbour')).toHaveCount(0);
    await context.close();
  });

  test('every segment of the active split names itself', async ({ page }) => {
    const pane = (id: string, title: string) => ({ id, title, draft: '', kind: 'conversation' });
    const split = { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', split: { layout: '1x2', focus: 0, panes: [pane('p1', 'First pane title that is long'), pane('p2', 'Second pane title that is long')] } };
    await seed(page, [split], 'sp');
    await page.goto('/');
    await stripTab(page, 'Second pane title that is long').hover();
    await expect(tooltip(page)).toHaveText('Second pane title that is long');
  });

  for (const scheme of ['light', 'dark'] as const) for (const width of [320, 600]) {
    test(`it stays on screen and legible: ${scheme} at ${width}px`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.setViewportSize({ width, height: 800 });
      await seed(page, tabsOf({ id: 'a', title: LONG }, { id: 'b', title: 'Other' }), 'a');
      await page.goto('/');
      await stripTab(page, LONG).hover();
      await expect(tooltip(page)).toHaveText(LONG);
      const box = (await tooltip(page).boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
      await expectAccessible(page);
    });
  }
});

test.describe('⌘O / Ctrl O on the new-tab field (TA-KEY-12)', () => {
  test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });
  const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
  const caption = (page: Page) => page.getByText('Type part of a file name.');

  test('the advertised chord does what the Open file… row does, with no engine call', async ({ page }) => {
    const calls: string[] = [];
    page.on('request', request => { if (request.url().includes('/api/engine/') && !/\/api\/engine\/(places|events|world|workspaces)\b/.test(request.url())) calls.push(request.url()); });
    await page.goto('/');
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    await expect(field(page)).toBeFocused();
    const hint = (await mac(page)) ? '⌘O' : 'Ctrl O';
    await expect(page.getByRole('option', { name: /^Open file…/ })).toContainText(hint);
    calls.length = 0;
    await expect(caption(page)).toHaveCount(0);
    await page.keyboard.press(`${await primary(page)}+o`);
    await expect(caption(page)).toBeVisible();
    await expect(field(page)).toBeFocused();
    // Same state as choosing the row with the pointer in a fresh field.
    await page.getByRole('button', { name: 'New tab', exact: true }).click();
    await expect(caption(page)).toHaveCount(0);
    await page.getByRole('option', { name: /^Open file…/ }).click();
    await expect(caption(page)).toBeVisible();
    expect(calls).toEqual([]);
    // First send, terminal and history keep working beside it: the field still starts a conversation on Enter.
    expect((await saved(page)).tabs.some((tab: { kind: string }) => tab.kind === 'newtab')).toBe(true);
  });

  test('from another tab the chord focuses the open New tab and asks for a file name, and a dialog keeps the chord', async ({ page }) => {
    await seed(page, tabsOf({ id: 'a', title: 'Conversation' }, { id: 'n', title: 'New tab', kind: 'newtab' }), 'a');
    await page.goto('/');
    await expect(stripTab(page, 'Conversation')).toHaveAttribute('aria-selected', 'true');
    // The seeded draft sits in the composer, and a writing field that already holds words keeps this chord.
    await stripTab(page, 'Conversation').press(`${await primary(page)}+o`);
    await expect(stripTab(page, 'New tab')).toHaveAttribute('aria-selected', 'true');
    await expect(caption(page)).toBeVisible();
    await expect(field(page)).toBeFocused();
    await expect(page.getByRole('tab')).toHaveCount(2);
    await page.getByRole('button', { name: 'All tabs', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).toBeVisible();
    await page.keyboard.press(`${await primary(page)}+o`);
    await expect(page.getByRole('dialog', { name: 'All tabs overview', exact: true })).toBeVisible();
    await expect(page.getByRole('tab')).toHaveCount(2);
  });

  test('a split focuses its New tab pane and asks for a file name', async ({ page }) => {
    const pane = (id: string, title: string, kind = 'conversation') => ({ id, title, draft: '', kind });
    const split = { id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', split: { layout: '1x2', focus: 0, panes: [pane('p1', 'Chat'), pane('p2', 'New tab', 'newtab')] } };
    await seed(page, [split], 'sp');
    await page.goto('/');
    await expect(field(page)).toBeVisible();
    await expect(caption(page)).toHaveCount(0);
    await page.keyboard.press(`${await primary(page)}+o`);
    await expect(caption(page)).toBeVisible();
    await expect(field(page)).toBeFocused();
    await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('group', { name: /Split/ })).toBeVisible();
  });
});

test.describe('the overview card (TA-OV-05, TA-OV-06)', () => {
  const overview = (page: Page) => page.getByRole('dialog', { name: 'All tabs overview', exact: true });
  const card = (page: Page, title: string) => overview(page).locator('.overview-card', { has: page.locator('.overview-card-title', { hasText: new RegExp(`^${title}$`) }) });
  const open = async (page: Page) => { await page.getByRole('button', { name: 'All tabs', exact: true }).click(); await expect(overview(page)).toBeVisible(); };

  test.describe('background press', () => {
    test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });
    const setup = async (page: Page) => {
      await seed(page, tabsOf({ id: 'a', title: 'Config stack' }, { id: 'b', title: 'Port fix' }, { id: 'c', title: 'Lexer' }), 'a');
      await page.goto('/');
      await open(page);
    };
    const untouched = async (page: Page) => {
      await expect(overview(page)).toBeVisible();
      await expect(stripTab(page, 'Config stack')).toHaveAttribute('aria-selected', 'true');
      const state = await saved(page);
      expect(state.activeId).toBe('a');
      expect(state.tabs.map((tab: { id: string }) => tab.id)).toEqual(['a', 'b', 'c']);
    };

    test('a plain click opens the tab and closes the overview', async ({ page }) => {
      await setup(page);
      await card(page, 'Port fix').getByRole('button', { name: 'Open Port fix' }).click();
      await expect(overview(page)).toBeHidden();
      await expect(stripTab(page, 'Port fix')).toHaveAttribute('aria-selected', 'true');
    });

    test('⌘/Ctrl-click keeps the foreground and the overlay and creates nothing', async ({ page }) => {
      await setup(page);
      await card(page, 'Port fix').getByRole('button', { name: 'Open Port fix' }).click({ modifiers: [(await primary(page)) as 'Meta' | 'Control'] });
      await untouched(page);
      await expect(card(page, 'Port fix')).toHaveAttribute('data-cursor', 'true');
    });

    test('middle-click does the same', async ({ page }) => {
      await setup(page);
      await card(page, 'Lexer').getByRole('button', { name: 'Open Lexer' }).click({ button: 'middle' });
      await untouched(page);
      await expect(card(page, 'Lexer')).toHaveAttribute('data-cursor', 'true');
    });

    test('middle background press protects the filter from PRIMARY paste and releases ordinary paste', async ({ page }) => {
      await setup(page);
      const filter = overview(page).getByRole('textbox', { name: 'Filter tabs' });
      await filter.fill('Lex');
      const protectedPaste = await card(page, 'Lexer').getByRole('button', { name: 'Open Lexer' }).evaluate(button => {
        button.dispatchEvent(new MouseEvent('auxclick', { button: 1, bubbles: true, cancelable: true }));
        const field = document.querySelector('[aria-label="Filter tabs"]')!;
        return !field.dispatchEvent(new Event('paste', { bubbles: true, cancelable: true }));
      });
      expect(protectedPaste).toBe(true);
      await expect(filter).toHaveValue('Lex');
      await expect(card(page, 'Lexer')).toHaveAttribute('data-cursor', 'true');
      await untouched(page);
      const ordinaryPaste = await filter.evaluate(async field => {
        await new Promise<void>(resolve => setTimeout(resolve, 0));
        return field.dispatchEvent(new Event('paste', { bubbles: true, cancelable: true }));
      });
      expect(ordinaryPaste).toBe(true);
    });

    test('in the filmstrip a modified press moves the cursor and does not open the centre card', async ({ page }) => {
      await setup(page);
      await overview(page).getByRole('radio', { name: 'Filmstrip' }).check();
      const centre = overview(page).locator('.overview-film-item[data-cursor="true"] .overview-film-open');
      await centre.click({ modifiers: [(await primary(page)) as 'Meta' | 'Control'] });
      await untouched(page);
      await centre.click({ button: 'middle' });
      await untouched(page);
      await centre.click();
      await expect(overview(page)).toBeHidden();
    });
  });

  test.describe('the card menu is the strip menu', () => {
    const menuLabels = (menu: Locator) => menu.locator(':scope > [role^="menuitem"] .menu-label').allTextContents();
    const stripMenu = async (page: Page, title: string) => {
      await stripTab(page, title).click({ button: 'right' });
      const menu = page.getByRole('menu', { name: `Actions for ${title}`, exact: true });
      await expect(menu).toBeVisible();
      const labels = await menuLabels(menu);
      await page.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);
      return labels;
    };
    const cardMenu = async (page: Page, title: string, first = false) => {
      await (first ? card(page, title).first() : card(page, title)).click({ button: 'right', position: { x: 40, y: 40 } });
      const menu = page.getByRole('menu', { name: `Actions for ${title}`, exact: true });
      await expect(menu).toBeVisible();
      return menu;
    };

    test('the same entries as the strip for an idle tab, a running tab, a pinned tab and a tab in a group', async ({ page }) => {
      await page.route('**/api/engine/**', route => route.abort());
      await seed(page, [...tabsOf({ id: 'a', title: 'Idle' }, { id: 'p', title: 'Pinned', pinned: true }, { id: 'r', title: 'Busy', running: true }), { ...make({ id: 'g1', title: 'Grouped' }), groupId: 'g' }], 'a');
      await page.addInitScript(() => {
        const state = JSON.parse(localStorage.getItem('codeaf.desktop.workspace.v1')!);
        if (!state.groups.length) { state.groups = [{ id: 'g', title: 'Trailing commas', collapsed: false }]; localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state)); }
      });
      await page.goto('/');
      const fromStrip: Record<string, string[]> = {};
      for (const title of ['Idle', 'Pinned', 'Busy', 'Grouped']) fromStrip[title] = await stripMenu(page, title);
      await open(page);
      for (const title of ['Idle', 'Pinned', 'Busy', 'Grouped']) {
        const menu = await cardMenu(page, title);
        expect(await menuLabels(menu), title).toEqual(fromStrip[title]);
        await page.keyboard.press('Escape');
        await expect(menu).toHaveCount(0);
      }
      // The old short overview menu is gone: Move to group, Create group.
      expect(fromStrip.Idle).toContain('Duplicate');
      expect(fromStrip.Idle).toContain('Rename tab');
      await expect(overview(page)).toBeVisible();
    });

    test('Pin, Duplicate and Add to group act on the strip from the overview', async ({ page }) => {
      await page.route('**/api/engine/**', route => route.abort());
      await seed(page, tabsOf({ id: 'a', title: 'One' }, { id: 'b', title: 'Two' }), 'a');
      await page.goto('/');
      await open(page);
      await (await cardMenu(page, 'Two')).getByRole('menuitem', { name: 'Pin tab' }).click();
      await expect.poll(async () => (await saved(page)).tabs.find((tab: { id: string }) => tab.id === 'b').pinned).toBe(true);
      await (await cardMenu(page, 'One')).getByRole('menuitem', { name: 'Duplicate' }).click();
      await expect(overview(page).locator('.overview-card')).toHaveCount(3);
      await expect.poll(async () => (await saved(page)).tabs.length).toBe(3);
      await (await cardMenu(page, 'One', true)).getByRole('menuitem', { name: 'Add to group' }).hover();
      await page.getByRole('menuitem', { name: 'New group…' }).click();
      await expect.poll(async () => (await saved(page)).groups.length).toBe(1);
      await expect(overview(page)).toBeVisible();
    });

    test('Close and stop that cannot stop shows the toast INSIDE the overview, with Try again and Undo', async ({ page }) => {
      const engine = await installMockEngine(page, { ...running, fail: { stop: 500 } });
      await seed(page, tabsOf({ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }), 'a');
      await page.goto('/');
      await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
      await open(page);
      await (await cardMenu(page, 'Config stack')).getByRole('menuitem', { name: /^Close and stop/ }).click();
      await expect.poll(() => stops(engine)).toBe(1);
      const toast = overview(page).locator('.toast');
      await expect(toast).toContainText('Could not stop Config stack. It is still running.');
      expect(await toast.getByRole('button').allTextContents()).toEqual(['Try again', 'Undo']);
      await toast.getByRole('button', { name: 'Try again' }).click();
      await expect.poll(() => stops(engine)).toBe(2);
      await toast.getByRole('button', { name: 'Undo' }).click();
      await expect(card(page, 'Config stack')).toBeVisible();
      await expect(toast).toHaveCount(0);
    });

    test('Close tab on a running tab keeps the work running and offers Stop it and Undo', async ({ page }) => {
      const engine = await installMockEngine(page, running);
      await seed(page, tabsOf({ id: 'a', title: 'Intro' }, { id: 'b', title: 'Config stack', running: true }), 'a');
      await page.goto('/');
      await expect.poll(() => engine.calls.some(call => call.path.endsWith('/sessions'))).toBe(true);
      await open(page);
      await (await cardMenu(page, 'Config stack')).getByRole('menuitem', { name: /^Close tab(?!s)/ }).click();
      const toast = overview(page).locator('.toast');
      await expect(toast).toContainText('Config stack');
      await expect(toast).toContainText('still running');
      expect(await toast.getByRole('button').allTextContents()).toEqual(['Stop it', 'Undo']);
      expect(stops(engine)).toBe(0);
      await toast.getByRole('button', { name: 'Stop it' }).click();
      await expect.poll(() => stops(engine)).toBe(1);
    });

    test('the Inbox card has no menu, exactly as the Inbox tab has none', async ({ page }) => {
      await page.route('**/api/engine/**', route => route.abort());
      await seed(page, [...tabsOf({ id: 'a', title: 'Intro' }), { id: 'inbox', kind: 'inbox', title: 'Inbox', draft: '', titleSource: 'manual', pinned: true }], 'a');
      await page.goto('/');
      await open(page);
      await overview(page).locator('.overview-card[data-kind="inbox"]').click({ button: 'right', position: { x: 40, y: 40 } });
      await expect(page.getByRole('menu')).toHaveCount(0);
    });

    for (const scheme of ['light', 'dark'] as const) for (const width of [320, 600]) {
      test(`the card menu is themed and reachable: ${scheme} at ${width}px`, async ({ page }) => {
        await page.route('**/api/engine/**', route => route.abort());
        await page.emulateMedia({ colorScheme: scheme });
        await page.setViewportSize({ width, height: 800 });
        await seed(page, tabsOf({ id: 'a', title: 'One' }, { id: 'b', title: 'Two' }), 'a');
        await page.goto('/');
        await page.getByRole('button', { name: 'All tabs', exact: true }).click();
        const menu = await cardMenu(page, 'Two');
        const box = (await menu.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width);
        await expectAccessible(page);
      });
    }
  });
});
