import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

async function openFixture(page: Page, active: 'shell' | 'chat' = 'shell') {
  await installMockEngine(page, { ...plainReply(), terminals: [{ id: 'held-shell', title: 'Verification shell', output: 'Owned terminal fixture ready\r\n' }] });
  await page.addInitScript(activeId => {
    if (sessionStorage.getItem('drag-seeded')) return;
    sessionStorage.setItem('drag-seeded', '1');
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ schema: 1, tabs: [
      { id: 'chat', kind: 'conversation', title: 'Verification chat', titleSource: 'manual', sessionFile: 'mock-session-1.jsonl', draft: '', pinned: false },
      { id: 'shell', kind: 'terminal', title: 'Verification shell', titleSource: 'manual', sessionFile: 'mock-session-1.jsonl', target: { terminalId: 'held-shell' }, draft: '', pinned: false },
    ], groups: [], closed: [], activeId, nextNumber: 3, recentIds: [activeId] }));
  }, active);
  await page.goto('/');
  if (active === 'shell') await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('Owned terminal fixture ready');
}
async function startDrag(page: Page, title: string, x: number, y: number) {
  const source = (await page.getByRole('tab', { name: title, exact: true }).boundingBox())!;
  await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + source.width / 2 + 8, source.y + source.height / 2 + 24, { steps: 5 });
  await page.mouse.move(x, y, { steps: 30 });
  await page.mouse.move(x + 1, y);
}
for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: chat drag onto terminal edge remains droppable throughout highlighted half and persists`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openFixture(page);
    const area = (await page.locator('.workspace-conversation').boundingBox())!;
    await startDrag(page, 'Verification chat', area.x + area.width - 20, area.y + area.height / 2);
    await expect(page.locator('.split-zone-pill')).toHaveText('Split right');
    await page.mouse.move(area.x + area.width * .75, area.y + area.height / 2, { steps: 20 });
    await page.mouse.move(area.x + area.width * .75 + 1, area.y + area.height / 2);
    await expect(page.locator('.split-zone-pill')).toHaveText('Split right');
    if (process.env.SPLIT_DRAG_EVIDENCE_DIR) await page.screenshot({ path: `${process.env.SPLIT_DRAG_EVIDENCE_DIR}/terminal-hover-${scheme}-${test.info().project.name}.png` });
    await page.mouse.up();
    await expect(page.locator('.workspace-pane')).toHaveCount(2);
    await expect(page.locator('.pane-title')).toHaveText(['Verification shell', 'Verification chat']);
    await page.reload();
    await expect(page.locator('.workspace-pane')).toHaveCount(2);
    await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('Owned terminal fixture ready');
  });
  for (const edge of ['left', 'bottom'] as const) test(`${scheme}: terminal drops inside ${edge} preview and close persists`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openFixture(page, 'chat');
    const area = (await page.locator('.workspace-conversation').boundingBox())!;
    const left = edge === 'left';
    const label = left ? 'Split left' : 'Split down';
    await startDrag(page, 'Verification shell', area.x + (left ? 20 : area.width / 2), area.y + (left ? area.height / 2 : area.height - 20));
    await expect(page.locator('.split-zone-pill')).toHaveText(label);
    await page.mouse.move(area.x + area.width * (left ? .25 : .5), area.y + area.height * (left ? .5 : .75), { steps: 20 });
    await page.mouse.move(area.x + area.width * (left ? .25 : .5) + 1, area.y + area.height * (left ? .5 : .75));
    await expect(page.locator('.split-zone-pill')).toHaveText(label);
    await page.mouse.up();
    await expect(page.locator('.workspace-pane')).toHaveCount(2);
    await expect(page.locator('.pane-title')).toHaveText(left ? ['Verification shell', 'Verification chat'] : ['Verification chat', 'Verification shell']);
    await page.reload();
    await expect(page.locator('.terminal-pane .xterm-rows')).toContainText('Owned terminal fixture ready');
    await page.getByRole('button', { name: 'Close pane Verification shell', exact: true }).click();
    await expect(page.locator('.workspace-pane')).toHaveCount(1);
    await page.reload();
    await expect(page.locator('.workspace-pane')).toHaveCount(1);
  });
  test(`${scheme}: exiting preview cancels split and terminal reorder persists`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openFixture(page);
    const area = (await page.locator('.workspace-conversation').boundingBox())!;
    await startDrag(page, 'Verification chat', area.x + area.width - 20, area.y + area.height / 2);
    await expect(page.locator('.split-zone-pill')).toHaveText('Split right');
    await page.mouse.move(area.x + area.width * .25, area.y + area.height / 2, { steps: 20 });
    await page.mouse.move(area.x + area.width * .25 + 1, area.y + area.height / 2);
    await expect(page.locator('.split-zone-preview')).toHaveCount(0);
    await page.mouse.up();
    await expect(page.locator('.workspace-pane')).toHaveCount(1);
    const target = (await page.getByRole('tab', { name: 'Verification chat', exact: true }).boundingBox())!;
    await startDrag(page, 'Verification shell', target.x + 6, target.y + target.height / 2);
    await expect(page.locator('.workspace-tab[data-drop="before"]')).toHaveCount(1);
    await page.mouse.up();
    const order = () => page.evaluate(async () => (await (await fetch('/api/engine/workspaces/now')).json()).workspace.tabs.map((t: { id: string }) => t.id));
    await expect.poll(order).toEqual(['shell', 'chat']);
    await page.reload();
    await expect.poll(order).toEqual(['shell', 'chat']);
    await expect(page.locator('.workspace-pane')).toHaveCount(1);
  });
  test(`${scheme}: terminal dragged onto chat center does not silently split`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openFixture(page, 'chat');
    const area = (await page.locator('.workspace-conversation').boundingBox())!;
    await startDrag(page, 'Verification shell', area.x + area.width / 2, area.y + area.height / 2);
    await expect(page.locator('.split-zones')).toBeVisible();
    await expect(page.locator('.split-zone-preview')).toHaveCount(0);
    await page.mouse.up();
    await expect(page.locator('.workspace-pane')).toHaveCount(1);
    await expect(page.getByRole('tab', { name: 'Verification shell', exact: true })).toBeVisible();
  });
}
