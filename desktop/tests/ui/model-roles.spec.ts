import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, MODEL } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp, send } from './support/conversation';

const OTHER = 'moonshotai/kimi-k3';
const fresh = () => ({
  ...plainReply(),
  initial: { entries: [], title: '' },
  models: [{ id: MODEL, name: 'DeepSeek V4.1 Flash' }, { id: OTHER, name: 'Kimi K3', efforts: ['low', 'medium', 'high'] }],
});
const chip = (page: Page, label: string) => page.getByRole('button', { name: `Model: ${label}` });

test('swapping the model in the composer sets the Conversation role and the next turn runs on it', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  await send(page, 'first');
  await expect.poll(() => engine.turnModels).toEqual([MODEL]);
  await chip(page, 'DeepSeek v4.1 Flash').click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  // Only the pinned models are listed until the full catalog is opened.
  await popover.getByText('All models…').click();
  await popover.getByRole('radiogroup', { name: 'Models', exact: true }).getByRole('radio', { name: 'Kimi K3' }).click();
  await expect(chip(page, 'Kimi K3')).toBeVisible();
  const put = engine.calls.find(call => call.method === 'PUT');
  expect(put?.path).toMatch(/\/models\/roles\/conversation$/);
  expect(put?.body.model).toBe(OTHER);
  await send(page, 'second');
  await expect.poll(() => engine.turnModels).toEqual([MODEL, OTHER]);
});

test('another catalog model switches the chat and an effort choice is saved on the role', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  // A fresh tab makes no engine call, so the list is read once the first message has made a session.
  await send(page, 'hello');
  await expect.poll(() => engine.calls.some(call => call.path.endsWith('/models/roles'))).toBe(true);
  await chip(page, 'DeepSeek v4.1 Flash').click();
  await page.getByRole('dialog', { name: 'Model' }).getByText('All models…').click();
  await page.getByRole('radiogroup', { name: 'Models', exact: true }).getByRole('radio', { name: 'Kimi K3' }).click();
  await expect(chip(page, 'Kimi K3')).toBeVisible();
  await chip(page, 'Kimi K3').click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  await popover.getByRole('radiogroup', { name: 'Effort' }).getByRole('radio', { name: 'High' }).click();
  await expect.poll(() => engine.calls.filter(call => call.method === 'PUT').map(call => `${call.body.model}:${call.body.effort}`)).toContain(`${OTHER}:high`);
});
