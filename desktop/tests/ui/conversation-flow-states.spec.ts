import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { richReply } from './support/scenarios-v2';
import { openApp, send } from './support/conversation';

// Design 1f in the conversation flow: scroll edges, anchor, folded rows and focus.
test('the top edge fades in over 120ms', async ({ page }) => {
  await installMockEngine(page, richReply());
  await openApp(page);
  const scroller = page.locator('.conversation-scroll');
  await expect(scroller).toHaveCSS('transition-duration', '0.12s');
  await expect(scroller).toHaveCSS('transition-property', 'mask-size');
});

test('late growth after a follow (an image decoding) does not unanchor the reader', async ({ page }) => {
  await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  await page.waitForTimeout(800);
  const distance = await page.locator('.conversation-scroll').evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop);
  expect(distance).toBeLessThanOrEqual(48);
  await expect(page.getByRole('button', { name: /^Latest/ })).toHaveCount(0);
});

test('a folded turn warms on hover and shows its unfold chevron; a mouse click leaves no focus ring', async ({ page }) => {
  await installMockEngine(page, plainReply());
  await openApp(page);
  await send(page, 'Explain the add helper');
  await page.getByRole('button', { name: 'Fold', exact: true }).click();
  const row = page.getByRole('button', { name: 'Unfold', exact: true }).first();
  const chevron = row.locator('.turn-folded-chevron');
  await expect(chevron).toHaveCSS('opacity', '0');
  const rest = await row.evaluate((el) => getComputedStyle(el).backgroundColor);
  await row.hover();
  await expect(chevron).toHaveCSS('opacity', '1');
  expect(await row.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe(rest);
  expect(await row.evaluate((el) => getComputedStyle(el).transform)).toBe('none');
  await row.click();
  await page.getByRole('button', { name: 'Fold', exact: true }).first().click();
  expect(await page.getByRole('button', { name: 'Unfold', exact: true }).first().evaluate((el) => getComputedStyle(el).boxShadow)).toBe('none');
});

test('an answer table fades its right edge only while more lies that way', async ({ page }) => {
  await installMockEngine(page, plainReply());
  await openApp(page);
  await send(page, 'Explain the add helper');
  const scroll = page.locator('.markdown-table-scroll');
  await expect(scroll).toBeVisible();
  await page.setViewportSize({ width: 360, height: 700 });
  // Wait for the table to overflow before scrolling: "false" equals "false" before the narrow layout lands, and scrolling then moves nothing.
  await expect(scroll).toHaveAttribute('data-more', 'true');
  await expect.poll(() => scroll.evaluate((el) => String(el.scrollLeft + el.clientWidth < el.scrollWidth - 1) === el.getAttribute('data-more'))).toBe(true);
  await scroll.evaluate((el) => (el.scrollLeft = el.scrollWidth));
  await expect(scroll).toHaveAttribute('data-more', 'false');
});
