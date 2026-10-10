import { test, expect } from '@playwright/test';

const page_html = (theme: string, body: string) => `
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
  window.log = [];
  ReactDOM.createRoot(document.getElementById('root')).render(${body});
  </script></body></html>`;

for (const theme of ['light', 'dark'] as const) {
  test(`header bar: padding, breathing dot, count buttons, rename · ${theme}`, async ({ page }) => {
    await page.route('**/bar-specimen', route => route.fulfill({ contentType: 'text/html', body: page_html(theme, `
      React.createElement(React.Fragment,null,
        React.createElement(ConversationBar,{lead:'title',title:'Old name',onRename:n=>window.log.push(n),onOpenFiltered:f=>window.log.push('open:'+f),counts:{running:2,needsYou:1},using:React.createElement('span',{className:'u'},'Using')}),
        React.createElement(ConversationBar,{lead:'title',title:'Plain',counts:{running:1,needsYou:0}}))`) }));
    await page.goto('/bar-specimen');
    const bars = page.locator('.conversation-bar');
    await expect(bars.first()).toHaveCSS('padding-left', '20px');
    await expect(bars.first()).toHaveCSS('padding-right', '10px');
    await expect(bars.first().locator('.cf-breathe')).toHaveCSS('animation-name', 'cf-breathe');
    // The title sits before the Using slot, and a bar without a handler draws spans, not buttons.
    expect(await bars.first().locator('.conversation-bar-title, .u').evaluateAll(n => n.map(e => e.className))).toEqual(['conversation-bar-title', 'u']);
    await expect(bars.nth(1).locator('button.conversation-bar-count')).toHaveCount(0);
    await bars.first().getByRole('button', { name: /running/ }).click();
    await bars.first().getByRole('button', { name: /need/ }).click();
    // Rename: Esc cancels, Enter commits, F2 starts it from the keyboard.
    const title = bars.first().locator('.conversation-bar-title');
    await title.dblclick();
    const field = bars.first().getByRole('textbox', { name: 'Rename conversation' });
    await expect(field).toHaveAttribute('maxlength', '80');
    await field.fill('Discarded'); await field.press('Escape');
    await expect(title).toHaveText('Old name');
    await title.focus(); await title.press('F2');
    await bars.first().getByRole('textbox').fill('New name'); await page.keyboard.press('Enter');
    expect(await page.evaluate(() => (window as any).log)).toEqual(['open:running', 'open:needs', 'New name']);
  });

  test(`header bar breath holds still under reduced motion · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.route('**/bar-specimen', route => route.fulfill({ contentType: 'text/html', body: page_html(theme, `
      React.createElement(ConversationBar,{lead:'title',title:'T',counts:{running:1,needsYou:0}})`) }));
    await page.goto('/bar-specimen');
    await expect(page.locator('.cf-breathe')).toHaveCSS('animation-name', 'none');
  });
}
