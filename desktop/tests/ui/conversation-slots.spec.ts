import { test, expect } from '@playwright/test';

const pageHtml = (theme: string, body: string) => `
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
  import {UsingChip} from '/src/features/places/using/UsingChip.tsx';
  import {blockRenderer} from '/src/features/conversation/blockRenderer.tsx';
  import {ThemeProvider} from '/src/design/ThemeProvider.tsx';
  import '/src/styles/tokens.css';
  localStorage.setItem('codeaf-theme', '${theme}');
  const chip = React.createElement(UsingChip, { text: 'Using 3 places · 4 sources', tone: 'quiet', open: false, onToggle() {} });
  const turn = { id: 't', user: 'hi', attachments: [], steer: [], state: 'done', digest: '', blocks: [
    { kind: 'placeChange', id: 'p', text: 'Now also using Release: brand-voice.md', undoReceipts: ['rc_1'], placeName: 'Release' },
    { kind: 'remember-line', id: 'r', text: 'Remembered the rule', undoReceipts: ['u1'] },
  ]};
  const draw = blockRenderer({ tasks: [], open: {}, onToggle() {}, onOpenTask() {}, onFocusQuestion() {} })(turn);
  ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(ThemeProvider, null, ${body}));
  </script></body></html>`;

for (const theme of ['light', 'dark'] as const) {
  test(`Using chip sits left of the counts · ${theme}`, async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    await page.route('**/slots-specimen', route => route.fulfill({ contentType: 'text/html', body: pageHtml(theme, `
      React.createElement(React.Fragment, null,
        React.createElement(ConversationBar, { lead: 'title', title: 'Release notes', counts: { running: 4, needsYou: 5 }, using: chip }),
        React.createElement(ConversationBar, { lead: 'trail', counts: { running: 1, needsYou: 0 }, using: chip }, React.createElement('span', null, 'Trail')))`) }));
    await page.goto('/slots-specimen');
    const bars = page.locator('.conversation-bar');
    const boxes = await bars.first().evaluate(bar => {
      const box = (selector: string) => {
        const node = bar.querySelector(selector);
        if (!node) return null;
        const rect = node.getBoundingClientRect();
        return { x: rect.x, right: rect.right };
      };
      return { title: box('.conversation-bar-title'), chip: box('.using-chip'), counts: box('.conversation-bar-counts') };
    });
    expect(boxes.title && boxes.chip && boxes.counts).toBeTruthy();
    const titleGap = boxes.chip!.x - boxes.title!.right;
    const countGap = boxes.counts!.x - boxes.chip!.right;
    expect(titleGap).toBeGreaterThanOrEqual(-1);
    expect(titleGap).toBeLessThanOrEqual(24);
    expect(countGap).toBeGreaterThan(titleGap);
    await expect(bars.nth(1).locator('.using-chip')).toHaveCount(0);
  });

  test(`a placeChange line is the membership note · ${theme}`, async ({ page }) => {
    await page.route('**/slots-specimen', route => route.fulfill({ contentType: 'text/html', body: pageHtml(theme, `
      React.createElement('div', null, draw(turn.blocks[0]), draw(turn.blocks[1]))`) }));
    await page.goto('/slots-specimen');
    const note = page.locator('.membership-note');
    await expect(note).toHaveCount(1);
    await expect(note).toHaveText('Now also using Release: brand-voice.md');
    await expect(note.locator('.membership-note-place')).toHaveText('Release');
    await expect(page.getByText('Remembered the rule')).toBeVisible();
    await expect(page.locator('.system-note, .note-item').filter({ hasText: 'Now also using' })).toHaveCount(0);
  });
}
