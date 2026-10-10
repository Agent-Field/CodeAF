import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';

for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: clock receipt shows a check, picked label and actual wait; person receipt keeps you and time`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    const at = new Date(2026, 9, 9, 14, 2).toISOString();
    const recentOutcomes = [
      { kind: 'ask', token: '4', outcome: 'decided', words: 'Keep strict', by: 'dial', elapsedSeconds: 37.2, at, callId: 'ask-clock' },
      { kind: 'ask', token: '5', outcome: 'decided', words: 'Use Postgres', by: 'person', at, callId: 'ask-person' },
    ];
    await installMockEngine(page, {
      initial: { entries: [] },
      turns: [{
        entries: ['ask-clock', 'ask-person'].map(CallID => ({ Role: 'tool', Tool: 'ask', CallID, Text: '', Answered: true, Output: 'Answered' })),
        patch: { recentOutcomes } as never,
      }],
    });
    await openApp(page);
    await send(page, 'Choose the parser policy and database');
    const auto = page.locator('.receipt[data-state="answered"]').filter({ hasText: 'Keep strict' });
    await expect(auto.locator('.receipt-label')).toHaveText('Keep strict');
    await expect(auto.locator('.receipt-rest')).toHaveText('picked by codeaf after 37s');
    await expect(auto.locator('.app-icon[data-icon="check"]')).toHaveCount(1);
    await expect(auto.locator('.receipt-mark')).toHaveCSS('width', '12px');
    await expect(auto).toHaveCSS('font-size', '12px');
    const person = page.locator('.receipt[data-state="answered"]').filter({ hasText: 'Use Postgres' });
    await expect(person.locator('.receipt-rest')).toHaveText('· you · 14:02');
    const colors = await auto.evaluate(el => {
      const probe = document.createElement('span');
      el.append(probe);
      const read = (token: string) => { probe.style.color = `var(${token})`; return getComputedStyle(probe).color; };
      const result = [getComputedStyle(el.querySelector('.receipt-label')!).color === read('--ink-2'), getComputedStyle(el.querySelector('.receipt-rest')!).color === read('--ink-3')];
      probe.remove();
      return result;
    });
    expect(colors).toEqual([true, true]);
    await page.reload();
    await expect(auto.locator('.receipt-rest')).toHaveText('picked by codeaf after 37s');
    await expect(person.locator('.receipt-rest')).toHaveText('· you · 14:02');
  });
}
