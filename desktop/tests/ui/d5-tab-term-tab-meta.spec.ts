import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// C-EDGE-5: a finished job's tab reads `exit N` after its name. A running job reads nothing (Shell 3j).
const SESSION = 'mock-session-1.jsonl';

async function openPair(page: Page) {
  await page.addInitScript((session: string) => {
    if (localStorage.getItem('codeaf.desktop.workspace.v1')) return;
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [
        { id: 'run-tab', kind: 'terminal', title: 'vite', draft: '', pinned: false },
        { id: 'done-tab', kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false },
      ],
      groups: [], activeId: 'run-tab', closed: [], nextNumber: 3, recentIds: ['run-tab', 'done-tab'],
    }));
    localStorage.setItem('codeaf.desktop.terminals.v1', JSON.stringify({
      'run-tab': { sessionFile: session, terminalId: 'run-1' },
      'done-tab': { sessionFile: session, terminalId: 'done-1' },
    }));
  }, SESSION);
  await page.goto('/');
}

async function metaStyle(tab: ReturnType<Page['getByRole']>) {
  return tab.evaluate(el => {
    const title = el.querySelector('.workspace-tab-title');
    const meta = el.querySelector('.workspace-tab-meta');
    if (!title || !meta) return null;
    const titleBox = title.getBoundingClientRect();
    const metaBox = meta.getBoundingClientRect();
    const button = el.getBoundingClientRect();
    const titleStyle = getComputedStyle(title);
    const metaStyle = getComputedStyle(meta);
    const probe = document.createElement('span');
    probe.style.color = 'var(--ink-3)';
    el.appendChild(probe);
    const ink3 = getComputedStyle(probe).color;
    probe.remove();
    const painted = titleStyle.maskImage;
    const mask = painted && painted !== 'none' ? painted : titleStyle.getPropertyValue('-webkit-mask-image');
    return {
      gap: metaBox.left - titleBox.right,
      metaRight: button.right - metaBox.right,
      fontSize: metaStyle.fontSize,
      color: metaStyle.color,
      ink3,
      mask,
      metaMask: metaStyle.maskImage || metaStyle.getPropertyValue('-webkit-mask-image'),
    };
  });
}

test('d5-tab-term-test: a finished job tab reads exit 0 after its name; a running one shows nothing', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await installMockEngine(page, {
    ...plainReply(),
    terminals: [
      { id: 'run-1', command: 'vite', title: 'vite', state: 'running', output: '' },
      { id: 'done-1', command: 'nightly-bench', title: 'nightly-bench', state: 'exited', exitCode: 0, endedAt: new Date(Date.now() - 120_000).toISOString(), output: 'ok\r\n' },
    ],
  });
  await openPair(page);
  const running = page.getByRole('tab', { name: 'vite', exact: true });
  const finished = page.getByRole('tab', { name: 'nightly-bench', exact: true });
  await expect(page.locator('.terminal-header .terminal-state')).toHaveText(/^Running/);
  await expect(running.locator('.workspace-tab-meta')).toHaveCount(0);
  await expect(running).not.toContainText('exit');

  await finished.click();
  await expect(page.locator('.terminal-header .terminal-state')).toHaveText(/^exit 0/);
  const meta = finished.locator('.workspace-tab-meta');
  await expect(finished.locator('.workspace-tab-title')).toHaveText('nightly-bench');
  await expect(meta).toHaveText('exit 0');
  await expect(running.locator('.workspace-tab-meta')).toHaveCount(0);

  const light = await metaStyle(finished);
  expect(light).not.toBeNull();
  expect(light!.fontSize).toBe('11px');
  expect(light!.color).toBe(light!.ink3);
  expect(Math.round(light!.gap)).toBe(8);
  expect(Math.abs(light!.metaRight)).toBeLessThan(1);
  expect(light!.mask).not.toBe('none');
  expect(light!.mask).toContain('linear-gradient');
  expect(light!.metaMask === 'none' || light!.metaMask === '').toBe(true);

  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', 'dark');
  const dark = await metaStyle(finished);
  expect(dark).not.toBeNull();
  expect(dark!.fontSize).toBe('11px');
  expect(dark!.color).toBe(dark!.ink3);
  expect(dark!.color).not.toBe(light!.color);
});
