import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

async function openJob(page: Page) {
  const engine = await installMockEngine(page, {
    ...plainReply(),
    terminals: [{ id: 'narrow-job', command: 'nightly-bench', title: 'nightly-bench', output: 'BenchmarkLoadAll-10\r\n' }],
  });
  await page.addInitScript(() => {
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [{ id: 'term-tab', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false }],
      groups: [], activeId: 'term-tab', closed: [], nextNumber: 2, recentIds: ['term-tab'],
    }));
    localStorage.setItem('codeaf.desktop.terminals.v1', JSON.stringify({
      'term-tab': { sessionFile: 'mock-session-1.jsonl', terminalId: 'narrow-job' },
    }));
  });
  await page.goto('/');
  await expect(page.locator('.terminal-state')).toHaveText(/^Running · \d+s$/);
  await expect(page.locator('.xterm-rows')).toContainText('BenchmarkLoadAll-10');
  return engine;
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`terminal tab at 320 and 600 px keeps state words and controls (${colorScheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    const engine = await openJob(page);
    const sizes = () => engine.calls.filter(call => call.path.endsWith('/resize')).map(call => call.body as { cols: number; rows: number });
    await expect.poll(() => sizes().length).toBeGreaterThan(0);
    let previous = sizes().at(-1)!.cols;
    let previousWidth = (await page.locator('.terminal-host').boundingBox())!.width;
    for (const width of [800, 601, 600, 480, 320]) {
      await page.setViewportSize({ width, height: 400 });
      const meta = page.locator('.terminal-meta');
      if (width <= 600) await expect(meta).toBeHidden();
      else await expect(meta).toBeVisible();
      await expect(page.locator('.terminal-title')).toHaveText('nightly-bench');
      await expect(page.locator('.terminal-state')).toBeVisible();
      for (const name of ['Stop', 'Copy output', 'More']) {
        const button = page.locator('.terminal-header').getByRole('button', { name, exact: true });
        await expect(button).toBeVisible();
        const bounds = (await button.boundingBox())!;
        expect(bounds.x).toBeGreaterThanOrEqual(0);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
      }
      const geometry = await page.locator('.terminal-pane').evaluate(pane => {
        const state = pane.querySelector('.terminal-state')!;
        const range = document.createRange(); range.selectNodeContents(state);
        const header = pane.querySelector('.terminal-header')!.getBoundingClientRect();
        const words = range.getBoundingClientRect();
        const ask = pane.querySelector('.terminal-ask')!;
        const field = pane.querySelector('.terminal-ask-field')!.getBoundingClientRect();
        const style = getComputedStyle(ask);
        return {
          stateRight: words.right, headerRight: header.right,
          askWidth: field.width,
          available: ask.getBoundingClientRect().width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
          pageWidth: document.documentElement.scrollWidth,
          titleWidth: pane.querySelector('.terminal-identity')!.getBoundingClientRect().width,
        };
      });
      expect(geometry.pageWidth).toBe(width);
      expect(geometry.titleWidth).toBeGreaterThan(0);
      expect(geometry.stateRight).toBeLessThanOrEqual(geometry.headerRight);
      if (width <= 600) expect(geometry.askWidth).toBeCloseTo(geometry.available, 1);
      // The navigation drawer can widen the pane while the window shrinks.
      const hostWidth = (await page.locator('.terminal-host').boundingBox())!.width;
      if (Math.abs(hostWidth - previousWidth) > 10) {
        if (hostWidth < previousWidth) await expect.poll(() => sizes().at(-1)!.cols).toBeLessThan(previous);
        else await expect.poll(() => sizes().at(-1)!.cols).toBeGreaterThan(previous);
      }
      previous = sizes().at(-1)!.cols;
      previousWidth = hostWidth;
    }
    await page.getByRole('textbox', { name: 'Ask codeaf about this output' }).fill('Why did this run?');
    await expect(page.getByRole('button', { name: 'Ask', exact: true })).toBeVisible();
    await page.locator('.terminal-header').getByRole('button', { name: 'More', exact: true }).click();
    await expect(page.getByRole('menuitem', { name: 'Copy output', exact: true })).toBeVisible();
  });
}
