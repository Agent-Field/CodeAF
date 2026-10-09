import { test, expect } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { openApp, posts, send } from './support/conversation';

// Design 1f, Scroll edges and Scroll behaviour: anchor, top mask, Latest pill.
const LONG = Array.from({ length: 60 }, (_, at) => `Paragraph ${at + 1} of a long reply that fills the reading column.`).join('\n\n');

const longReply: Scenario = {
  initial: { title: 'Long', entries: [], running: false },
  turns: [{ entries: [{ Role: 'assistant', Text: LONG, Answer: true }] }],
};

const scroller = (page: import('@playwright/test').Page) => page.locator('.conversation-scroll');
const pill = (page: import('@playwright/test').Page) => page.getByRole('button', { name: /^Latest/ });

test('the top fade turns on only once content sits above the edge', async ({ page }) => {
  await installMockEngine(page, longReply);
  await openApp(page);
  await send(page, 'Write a lot');
  await expect(page.getByText('Paragraph 60 of a long reply', { exact: false })).toBeVisible();
  // Following the end of a long reply leaves content above the top edge.
  await expect(scroller(page)).toHaveAttribute('data-scrolled', 'true');
  await scroller(page).evaluate((el) => (el.scrollTop = 0));
  await expect(scroller(page)).not.toHaveAttribute('data-scrolled', 'true');
});

test('the Latest pill appears only when unanchored with something new, and returns to the end', async ({ page }) => {
  await installMockEngine(page, longReply);
  await openApp(page);
  await send(page, 'Write a lot');
  await expect(page.getByText('Paragraph 60 of a long reply', { exact: false })).toBeVisible();
  await expect(pill(page)).toHaveCount(0);
  // Away from the end with nothing live or new: still no pill.
  await scroller(page).evaluate((el) => (el.scrollTop = 0));
  await expect(pill(page)).toHaveCount(0);
  // Within 48px of the end counts as the bottom.
  await scroller(page).evaluate((el) => (el.scrollTop = el.scrollHeight - el.clientHeight - 40));
  await expect(pill(page)).toHaveCount(0);
});

test('a running turn offers Latest with the live Working label while the reader is away', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { title: 'Long', entries: [] }, turns: [{}], manual: true });
  await openApp(page);
  await send(page, 'Write a lot');
  // The reply must land after the engine has the message, or it would belong to an
  // earlier, settled turn, which folds to its digest.
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ running: true, entries: [{ Role: 'user', Text: 'Write a lot' }, { Role: 'assistant', Text: LONG, Answer: true }] });
  await expect(page.getByText('Paragraph 60 of a long reply', { exact: false })).toBeVisible();
  await expect(pill(page)).toHaveCount(0);
  await scroller(page).evaluate((el) => (el.scrollTop = 0));
  await expect(pill(page)).toBeVisible();
  await expect(pill(page)).toContainText('Working');
  await pill(page).click();
  await expect(pill(page)).toHaveCount(0);
  await expect.poll(() => scroller(page).evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(48);
});
