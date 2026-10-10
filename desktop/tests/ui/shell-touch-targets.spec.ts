import { test, expect, type Browser, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Coarse pointer (SH-042, SH-087, SH-136, SH-212). Sizes are the touch tokens. A hold opens the menu
// the control already has; a mouse hold does not, so dragging a tab stays a drag.
const delay = design.interaction.longPressDelay;
const close = Number.parseFloat(design.foundation['hit-coarse-close']);
const row = Number.parseFloat(design.foundation['hit-coarse-row']);
const divider = Number.parseFloat(design.foundation['pane-resize-hit-coarse']);

const port = process.env.CODEAF_UI_PORT ?? '1422';

async function coarsePage(browser: Browser, scheme: 'light' | 'dark') {
  const context = await browser.newContext({
    hasTouch: true,
    isMobile: true,
    viewport: { width: 1200, height: 800 },
    baseURL: `http://127.0.0.1:${port}`,
    colorScheme: scheme,
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  await page.route('**/api/engine/**', route => route.abort());
  return { context, page };
}

function seed(page: Page, tabs: unknown[], activeId: string, groups: { id: string; title: string }[] = []) {
  const state = { tabs, groups: groups.map(group => ({ ...group, collapsed: false })), closed: [], activeId, nextNumber: tabs.length + 1, recentIds: tabs.map(tab => (tab as { id: string }).id) };
  return page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}

const plain = (id: string, title: string, groupId?: string) => ({ id, title, draft: '', pinned: false, titleSource: 'manual', kind: 'conversation', groupId });
const pane = (id: string, title: string) => ({ id, title, draft: '', kind: 'conversation' });

async function requireCoarse(page: Page) {
  expect(await page.evaluate(() => matchMedia('(pointer: coarse)').matches), 'this browser did not emulate a coarse pointer').toBe(true);
}

async function hold(page: Page, locator: ReturnType<Page['locator']>) {
  const box = (await locator.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
}

for (const scheme of ['light', 'dark'] as const) {
  test(`coarse pointer grows the active close, rail rows, and split divider (${scheme})`, async ({ browser }) => {
    const { context, page } = await coarsePage(browser, scheme);
    try {
      const split = {
        id: 'sp', title: 'Split', draft: '', pinned: false, kind: 'conversation', titleSource: 'manual',
        split: { layout: '2x2', focus: 0, panes: [pane('p1', 'Left'), pane('p2', 'Right'), pane('p3', 'Lower'), pane('p4', 'Last')] },
      };
      await seed(page, [split, plain('b', 'Notes')], 'sp');
      await page.goto('/');
      await requireCoarse(page);
      const activeClose = page.locator('.workspace-split-tab[data-active="true"] .workspace-tab-close');
      await expect(activeClose).toBeVisible();
      const closeBox = await activeClose.evaluate(el => {
        const rect = el.getBoundingClientRect();
        return { width: rect.width, height: rect.height, opacity: getComputedStyle(el).opacity };
      });
      expect(closeBox.opacity).toBe('1');
      expect(closeBox.width).toBeCloseTo(close, 0);
      expect(closeBox.height).toBeCloseTo(close, 0);

      const now = page.locator('.sidebar.rail').getByRole('button', { name: 'Now', exact: true });
      await expect(now).toBeVisible();
      expect((await now.boundingBox())!.height).toBeCloseTo(row, 0);

      const col = page.locator('.pane-resize[data-axis="col"]');
      const horizontal = page.locator('.pane-resize[data-axis="row"]');
      await expect(col).toBeVisible();
      await expect(horizontal).toBeVisible();
      const colBox = (await col.boundingBox())!;
      const rowBox = (await horizontal.boundingBox())!;
      expect(colBox.width).toBeCloseTo(divider, 0);
      expect(rowBox.height).toBeCloseTo(divider, 0);
      const left = (await page.locator('.workspace-pane').nth(0).boundingBox())!;
      const right = (await page.locator('.workspace-pane').nth(1).boundingBox())!;
      const lower = (await page.locator('.workspace-pane').nth(2).boundingBox())!;
      expect(Math.abs((colBox.x + colBox.width / 2) - ((left.x + left.width + right.x) / 2))).toBeLessThan(1);
      expect(Math.abs((rowBox.y + rowBox.height / 2) - ((left.y + left.height + lower.y) / 2))).toBeLessThan(1);
    } finally {
      await context.close();
    }
  });
}

test('a coarse hold opens the tab and group menus and a place row menu, and a short tap does not', async ({ browser }) => {
  const { context, page } = await coarsePage(browser, 'light');
  try {
    await seed(page, [plain('a', 'Notes'), plain('b', 'Other'), plain('c', 'Config stack', 'g'), plain('d', 'Fixtures', 'g')], 'a', [{ id: 'g', title: 'Trailing commas' }]);
    await page.goto('/');
    await requireCoarse(page);
    await expect(page.getByRole('tab', { name: 'Other', exact: true })).toBeVisible();

    const other = page.getByRole('tab', { name: 'Other', exact: true });
    await other.click();
    await expect(other).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('tab', { name: 'Notes', exact: true }).click();

    const menu = page.getByRole('menu', { name: 'Actions for Other', exact: true });
    await hold(page, other);
    await page.waitForTimeout(delay / 2);
    await expect(menu).toHaveCount(0);
    await expect(menu).toBeVisible();
    await page.mouse.up();
    // The open menu marks the page inert, so the tab is checked after the menu goes.
    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
    await expect(other).toHaveAttribute('aria-selected', 'false');
    await expect(page.getByRole('tab', { name: 'Notes', exact: true })).toHaveAttribute('aria-selected', 'true');

    const label = page.locator('.workspace-group-label');
    const groupMenu = page.getByRole('menu', { name: 'Actions for group Trailing commas', exact: true });
    await expect(label).toHaveAttribute('aria-expanded', 'true');
    await hold(page, label);
    await expect(groupMenu).toBeVisible();
    await page.mouse.up();
    await page.keyboard.press('Escape');
    await expect(label).toHaveAttribute('aria-expanded', 'true');

    await page.locator('.place-rail').evaluate(rail => {
      const row = document.createElement('div');
      row.className = 'rail-row';
      row.dataset.touchProbe = 'rail';
      row.textContent = 'Probe place';
      row.addEventListener('contextmenu', event => {
        event.preventDefault();
        const bottom = Math.round(row.getBoundingClientRect().bottom);
        row.dataset.opened = `${event.button}:${Math.round(event.clientY)}:${bottom}`;
      });
      rail.append(row);
    });
    const probe = page.locator('[data-touch-probe="rail"]');
    await hold(page, probe);
    await expect(probe).toHaveAttribute('data-opened', /\d+/);
    const opened = (await probe.getAttribute('data-opened'))!;
    const [button, clientY, bottom] = opened.split(':').map(Number);
    expect(button).toBe(2);
    expect(clientY).toBe(bottom);
    await page.mouse.up();

    const notes = page.locator('.workspace-tab', { has: page.getByRole('tab', { name: 'Notes', exact: true }) });
    const notesMenu = page.getByRole('menu', { name: 'Actions for Notes', exact: true });
    await hold(page, notes.locator('.workspace-tab-close'));
    await page.waitForTimeout(delay + 100);
    await expect(notesMenu).toHaveCount(0);
    await page.mouse.up();
  } finally {
    await context.close();
  }
});

test('a mouse hold does not open the tab menu', async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  expect(await page.evaluate(() => matchMedia('(pointer: coarse)').matches)).toBe(false);
  await seed(page, [plain('a', 'Notes'), plain('b', 'Other')], 'a');
  await page.goto('/');
  const other = page.getByRole('tab', { name: 'Other', exact: true });
  await expect(other).toBeVisible();
  await hold(page, other);
  await page.waitForTimeout(delay + 100);
  await expect(page.getByRole('menu', { name: 'Actions for Other', exact: true })).toHaveCount(0);
  await page.mouse.up();
});
