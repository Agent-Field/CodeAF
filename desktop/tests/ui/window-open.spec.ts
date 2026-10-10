import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';

// Rust owns the native window. This browser contract proves its tab URL selects
// the saved tab without starting or stopping engine work during window boot.
for (const theme of ['light', 'dark']) {
  test(`a window URL focuses its saved tab · ${theme}`, async ({ page }) => {
    await page.addInitScript(theme => localStorage.setItem('codeaf-theme', theme), theme);
    const engine = await installMockEngine(page, { initial: { entries: [], title: '' } });
    await installMockPlaces(page);
    await page.route('**/api/engine/workspaces/**', route => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ key: 'now', revision: 1, workspace: {
        schema: 1,
        tabs: ['first', 'target'].map(id => ({ id, kind: 'conversation', title: id, draft: `draft-${id}`, pinned: false })),
        groups: [], closed: [], nextNumber: 3,
      } }),
    }));
    await page.goto('/?place=now&tab=target');
    await expect(page.getByRole('tab', { name: 'target', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('textbox', { name: /message/i })).toHaveValue('draft-target');
    expect(engine.calls.filter(call => call.method === 'POST')).toEqual([]);
  });
}
