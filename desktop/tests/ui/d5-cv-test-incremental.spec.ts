import { test, expect } from '@playwright/test';
import { installMockEngine, OUTPUT_CAP_BYTES, type Scenario } from './support/mock-engine';
import { openApp, send } from './support/conversation';

// An output over the engine's cap arrives omitted; the row fetches it only when expanded (CV-300).
const BIG = Array.from({ length: 2000 }, (_, i) => `match ${i + 1}: needle in a haystack`).join('\n');
const scenario: Scenario = {
  initial: { title: 'Search', entries: [] },
  turns: [{ entries: [
    { Role: 'assistant', Text: 'Searching.', Answer: false },
    { Role: 'tool', Text: '', Tool: 'probe', Hint: 'probe', CallID: 'g1', Answered: true, Args: JSON.stringify({ pattern: 'needle' }), Output: BIG },
    { Role: 'assistant', Text: 'Found two thousand matches.', Answer: true },
  ] }],
  tools: { g1: { output: BIG, full: true } },
};

test('an omitted tool output is fetched once, on expand, and the transcript was read by tail', async ({ page }) => {
  expect(BIG.length).toBeGreaterThan(OUTPUT_CAP_BYTES);
  const engine = await installMockEngine(page, scenario);
  await openApp(page);
  await send(page, 'Find needle');
  await expect(page.getByText('Found two thousand matches.')).toBeVisible();
  const tools = () => engine.calls.filter((c) => c.method === 'GET' && c.path.includes('/tools/g1'));
  expect(tools()).toHaveLength(0);
  await page.getByRole('button', { name: /^Worked / }).click();
  await page.getByRole('button', { name: 'Used probe' }).click();
  await page.getByRole('button', { name: 'probe probe' }).click();
  await expect(page.getByLabel('Output')).toContainText('match 1:');
  expect(tools()).toHaveLength(1);
});
