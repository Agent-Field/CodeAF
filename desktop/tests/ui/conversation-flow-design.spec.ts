import { openPage } from './support/shell-navigation';
import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { richReply } from './support/scenarios-v2';
import { message, openApp, send } from './support/conversation';

// Numbers measured from design v3 (Components: Update · Final answer, Turn footer · System notes,
// Work block · Step, Tool call row, Thinking, Task notice, Receipts in the flow), light and dark.

async function openSpecimen(page: Page, name: string, scheme: 'light' | 'dark'): Promise<Locator> {
  await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
  await installMockEngine(page, { ...plainReply(), initial: { id: 'specimen', entries: [] } });
  await page.goto('/');
  await openPage(page, 'Design system');
  const section = page.locator(`section[aria-label="${name} specimen"]`);
  await section.scrollIntoViewIfNeeded();
  return section;
}

const style = (locator: Locator, property: string) => locator.evaluate((el, prop) => getComputedStyle(el).getPropertyValue(prop), property);
const rootColor = (page: Page, token: string) =>
  page.evaluate((name) => {
    const probe = document.createElement('span');
    probe.style.color = `var(${name})`;
    document.body.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, token);

for (const scheme of ['light', 'dark'] as const) {
  test.describe(`${scheme}: flow components follow the design`, () => {
    test('a cut update ends on the line of its words; steps, thinking and notices wear the design type and height', async ({ page }) => {
      const turns = await openSpecimen(page, 'Turns', scheme);
      const cut = turns.locator('.update-cut');
      const sameLine = await turns.locator('.update-body[data-cut]').evaluate((body) => {
        const words = body.querySelector('.markdown p')!.getClientRects();
        const marker = body.querySelector('.update-cut')!.getBoundingClientRect();
        return Math.abs(marker.top - words[words.length - 1].top) < 4;
      });
      expect(sameLine).toBe(true);
      await expect(cut).toHaveCSS('margin-inline-start', '4px');
      await expect(turns.locator('.update-block').first()).toHaveCSS('border-left-width', '2px');

      const work = await openSpecimen(page, 'Work', scheme);
      await expect(work.locator('.work-step-title').first()).toHaveCSS('font-weight', '400');
      await expect(work.locator('.work-step .work-time').first()).toHaveCSS('font-weight', '400');
      await expect(work.locator('.thinking-row').first()).toHaveCSS('font-weight', '400');
      await expect(work.locator('.thinking[data-live]').first()).toHaveCSS('row-gap', '6px');

      const notices = await openSpecimen(page, 'Task notices', scheme);
      const heights = await notices.locator('.task-notice').evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().height)));
      expect(heights).toEqual([35, 50, 35]);
    });

    test('a running step is one row with its command beneath; a failed call carries its time in danger ink', async ({ page }) => {
      const work = await openSpecimen(page, 'Work', scheme);
      const running = work.locator('.work-step[data-state="running"]').first();
      await expect(running.locator('.work-step-caption')).toHaveText('$ go test ./internal/session -run TestShape');
      await expect(running.locator('.work-step-head > .app-icon[data-icon="chevron"]')).toHaveCSS('visibility', 'hidden');
      await expect(running.locator('.work-call')).toHaveCount(0);
      await running.getByRole('button').first().click();
      await expect(running.locator('.work-call').first()).toBeVisible();

      const danger = await rootColor(page, '--danger');
      const failed = work.locator('.work-call[data-state="failed"] .work-call-time').first();
      expect(await style(failed, 'color')).toBe(danger);
      await expect(work.locator('.work-call[data-state="failed"] .work-call-text').first()).toHaveCSS('font-family', /mono|Menlo|Consolas/i);
      const output = work.locator('.work-term-body .type-code:not(.work-term-command)').first();
      expect(await style(work.locator('.work-term-command').first(), 'color')).toBe(await rootColor(page, '--ink'));
      await expect(output).toHaveCSS('font-size', '11.5px');
      expect(await style(output, 'color')).toBe(await rootColor(page, '--ink-2'));
    });

    test('a long engine note is a chevron disclosure line; a receipt reads "verdict · who · when" with its command on a field chip', async ({ page }) => {
      const notes = await openSpecimen(page, 'System notes', scheme);
      const row = notes.locator('.note-folded-row');
      await expect(row).toHaveAttribute('aria-expanded', 'false');
      await expect(row).toContainText('Task 1 done: reading the folder.');
      await row.click();
      await expect(row).toHaveAttribute('aria-expanded', 'true');
      await expect(notes.locator('.note-folded-body')).toContainText('Folder: a scratch workspace.');

      const tray = await openSpecimen(page, 'Decision tray', scheme);
      await expect(tray.locator('.receipt-rest').first()).toHaveText('· you · 14:02');
      const code = tray.locator('.receipt-code').first();
      expect(await style(code, 'background-color')).toBe(await rootColor(page, '--field'));
      await expect(code).toHaveCSS('font-size', '11px');
    });
  });
}

test('the answer names its work on hover and the link opens the work block', async ({ page }) => {
  await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  const summary = page.getByRole('button', { name: /^Worked \d+s · 2 steps · 5 calls$/ });
  await expect(summary).toHaveAttribute('aria-expanded', 'false');
  await page.locator('.answer-block').hover();
  await page.getByRole('button', { name: /^Worked \d+s$/ }).click();
  await expect(summary).toHaveAttribute('aria-expanded', 'true');
  await summary.click();
  await expect(summary).toHaveAttribute('aria-expanded', 'false');
  await expect(message(page)).toBeVisible();
});
