import { test, expect, type Page } from '@playwright/test';

// The suite's Vite server serves both harnesses, so council coverage needs no extra servers or fixed ports.
const COUNCIL = '/tests/council-wire/harness/index.html';
const HOME = '/tests/places-home/harness/index.html';
type Fake = { calls: string[]; say: (s: string, t: string) => void; decide: () => void; escalate: () => void };
const calls = (page: Page) => page.evaluate(() => (window as unknown as { __council: Fake }).__council.calls.slice());

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme); });

    test('Live shows the running council as "2 of 6 turns" and opening it makes the active tab', async ({ page }) => {
      await page.goto(`${HOME}?live=1`);
      const row = page.locator('[data-attention-id="council"] button');
      await expect(row).toContainText('Marketing with Software');
      await expect(row.locator('.home-live-aside')).toHaveText('2 of 6 turns');
      await row.click();
      await expect(page.getByLabel('Workspace state')).toContainText('"active":"council"');
    });

    test('an open council labels each line by its place and keeps a composer for the person', async ({ page }) => {
      await page.goto(COUNCIL);
      await expect(page.locator('.council-head-title')).toHaveText('Marketing with Software');
      await expect(page.locator('.council-message-name')).toHaveText(['Marketing', 'Software']);
      await expect(page.getByPlaceholder('Add to this discussion')).toBeEnabled();
      await expect(page.locator('.council-decided')).toHaveCount(0);
    });

    test('a decision draws the decided card and ends the discussion', async ({ page }) => {
      await page.goto(COUNCIL);
      await page.evaluate(() => (window as unknown as { __council: Fake }).__council.decide());
      const card = page.locator('.council-decided');
      await expect(card.locator('.council-decided-title')).toHaveText('Decided: promise it outside strict mode');
      await expect(card.locator('.council-decided-facts')).toContainText('3 turns');
      await expect(page.getByPlaceholder('This discussion has ended')).toBeDisabled();
    });

    test('typing pauses the council once and it stays paused until Send', async ({ page }) => {
      await page.goto(COUNCIL);
      const box = page.getByPlaceholder('Add to this discussion');
      await box.pressSequentially('wait a moment');
      await expect.poll(() => calls(page)).toEqual(['pause']);
      // No resume while text remains: more keystrokes and a pause of their own make no second request.
      await page.waitForTimeout(300);
      expect(await calls(page)).toEqual(['pause']);
      await box.press('Enter');
      await expect.poll(() => calls(page)).toEqual(['pause', 'steer:wait a moment']);
      await expect(page.locator('.user-message')).toContainText('wait a moment');
    });

    test('a council that hit its cap is ended: the composer closes and no decided card is drawn', async ({ page }) => {
      await page.goto(COUNCIL);
      await page.evaluate(() => (window as unknown as { __council: Fake }).__council.escalate());
      await expect(page.getByPlaceholder('This discussion has ended')).toBeDisabled();
      await expect(page.locator('.council-decided')).toHaveCount(0);
    });
  });
}

// The engine has no sink yet that turns an Escalation into a needs-you item (internal/council/run.go names none outside tests), and the
// renderer's world feed carries no capped-council item, so there is nothing for Next up to list. Enable when the bridge lands it (I2.11).
test.fixme('cap escalation with no shared parent adds a Next up item', async () => {});
