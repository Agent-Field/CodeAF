import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send, expectNoHorizontalOverflow } from './support/conversation';
import { tokenColor } from './contracts';
import type { EngineQuestion } from '../../src/features/chat/engine-client';

const questions: EngineQuestion[] = [
  { id: 1, kind: 'choice', ask: 'choice', head: 'Choose storage', options: [{ key: 'one', label: 'Local' }] },
  { id: 2, kind: 'choice', ask: 'choice', head: 'Choose task scope', blocking: { tasks: ['task-1'] }, options: [{ key: 'one', label: 'Small' }] },
];

for (const theme of ['light', 'dark'] as const) {
  test(`local header excludes other chats and returns to the first question · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const engine = await installMockEngine(page, {
      initial: { entries: [] },
      turns: [{ entries: [{ Role: 'assistant', Text: 'Ready.', Answer: true }], patch: {
        title: 'Local chat', questions,
        tasks: [{ ID: 'task-1', Title: 'Local task', Status: 'paused', Waits: [], Seat: 'worker', Steps: 0 }],
      } }],
      world: { rows: [{ chatId: 'elsewhere', sessionFile: '/elsewhere/session.jsonl', title: 'Other chat', running: false, needsYou: 19, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false }], items: [] },
    });
    await openApp(page); await send(page, 'Start');
    const tray = page.getByRole('region', { name: 'Waiting on you' });
    const count = page.locator('.conversation-bar-counts').getByRole('button', { name: '2 need you here', exact: true });
    await expect(count).toBeVisible();
    // I-ICV-34: the count opens the expanded Tasks view filtered to Needs you.
    await count.click();
    await expect(page.locator('.tasks-table-tab[data-filter="needs"]')).toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('button', { name: 'Back to the conversation' }).click();
    await expect(page.locator('.expanded-tasks')).toHaveCount(0);
    engine.update({ questions: [questions[0]] });
    await expect(page.locator('.conversation-bar-counts').getByRole('button', { name: '1 needs you here', exact: true })).toBeVisible();
    await page.setViewportSize({ width: 320, height: 568 });
    await expect(page.getByRole('button', { name: '1 needs you here', exact: true })).toBeVisible();
    await expectNoHorizontalOverflow(page);
    engine.update({ questions: [] });
    await expect(page.locator('.conversation-bar-counts .conversation-bar-count')).toHaveCount(0);
  });

  test(`walking chip uses measured design geometry and hides without progress · ${theme}`, async ({ page }) => {
    await page.route('**/header-specimen', route => route.fulfill({ contentType: 'text/html', body: `
      <html data-theme="${theme}"><body><div id="root"></div><script type="module">
      import RefreshRuntime from '/@react-refresh';
      RefreshRuntime.injectIntoGlobalHook(window);
      window.$RefreshReg$ = () => {};
      window.$RefreshSig$ = () => type => type;
      window.__vite_plugin_react_preamble_installed__ = true;
      </script><script type="module">
      import React from '/.vite-cache/deps/react.js';
      import ReactDOM from '/.vite-cache/deps/react-dom_client.js';
      import {ConversationBar} from '/src/features/conversation/ConversationBar.tsx';
      import '/src/styles/tokens.css';
      ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(React.Fragment,null,
        React.createElement(ConversationBar,{lead:'title',counts:{running:0,needsYou:0},nextUp:{index:1,total:5}},'Chat title'),
        React.createElement(ConversationBar,{lead:'title',counts:{running:0,needsYou:0}},'Quiet chat'),
        React.createElement(ConversationBar,{lead:'title',counts:{running:0,needsYou:0},nextUp:{index:0,total:5}},'Unknown progress')));
      </script></body></html>` }));
    await page.goto('/header-specimen');
    const chip = page.locator('.next-up-chip');
    await expect(chip).toHaveCount(1);
    await expect(chip).toHaveText('Next up1 of 5');
    await expect(chip).toHaveCSS('height', '24px');
    await expect(chip).toHaveCSS('border-radius', '99px');
    await expect(chip).toHaveCSS('padding-left', '10px');
    await expect(chip).toHaveCSS('gap', '8px');
    await expect(chip).toHaveCSS('font-size', '12px');
    await expect(chip).toHaveCSS('background-color', await tokenColor(page, 'field'));
    await expect(chip).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    await expect(chip.locator('.next-up-chip-progress')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
    expect((await chip.boundingBox())!.height).toBe(24);
  });
}
