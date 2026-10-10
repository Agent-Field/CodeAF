import { test, expect, type Page } from '@playwright/test';

// SH-105/106: a group label drags the whole group, and Alt+Shift+←/→ moves it one slot and announces it.
const KEY = 'codeaf.desktop.workspace.v1';
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });
const tabs = [tab('a', 'Alpha'), tab('g1', 'One', { groupId: 'G' }), tab('g2', 'Two', { groupId: 'G' }), tab('b', 'Beta'), tab('c', 'Gamma')];
async function open(page: Page) {
 const state = { tabs, groups: [{ id: 'G', title: 'Release', collapsed: false }], activeId: 'a', closed: [], nextNumber: 6, recentIds: tabs.map(t => t.id) };
 await page.route('**/api/engine/**', route => route.abort());
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
 await page.goto('/');
}
const order = (page: Page) => page.locator('.workspace-tabstrip').evaluate(strip => Array.from(strip.querySelectorAll('[role="tab"]')).map(el => el.getAttribute('aria-label')));
const label = (page: Page) => page.locator('.workspace-group-label');

test('dragging the group label onto the right half of a tab moves the whole group, members in order', async ({ page }) => {
 await open(page);
 const dt = await page.evaluateHandle(() => new DataTransfer());
 const target = page.getByRole('tab', { name: 'Beta', exact: true });
 const box = (await target.boundingBox())!;
 await label(page).dispatchEvent('dragstart', { dataTransfer: dt });
 await target.dispatchEvent('dragover', { dataTransfer: dt, clientX: box.x + box.width * 0.9, clientY: box.y + box.height / 2 });
 await target.dispatchEvent('drop', { dataTransfer: dt, clientX: box.x + box.width * 0.9, clientY: box.y + box.height / 2 });
 expect(await order(page)).toEqual(['Alpha', 'Beta', 'One', 'Two', 'Gamma']);
});

test('Alt+Shift+→ on the label moves the group one slot and announces it; ← moves it back', async ({ page }) => {
 await open(page);
 await label(page).focus();
 await page.keyboard.press('Alt+Shift+ArrowRight');
 expect(await order(page)).toEqual(['Alpha', 'Beta', 'One', 'Two', 'Gamma']);
 await expect(page.locator('.workspace-strip-note')).toHaveText('Moved group Release to position 3 of 4');
 await label(page).focus();
 await page.keyboard.press('Alt+Shift+ArrowLeft');
 expect(await order(page)).toEqual(['Alpha', 'One', 'Two', 'Beta', 'Gamma']);
});
