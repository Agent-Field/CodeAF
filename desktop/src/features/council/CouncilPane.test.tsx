import { test, expect } from '@playwright/test';

type Fake = { calls: string[]; say: (s: string, t: string) => void; decide: () => void };
const fake = (page: import('@playwright/test').Page) => page.evaluate(() => (window as unknown as { __council: Fake }).__council.calls.slice());

for (const theme of ['light', 'dark'] as const) {
  test.describe(theme, () => {
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme); await page.goto('/'); });

    test('a council session draws labelled place messages under a two-square title', async ({ page }) => {
      await expect(page.locator('.council-head-title')).toHaveText('Marketing with Software');
      await expect(page.locator('.council-head .place-swatch')).toHaveCount(2);
      await expect(page.locator('.council-message')).toHaveCount(2);
      await expect(page.locator('.council-message').first().locator('.council-message-name')).toHaveText('Marketing');
      await expect(page.locator('.council-message').nth(1).locator('.council-message-name')).toHaveText('Software');
      await expect(page.getByPlaceholder('Add to this discussion')).toBeVisible();
      await expect(page.locator('.council-decided')).toHaveCount(0);
    });

    test('new lines arrive while it runs and a decided card closes it', async ({ page }) => {
      await page.evaluate(() => { const c = (window as unknown as { __council: Fake }).__council; c.say('Marketing', 'Then I will write it that way.'); c.decide(); });
      await expect(page.locator('.council-message')).toHaveCount(3);
      await expect(page.locator('.council-decided-title')).toContainText('Decided: promise it outside strict mode');
      await expect(page.locator('.council-decided-facts')).toContainText('3 turns');
      await expect(page.getByPlaceholder('This discussion has ended')).toBeDisabled();
    });

    test('typing pauses once, sending steers and resumes, clearing resumes', async ({ page }) => {
      const box = page.getByPlaceholder('Add to this discussion');
      await box.pressSequentially('wait');
      await expect.poll(() => fake(page)).toEqual(['pause']);
      await box.fill('');
      await expect.poll(() => fake(page)).toEqual(['pause', 'resume']);
      await box.pressSequentially('hold on');
      await box.press('Enter');
      await expect.poll(() => fake(page)).toEqual(['pause', 'resume', 'pause', 'steer:hold on']);
      await expect(page.locator('.user-message')).toContainText('hold on');
    });
  });
}

test('an ordinary session is not a council', async ({ page }) => {
  await page.goto('/?session=/home/.codeaf/sessions/other/transcript.jsonl');
  await expect(page.locator('.council-head')).toHaveCount(0);
});
