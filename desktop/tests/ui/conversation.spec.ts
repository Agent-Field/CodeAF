import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply, streaming } from './support/scenarios';
import { message, openApp, posts, send } from './support/conversation';

const fresh = () => ({ ...plainReply(), initial: { entries: [], title: '' } });

test('a fresh tab is quiet: centred composer, no engine call, Send disabled when blank', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled();
  await message(page).fill('   ');
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled();
  const box = (await message(page).boundingBox())!;
  const middle = box.y + box.height / 2;
  expect(middle).toBeGreaterThan(page.viewportSize()!.height * 0.25);
  expect(middle).toBeLessThan(page.viewportSize()!.height * 0.75);
  expect(engine.calls).toEqual([]);
});

test('first send creates the session, shows literal words, renders a Markdown reply and docks the composer', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  const before = (await message(page).boundingBox())!.y;
  await send(page, 'Explain **the** add helper');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  expect(posts(engine, '/sessions')).toHaveLength(1);
  expect(posts(engine, '/turn')[0].body).toMatchObject({ text: 'Explain **the** add helper', mode: 'submit' });
  const bubble = page.getByRole('tabpanel').getByText('Explain **the** add helper', { exact: true });
  await expect(bubble).toBeVisible();
  expect(await bubble.locator('strong').count()).toBe(0);
  await expect(page.getByText('Here is the short version.')).toBeVisible();
  await expect(page.locator('pre').filter({ hasText: 'export const add' })).toBeVisible();
  await expect(page.getByText('ts', { exact: true }).first()).toBeVisible();
  await expect(page.getByRole('button', { name: /^Copy code/ }).first()).toBeVisible();
  const table = page.getByRole('table');
  await expect(table).toBeVisible();
  const inScrollRegion = await table.evaluate(el => {
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const overflow = getComputedStyle(n).overflowX;
      if (overflow === 'auto' || overflow === 'scroll') return true;
    }
    return false;
  });
  expect(inScrollRegion).toBe(true);
  await expect.poll(async () => (await message(page).boundingBox())!.y).toBeGreaterThan(before);
  const docked = (await message(page).boundingBox())!;
  expect(docked.y + docked.height).toBeGreaterThan(page.viewportSize()!.height * 0.8);
  await expect(message(page)).toHaveValue('');
});

test('Enter sends, Shift+Enter adds a line, composition never sends', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  await message(page).fill('first line');
  await message(page).press('Shift+Enter');
  await message(page).pressSequentially('second line');
  await expect(message(page)).toHaveValue('first line\nsecond line');
  expect(posts(engine, '/turn')).toHaveLength(0);
  await message(page).evaluate(el => {
    el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229, bubbles: true, cancelable: true }));
  });
  await page.waitForTimeout(150);
  expect(posts(engine, '/turn')).toHaveLength(0);
  await expect(message(page)).toHaveValue('first line\nsecond line');
  await message(page).press('Enter');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  expect(posts(engine, '/turn')[0].body.text).toBe('first line\nsecond line');
});

test('while running: Stop when empty, Steer with text, Queue from the menu', async ({ page }) => {
  const engine = await installMockEngine(page, { ...streaming(), manual: true });
  await openApp(page);
  await send(page, 'Say hello');
  await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
  await message(page).fill('also be brief');
  await expect(page.getByRole('button', { name: 'Steer', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Stop', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'More send options' }).click();
  await page.getByRole('menuitem', { name: /Queue/ }).click();
  await expect.poll(() => posts(engine, '/turn').length).toBe(2);
  expect(posts(engine, '/turn')[1].body).toMatchObject({ text: 'also be brief', mode: 'queue' });
  await message(page).fill('steer this way');
  await message(page).press('Enter');
  await expect.poll(() => posts(engine, '/turn').length).toBe(3);
  expect(posts(engine, '/turn')[2].body).toMatchObject({ text: 'steer this way', mode: 'steer' });
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect.poll(() => posts(engine, '/stop').length).toBe(1);
});

test('folding a turn shows the digest and survives reload; reattach uses the saved session file', async ({ page }) => {
  const engine = await installMockEngine(page, fresh());
  await openApp(page);
  await send(page, 'Explain the add helper');
  await expect(page.getByRole('table')).toBeVisible();
  const fold = page.getByRole('button', { name: 'Fold', exact: true });
  await expect(fold).toHaveAttribute('aria-expanded', 'true');
  await fold.click();
  await expect(page.getByRole('button', { name: 'Unfold', exact: true })).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByText('Here is the short version.')).toBeVisible();
  await expect(page.getByRole('table')).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('button', { name: 'Unfold', exact: true })).toBeVisible();
  await expect.poll(() => posts(engine, '/sessions').length).toBeGreaterThanOrEqual(2);
  expect(posts(engine, '/sessions').pop()!.body).toEqual({ sessionFile: 'mock-session-1.jsonl' });
  await page.getByRole('button', { name: 'Unfold', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Fold', exact: true })).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('table')).toBeVisible();
});

test('a failed turn keeps the draft and offers Retry', async ({ page }) => {
  const engine = await installMockEngine(page, { ...fresh(), fail: { turn: 500 } });
  await openApp(page);
  await send(page, 'please keep me');
  await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
  await expect(message(page)).toHaveValue('please keep me');
  expect(posts(engine, '/turn').length).toBeGreaterThanOrEqual(1);
});
