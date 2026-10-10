import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { NOW, withHistory } from './support/scenarios-history';
import type { MockConversation } from './support/history-engine';

/** Fourteen archived chats, one minute apart, so they share one day and a shift-click can cover all of them. */
function archivedChats(count: number): MockConversation[] {
  return Array.from({ length: count }, (_, index) => {
    const n = String(index + 1).padStart(2, '0');
    return {
      id: `chat-${n}`, title: `Chat ${n}`, at: new Date(NOW.getTime() - index * 60_000).toISOString(), archived: true,
      messages: [{ role: 'user', text: `Note ${n}` }],
      recap: { line: `Discussed chat ${n}`, discussed: '', decided: [], outcome: '', files: [] },
    };
  });
}

async function open(page: Page) {
  await page.clock.setFixedTime(NOW);
  const engine = await installMockEngine(page, withHistory(archivedChats(14)));
  await page.goto('/');
  await page.keyboard.press('Control+y');
  await expect(page.getByRole('heading', { name: 'History' })).toBeVisible();
  await expect(page.getByRole('option', { name: /Chat 01/ })).toBeVisible();
  return engine;
}

const option = (page: Page, title: string) => page.getByRole('option', { name: new RegExp(title) });

test('d5-tab-hist-test: Delete asks inline with the count, Undo restores within 10s', async ({ page }) => {
  test.setTimeout(45_000);
  const engine = await open(page);
  const list = page.getByRole('listbox', { name: 'Conversations' });

  await option(page, 'Chat 01').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  const one = page.getByRole('group', { name: 'Delete 1 chat?' });
  await expect(one).toBeVisible();
  await expect(option(page, 'Chat 01')).toHaveCount(0);
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await expect(one.getByRole('button', { name: 'Cancel' })).toHaveClass(/button-quiet/);
  await expect(one.getByRole('button', { name: 'Delete', exact: true })).toHaveClass(/button-danger/);

  const sibling = await option(page, 'Chat 02').boundingBox();
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(value => { document.documentElement.dataset.theme = value; }, theme);
    const measured = await one.evaluate(el => {
      const buttons = [...el.querySelectorAll('button')];
      const question = el.querySelector('.history-delete-question');
      if (!question || buttons.length < 2) return null;
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return {
        height: box.height, width: box.width, position: style.position,
        question: getComputedStyle(question).color,
        cancelBg: getComputedStyle(buttons[0]).backgroundColor,
        deleteBg: getComputedStyle(buttons[1]).backgroundColor,
        deleteColor: getComputedStyle(buttons[1]).color,
      };
    });
    expect(measured, theme).toBeTruthy();
    expect(measured!.position, theme).toBe('absolute');
    expect(measured!.height, theme).toBeCloseTo(sibling!.height, 0);
    expect(measured!.width, theme).toBeCloseTo(sibling!.width, 0);
    expect(measured!.deleteBg, theme).not.toBe(measured!.cancelBg);
    expect(measured!.deleteColor, theme).not.toBe(measured!.question);
  }

  await page.keyboard.press('Escape');
  await expect(one).toHaveCount(0);
  await expect(option(page, 'Chat 01')).toBeVisible();
  await expect(page.locator('dialog[open]')).toHaveCount(0);

  await option(page, 'Chat 01').click();
  await list.evaluate(node => { node.scrollTop = node.scrollHeight; });
  const last = option(page, 'Chat 14');
  await expect(last).toBeVisible();
  await last.click({ modifiers: ['Shift'] });
  await last.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  const many = page.getByRole('group', { name: 'Delete 14 chats?' });
  await expect(many).toBeVisible();
  await expect(option(page, 'Chat 01')).toHaveCount(0);
  await expect(option(page, 'Chat 02')).toBeVisible();
  await expect(page.locator('dialog[open]')).toHaveCount(0);
  await many.getByRole('button', { name: 'Delete', exact: true }).click();

  // The toast fades in. The clock is fixed, so the entrance only finishes when time moves.
  const toast = page.locator('.toast').filter({ hasText: '14 chats deleted' });
  await page.clock.fastForward(300);
  await expect(toast).toBeVisible();
  await expect(toast.getByRole('button', { name: 'Undo' })).toBeVisible();
  await page.clock.fastForward(9_000);
  await expect(toast).toBeVisible();
  await toast.getByRole('button', { name: 'Undo' }).click();
  await expect(toast).toHaveCount(0);
  await expect(option(page, 'Chat 01')).toBeVisible();
  await expect(option(page, 'Chat 14')).toBeVisible();
  expect(engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/history/restore'))).toHaveLength(1);
  expect(engine.calls.some(call => call.method === 'POST' && call.path.endsWith('/history/delete') && Array.isArray(call.body.ids) && call.body.ids.length === 14)).toBe(true);

  // The fourteen-row selection is still held. A plain click narrows it, so this Delete… asks about one chat.
  await option(page, 'Chat 01').click();
  await option(page, 'Chat 01').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  await page.getByRole('group', { name: 'Delete 1 chat?' }).getByRole('button', { name: 'Delete', exact: true }).click();
  const single = page.locator('.toast').filter({ hasText: '1 chat deleted' });
  await page.clock.fastForward(300);
  await expect(single).toBeVisible();
  await page.clock.fastForward(10_000);
  await expect(single).toHaveCount(0);
  await expect(option(page, 'Chat 01')).toHaveCount(0);
  await expect(option(page, 'Chat 02')).toBeVisible();
  expect(engine.calls.filter(call => call.path.endsWith('/history/restore'))).toHaveLength(1);
});
