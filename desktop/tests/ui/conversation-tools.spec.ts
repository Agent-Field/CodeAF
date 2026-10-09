import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { toolsReply } from './support/scenarios';
import { openApp, send } from './support/conversation';

test('tools collapse to one line, expand to rows, rows to output, and full output is fetched', async ({ page }) => {
  const engine = await installMockEngine(page, { ...toolsReply(), initial: { entries: [] } });
  await openApp(page);
  await send(page, 'Check the build');
  const summary = page.getByRole('button', { name: /Worked · 3 steps/ });
  await expect(summary).toBeVisible();
  await expect(summary).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByText('The build passes.')).toBeVisible();
  await expect(page.getByRole('button', { name: /package\.json/ })).toHaveCount(0);
  await summary.click();
  await expect(summary).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('button', { name: /package\.json/ })).toBeVisible();
  const row = page.getByRole('button', { name: /npm run build/ });
  await expect(row).toHaveAttribute('aria-expanded', 'false');
  await row.click();
  await expect(row).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByText('line 1: build output')).toBeVisible();
  await expect(page.getByText('line 60: build output')).toHaveCount(0);
  await page.getByRole('button', { name: 'Show full output' }).click();
  await expect(page.getByText('line 60: build output')).toBeVisible();
  expect(engine.calls.some(c => c.method === 'GET' && c.path.endsWith('/tools/call-3'))).toBe(true);
  await expect(page.getByRole('button', { name: 'Show full output' })).toHaveCount(0);
});
