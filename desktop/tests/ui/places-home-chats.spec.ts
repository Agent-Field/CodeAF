import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor } from './support/mock-places';

const placeId = 'pl_0000000000000005';

for (const theme of ['light', 'dark']) {
  test(`d5-pl-test-home: ${theme} real digests and open-or-focus in this place`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { initial: { entries: [], title: 'Strict mode decision' }, turns: [] });
    await installMockPlaces(page, {
      places: [{ id: placeId, name: 'Config parser', pinned: true }],
      chats: [
        { id: 'sess-run', title: 'Update fixtures', places: [placeId] },
        { id: 'sess-need', title: 'Port fix to v1', places: [placeId] },
        { id: 'sess-done', title: 'Strict mode decision', places: [placeId] },
      ],
    });
    // The response retains the canonical wire fixture, with engine-written recap evidence for this test's chats.
    const digest = JSON.parse(readFileSync(new URL('../../src/features/places/fixtures/home-place.json', import.meta.url), 'utf8'));
    digest.children = [];
    digest.chats.push({ ...digest.chats[0], id: 'sess-done', title: 'Strict mode decision', live: false, doing: '', needsYou: false, tasks: { running: 0, incomplete: 0, failed: 0, done: 1 }, at: undefined });
    for (const chat of digest.chats) chat.sessionFile = sessionFileFor(chat.id);
    digest.chats.push({ ...digest.chats[2], id: 'sess-missing', title: 'Missing saved location', sessionFile: undefined });
    digest.recap = { label: 'Since yesterday', text: 'Kept strict mode.', since: digest.readAt, chats: 1, unsummarised: 0,
      items: [{ chatId: 'sess-done', chatTitle: 'Strict mode decision', at: digest.readAt, line: 'Kept strict mode as the default.' }] };
    await page.route(`**/api/engine/places/${placeId}`, route => route.fulfill({ json: digest }));
    await page.goto('/');
    await page.getByRole('navigation', { name: 'Places' }).getByRole('button', { name: 'Config parser', exact: true }).click();
    const chats = page.getByRole('region', { name: 'Chats', exact: true });
    await expect(chats.locator('.places-chat-row')).toHaveCount(4);
    await expect(chats.locator('[data-chat-id="sess-done"]')).toContainText('Kept strict mode as the default.');
    await expect(chats.locator('[data-chat-id="sess-done"] time')).toHaveCount(0);
    await expect(chats.locator('[data-chat-id="sess-run"] .places-chat-excerpt')).toHaveCount(0);
    await expect(chats.locator('[data-chat-id="sess-run"]')).toHaveAttribute('data-status', 'running');
    await expect(chats.locator('[data-chat-id="sess-need"]')).toHaveAttribute('data-status', 'waiting');
    await expect(chats.locator('[data-chat-id="sess-need"]')).toContainText('Allow the v1 branch push?');
    for (const row of await chats.locator('.places-chat-row').all()) {
      expect(await row.evaluate(el => el.getBoundingClientRect().height)).toBe(44);
    }
    await chats.locator('[data-chat-id="sess-missing"] button').click();
    await expect(chats.getByRole('alert')).toHaveText('This chat cannot be opened here: the engine did not say where it is saved.');
    for (const width of [320, 1200]) {
      await page.setViewportSize({ width, height: 800 });
      await expect(chats.locator('.places-chat-title').first()).toHaveCSS('font-size', '13px');
      await expect(chats.locator('.places-chat-excerpt').first()).toHaveCSS('font-size', '11px');
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
    }
    const row = chats.locator('[data-chat-id="sess-done"] button');
    await row.click();
    const tab = page.locator('.workspace-tabstrip').getByRole('tab').filter({ hasText: 'Strict mode decision' });
    await expect(tab).toHaveCount(1);
    await expect(tab).toHaveAttribute('aria-selected', 'true');
    await page.locator('.workspace-tab.is-place-home').click();
    await row.click();
    await expect(tab).toHaveCount(1);
    await expect(tab).toHaveAttribute('aria-selected', 'true');
  });
}
