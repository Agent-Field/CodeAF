import { test, expect, type Page } from '@playwright/test';

async function mount(page: Page, finished: boolean, log = true) {
  await page.route('**/terminal-menu-contract', route => route.fulfill({ contentType: 'text/html', body: '<html><body><div id="root"></div></body></html>' }));
  await page.goto('/terminal-menu-contract');
  await page.evaluate(async ({ finished, log }) => {
    const refresh = '/@react-refresh';
    const { default: runtime } = await import(/* @vite-ignore */ refresh);
    runtime.injectIntoGlobalHook(window);
    Object.assign(window, { $RefreshReg$: () => {}, $RefreshSig$: () => (type: unknown) => type, __vite_plugin_react_preamble_installed__: true });
    const react = '/.vite-cache/deps/react.js';
    const dom = '/.vite-cache/deps/react-dom_client.js';
    const header = '/src/features/terminal/TerminalHeader.tsx';
    const theme = '/src/design/ThemeProvider.tsx';
    await import(/* @vite-ignore */ '/src/styles/tokens.css');
    await import(/* @vite-ignore */ '/src/styles/ui.css');
    await import(/* @vite-ignore */ '/src/App.css');
    await import(/* @vite-ignore */ '/src/features/terminal/terminal.css');
    const { default: React } = await import(/* @vite-ignore */ react);
    const { default: { createRoot } } = await import(/* @vite-ignore */ dom);
    const { TerminalHeader } = await import(/* @vite-ignore */ header);
    const { ThemeProvider } = await import(/* @vite-ignore */ theme);
    const record = (action: string) => { document.body.dataset.action = action; };
    createRoot(document.getElementById('root')).render(React.createElement(ThemeProvider, null, React.createElement(TerminalHeader, {
      title: 'nightly-bench', meta: '~/codeaf · job', words: '', canStop: !finished,
      finishedJob: finished, removeLabel: 'Remove job', readOutput: async () => 'output',
      onStop: () => record('stop'), onClose: () => record('close'), onRemove: () => record('remove'),
      onRerun: finished ? () => record('rerun') : undefined,
      onOpenLog: log ? () => record('log') : undefined,
    })));
  }, { finished, log });
  await page.getByRole('button', { name: 'More', exact: true }).click();
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`a finished job's menu reads Open log, Run again, Close tab, Remove job in order (${colorScheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    await mount(page, true);
    const menu = page.getByRole('menu');
    await expect(menu.locator('.menu-label')).toHaveText(['Open log', 'Run again', 'Close tab', 'Remove job']);
    await expect(menu.getByRole('separator')).toHaveCount(1);
    await expect(menu.locator('.menu-item')).toHaveCount(4);
    await expect(menu.locator('.menu-item').nth(1).locator('[data-icon="reload"]')).toBeVisible();
    await expect(menu.locator('.menu-item').first().locator('[data-icon="scrollText"]')).toBeVisible();
    await expect(menu.locator('.menu-item-danger')).toHaveText('Remove job');
    await menu.evaluate(async el => { await Promise.all(el.getAnimations({ subtree: true }).map(animation => animation.finished)); });
    expect(await menu.evaluate(el => el.getBoundingClientRect().width)).toBe(230);
    expect(await menu.locator('.menu-item').first().evaluate(el => ({ height: el.getBoundingClientRect().height, font: getComputedStyle(el).fontSize, gap: getComputedStyle(el).gap }))).toEqual({ height: 28, font: '13px', gap: '10px' });
    await menu.getByRole('menuitem', { name: /Close tab/ }).click();
    await expect(page.locator('body')).toHaveAttribute('data-action', 'close');
    await page.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Open log', exact: true }).click();
    await expect(page.locator('body')).toHaveAttribute('data-action', 'log');
  });
}

test("a running job's menu has no disabled items", async ({ page }) => {
  await mount(page, false);
  const menu = page.getByRole('menu');
  await expect(menu.locator('.menu-label')).toHaveText(['Copy output', 'Remove job']);
  await expect(menu.locator('[data-disabled], [aria-disabled="true"]')).toHaveCount(0);
  await expect(menu.getByRole('menuitem', { name: 'Stop', exact: true })).toHaveCount(0);
});

test('Open log is absent when no retained log opener exists', async ({ page }) => {
  await mount(page, true, false);
  await expect(page.getByRole('menu').locator('.menu-label')).toHaveText(['Run again', 'Close tab', 'Remove job']);
});
