import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';

const reply = Array.from({ length: 90 }, (_, i) => `Scroll memory paragraph ${i + 1}.`).join('\n\n');

for (const bottom of [false, true]) {
  test(`reload restores ${bottom ? 'bottom following' : 'a mid-transcript position'} in this window`, async ({ page }) => {
    await installMockEngine(page, {
      initial: { title: 'Scroll memory', entries: [], running: false },
      turns: [{ entries: [{ Role: 'assistant', Text: reply, Answer: true }] }],
    });
    await openApp(page);
    await send(page, 'Remember where I am reading');
    const scroller = page.locator('.conversation-scroll');
    await expect(page.getByText('Scroll memory paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(2);
    // Programmatic positioning avoids platform wheel momentum while exercising the real scroll listener and persistence.
    const target = await scroller.evaluate((el, following) => {
      const max = el.scrollHeight - el.clientHeight;
      el.dispatchEvent(new WheelEvent('wheel', { bubbles: true }));
      el.scrollTop = following ? max : Math.round(max / 2);
      el.dispatchEvent(new Event('scroll'));
      return el.scrollTop;
    }, bottom);
    expect(target).toBeGreaterThan(100);
    await expect.poll(() => page.evaluate(() => {
      const key = Object.keys(localStorage).find(k => k.startsWith('codeaf.desktop.tabScroll.v2.') && !k.endsWith('.index'));
      return key ? localStorage.getItem(key) : null;
    })).toContain(`"top":${Math.round(target)}`);
    await page.reload();
    await expect(page.getByText('Scroll memory paragraph 90.', { exact: true })).toBeAttached();
    await expect.poll(() => scroller.evaluate((el, saved) => Math.abs(el.scrollTop - (saved.following ? el.scrollHeight - el.clientHeight : saved.target)), { following: bottom, target })).toBeLessThanOrEqual(2);
  });
}
