import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectThemedSurface, menuSurface, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

const sessionFile = 'mock-session-1.jsonl';
const header = (page: Page) => page.locator('.terminal-header');
const terminalCalls = (engine: Awaited<ReturnType<typeof installMockEngine>>, action: string) =>
  engine.calls.filter(call => call.method === 'POST' && call.path.endsWith(action));

// The real terminal routes keep the record and replay its output; no renderer handler is replaced.
async function openJob(page: Page, finished = true) {
  const engine = await installMockEngine(page, {
    ...plainReply(),
    terminals: [{ id: 'bench', command: 'nightly-bench', title: 'nightly-bench',
      state: finished ? 'exited' : 'running', ...(finished ? { exitCode: 0, endedAt: '2026-10-10T12:00:00Z' } : {}),
      output: 'benchmark complete\r\n' }],
  });
  await page.addInitScript(file => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    const tab = { id: 'bench-tab', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false,
      sessionFile: file, target: { terminalId: 'bench' } };
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [tab], groups: [],
      activeId: tab.id, closed: [], nextNumber: 2, recentIds: [tab.id] }));
  }, sessionFile);
  await page.goto('/');
  await expect(header(page).locator('.terminal-state')).toHaveText(finished ? /^exit 0/ : /^Running/);
  await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('benchmark complete');
  return engine;
}

async function headerMenu(page: Page) {
  await header(page).getByRole('button', { name: 'More', exact: true }).click();
  const menu = page.getByRole('menu', { name: 'More' });
  await expect(menu).toBeVisible();
  return menu;
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.addInitScript(() => localStorage.removeItem('codeaf-theme'));
    });

    test('TT-09: running More offers Copy output and Remove, with Stop only in the header', async ({ page }) => {
      const engine = await openJob(page, false);
      await expect(header(page).getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
      const menu = await headerMenu(page);
      await expect(menu.getByRole('menuitem')).toHaveText(['Copy output', 'Remove job']);
      await expect(menu.getByRole('menuitem', { name: 'Stop', exact: true })).toHaveCount(0);
      await expect(menu.getByRole('menuitem', { name: 'Run again' })).toHaveCount(0);
      await page.keyboard.press('Escape');
      await expect(header(page).getByRole('button', { name: 'More', exact: true })).toBeFocused();
      expect(terminalCalls(engine, '/close')).toHaveLength(0);
      expect(terminalCalls(engine, '/remove')).toHaveLength(0);
    });

    test('TT-10: finished More orders the backed actions and Run again starts the same command once', async ({ page }) => {
      const engine = await openJob(page);
      await expect(header(page).getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
      const menu = await headerMenu(page);
      // TERM-220 records the pending log-file dependency; scrollback alone cannot open a file.
      await expect(menu.getByRole('menuitem', { name: 'Open log', exact: true })).toHaveCount(0);
      await expect(menu.getByRole('menuitem')).toHaveText([ 'Run again', /Close tab/, 'Remove job' ]);
      await expect(menu.getByRole('menuitem', { name: 'Run again' }).locator('[data-icon="reload"]')).toBeVisible();
      await expect(menu.getByRole('menuitem', { name: /Close tab/ }).locator('kbd')).not.toBeEmpty();
      await menu.getByRole('menuitem', { name: 'Run again', exact: true }).click();
      await expect(header(page).locator('.terminal-state')).toHaveText(/^Running/);
      await expect(page.getByRole('tab')).toHaveCount(2);
      expect(terminalCalls(engine, '/terminals')).toHaveLength(1);
      expect(terminalCalls(engine, '/terminals')[0].body).toMatchObject({ command: 'nightly-bench', title: 'nightly-bench' });
      expect(terminalCalls(engine, '/remove')).toHaveLength(0);
    });

    test('TT-11: finished tab right-click shares Run again and Remove; Close keeps the engine record', async ({ page }) => {
      const engine = await openJob(page);
      const tab = page.getByRole('tab', { name: 'nightly-bench', exact: true });
      await tab.click({ button: 'right' });
      const menu = page.getByRole('menu');
      await expect(menu.getByRole('menuitem', { name: 'Run again', exact: true })).toBeVisible();
      await expect(menu.getByRole('menuitem', { name: 'Remove job', exact: true })).toBeVisible();
      await expect(menu.getByRole('menuitem', { name: 'Open log', exact: true })).toHaveCount(0);
      const labels = await menu.getByRole('menuitem').allTextContents();
      const close = labels.findIndex(label => label.startsWith('Close tab'));
      expect(labels.indexOf('Run again')).toBeLessThan(close);
      expect(labels[close + 1]).toBe('Remove job');
      await menu.getByRole('menuitem', { name: /^Close tab(?:\s|$)/ }).click();
      await expect(tab).toHaveCount(0);
      expect(terminalCalls(engine, '/close')).toHaveLength(0);
      expect(terminalCalls(engine, '/remove')).toHaveLength(0);
      await page.getByRole('tab').first().press(`${await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control')}+Shift+t`);
      await expect(tab).toHaveAttribute('aria-selected', 'true');
      await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('benchmark complete');
      expect(terminalCalls(engine, '/terminals')).toHaveLength(0);
    });

    test('TT-10/TT-11: Remove job deletes the engine record and closes its tab', async ({ page }) => {
      const engine = await openJob(page);
      await page.getByRole('tab', { name: 'nightly-bench', exact: true }).click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Remove job', exact: true }).click();
      await expect(page.getByRole('tab', { name: 'nightly-bench', exact: true })).toHaveCount(0);
      expect(terminalCalls(engine, '/remove')).toHaveLength(1);
      expect(terminalCalls(engine, '/remove')[0].path).toContain('/terminals/bench/remove');
      expect(terminalCalls(engine, '/close')).toHaveLength(0);
    });

    test('TT-12: finished exit metadata is 11px ink-3 and running metadata stays absent', async ({ page }) => {
      await openJob(page);
      const tab = page.getByRole('tab', { name: 'nightly-bench', exact: true });
      await tab.hover();
      const meta = tab.locator('.workspace-tab-meta');
      await expect(meta).toHaveText('exit 0');
      await expect(meta).toHaveCSS('font-size', '11px');
      await expect(meta).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(tab.locator('.workspace-tab-title')).toHaveCSS('mask-image', /linear-gradient/);
      await (await headerMenu(page)).getByRole('menuitem', { name: 'Run again', exact: true }).click();
      const running = page.getByRole('tab', { selected: true });
      await expect(header(page).locator('.terminal-state')).toHaveText(/^Running/);
      await expect(running.locator('.workspace-tab-meta')).toHaveCount(0);
    });

    test('TT-18: at 600px and 320px meta hides while state, actions and full-width Ask remain reachable', async ({ page }) => {
      await openJob(page, false);
      await expect(header(page).locator('.terminal-meta')).toBeVisible();
      for (const width of [600, 320]) {
        await page.setViewportSize({ width, height: 560 });
        await expect(header(page).locator('.terminal-meta')).toBeHidden();
        await expect(header(page).locator('.terminal-state')).toBeVisible();
        for (const name of ['Stop', 'Copy output', 'More']) {
          const button = header(page).getByRole('button', { name, exact: true });
          await expect(button).toBeVisible();
          const box = (await button.boundingBox())!;
          expect(box.x).toBeGreaterThanOrEqual(0);
          expect(box.x + box.width).toBeLessThanOrEqual(width);
        }
        const geometry = await page.locator('.terminal-ask-field').evaluate(el => {
          const parent = el.parentElement!;
          const padding = getComputedStyle(parent);
          return { width: el.getBoundingClientRect().width,
            available: parent.getBoundingClientRect().width - parseFloat(padding.paddingLeft) - parseFloat(padding.paddingRight),
            overflow: document.documentElement.scrollWidth > innerWidth };
        });
        expect(Math.abs(geometry.width - geometry.available)).toBeLessThan(1);
        expect(geometry.overflow).toBe(false);
        await expect(page.getByRole('textbox', { name: 'Ask codeaf about this output' })).toBeVisible();
      }
    });

    test('TN-03: Start rows show platform chords, Open file focuses the field and New terminal starts one shell', async ({ page }) => {
      const engine = await installMockEngine(page, plainReply());
      await page.goto('/');
      await page.getByRole('button', { name: 'New tab', exact: true }).click();
      const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
      await expect(page.getByRole('option', { name: /^New terminal/ })).toContainText(mac ? '⌃`' : 'Ctrl `');
      await expect(page.getByRole('option', { name: /^Open file…/ })).toContainText(mac ? '⌘O' : 'Ctrl O');
      await page.getByRole('option', { name: /^Open file…/ }).click();
      await expect(page.getByRole('combobox', { name: 'Search or start' })).toBeFocused();
      await expect(page.getByText('Type part of a file name.', { exact: true })).toBeVisible();
      expect(terminalCalls(engine, '/terminals')).toHaveLength(0);
      await page.getByRole('option', { name: /^New terminal/ }).click();
      await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('mock$');
      expect(terminalCalls(engine, '/terminals')).toHaveLength(1);
      expect(terminalCalls(engine, '/terminals')[0].body).not.toHaveProperty('command');
    });

    test('TT-09/TT-10/TT-11: open running, finished and tab menus meet the themed axe contract', async ({ page }) => {
      // Three complete axe scans need room on the shared laptop without weakening any rule.
      test.setTimeout(60_000);
      await openJob(page, false);
      await expectThemedSurface(page, await headerMenu(page), menuSurface);
      await expectAccessible(page);
      await page.keyboard.press('Escape');
      await header(page).getByRole('button', { name: 'Stop', exact: true }).click();
      await expect(header(page).locator('.terminal-state')).toHaveText(/^closed/);
      await expectThemedSurface(page, await headerMenu(page), menuSurface);
      await expectAccessible(page);
      await page.keyboard.press('Escape');
      await page.getByRole('tab', { name: 'nightly-bench', exact: true }).click({ button: 'right' });
      await expectThemedSurface(page, page.getByRole('menu'), menuSurface);
      await expectAccessible(page);
    });
  });
}
