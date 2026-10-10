import { test, expect } from '@playwright/test';
import { message } from './support/conversation';
import { dismissCoveringToasts, emitState, installNativeWebMock, nativeCalls } from './support/native-web-mock';

const url = 'https://go.dev/doc';

test('chat-plus opens a focused conversation with the page link attached and no send', async ({ page }) => {
  const turns: string[] = [];
  await page.route('**/api/engine/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() === 'POST' && path.includes('/turn')) turns.push(path);
    return route.abort();
  });
  await installNativeWebMock(page);
  const state = {
    tabs: [
      { id: 'chat1', title: 'Config stack', draft: '', pinned: false },
      { id: 'webpane1', title: 'pkg.go.dev', draft: '', pinned: false, kind: 'web', target: { url } },
      { id: 'later', title: 'Later notes', draft: '', pinned: false },
    ],
    groups: [], closed: [], activeId: 'webpane1', nextNumber: 4, recentIds: ['webpane1', 'chat1', 'later'],
  };
  await page.addInitScript(value => {
    if (!sessionStorage.getItem('seeded')) {
      localStorage.setItem('codeaf.desktop.workspace.v1', value);
      sessionStorage.setItem('seeded', '1');
    }
  }, JSON.stringify(state));
  await page.goto('/');
  await dismissCoveringToasts(page);
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
  await emitState(page, 'webpane1', { loading: false, title: 'json package' });
  await expect(page.locator('.workspace-tab[data-kind="web"]')).toContainText('json package');
  await page.getByRole('button', { name: 'Start a conversation with this page' }).click();

  await expect.poll(async () => page.locator('.workspace-tab').evaluateAll(nodes => nodes.map(node => `${node.getAttribute('data-kind')}:${node.getAttribute('data-active')}:${node.querySelector('.workspace-tab-title')?.textContent ?? ''}`))).toEqual([
    'conversation:false:Config stack',
    'web:false:json package',
    'conversation:true:json package',
    'conversation:false:Later notes',
  ]);
  const composer = page.locator('.composer');
  const link = composer.getByRole('link', { name: /json package/ });
  await expect(link).toHaveAttribute('href', url);
  await expect(message(page)).toHaveValue(`About this page, "json package": ${url}\n\n`);
  await expect(message(page)).not.toHaveValue(/<html|Package json implements/);
  await expect(page.locator('.user-message')).toHaveCount(0);
  expect(turns).toEqual([]);
});
