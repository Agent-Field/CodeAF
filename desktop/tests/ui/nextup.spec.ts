import { expect, test, type Page } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { openNextUp, origin } from './fixtures/nextup';

// Finite mock streams can still be reconnecting when a test closes its page.
test.afterEach(async ({ page }) => { await page.unrouteAll({ behavior: 'ignoreErrors' }); });

const pill = (page: Page) => page.getByRole('button', { name: /\d+ need you elsewhere/ });
async function counted(page: Page, count: number) {
 await expect(pill(page)).toHaveAccessibleName(`${count} need you elsewhere. Press ⌘J`);
 await expect(pill(page).locator('.frame-pill-count')).toHaveText(`${count} need you`);
 await expect(pill(page).locator('.frame-pill-muted')).toHaveText(['elsewhere', '⌘J']);
}
const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });
const popover = (page: Page) => page.getByRole('group', { name: /need you in other conversations/ });

async function current(page: Page, title: string, tint: string) {
 await expect(page.getByRole('tab', { name: title, exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.locator('body')).toHaveAttribute('data-tint', tint);
 await expect(tray(page)).toBeVisible();
}
async function answer(page: Page) {
 await tray(page).getByRole('radio', { name: /SQLite/ }).focus();
 await page.keyboard.press('Space');
 await expect(tray(page).getByRole('radio', { name: /SQLite/ })).toBeChecked();
 await tray(page).getByRole('button', { name: 'Choose', exact: true }).click();
}
async function returned(page: Page) {
 await expect(page.getByRole('tab', { name: 'Config stack', exact: true })).toHaveAttribute('aria-selected', 'true');
 await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
 await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Keep my draft');
 await expect(page.locator('.next-up-chip')).toHaveCount(0);
}

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: frame pill excludes here; hover lists five items in three places; zero hides`, async ({ page }) => {
  const fixture = await openNextUp(page, theme);
  await counted(page, 5);
  await pill(page).hover();
  await expect(popover(page)).toBeVisible();
  await expect(popover(page).locator('.queue-popover-where')).toHaveText('in other conversations · 3 places');
  await expect(popover(page).locator('.queue-popover-ask')).toHaveText([
   'Choose the headline?', 'Use the shared lexer?', 'Keep the old schema?', 'Run the migration?', 'Delete the notes?',
  ]);
  await expect(popover(page)).not.toContainText(fixture.here.text);
  await expect(popover(page).getByRole('button', { name: 'Start ⌘J', exact: true })).toBeVisible();
  await expect(popover(page).getByRole('button', { name: 'Accept 3 suggestions', exact: true })).toBeVisible();
  await expect(popover(page)).toHaveCSS('width', design.foundation['i2-queue-pop-width']);
  fixture.clearElsewhere();
  await expect(pill(page)).toHaveCount(0);
  await expect(popover(page)).toHaveCount(0);
  await expect(tray(page)).toBeVisible();
 });

 test(`${theme}: Command J walks blocking first and irreversible last across frame tints; clear returns`, async ({ page }) => {
  const fixture = await openNextUp(page, theme);
  await page.keyboard.press('Meta+j');
  const order = [['Brand copy', 'rose'], ['Build parser', 'tide'], ['Ledger schema', 'sand'], ['Ledger import', 'sand'], ['Remove notes', 'rose']];
  for (const [index, [title, tint]] of order.entries()) {
   await current(page, title, tint);
   await expect(page.locator('.next-up-chip')).toHaveText(`Next up${index + 1} of 5`);
   await answer(page);
  }
  const clear = page.getByRole('region', { name: "You're clear" });
  await expect(clear).toBeVisible();
  await expect(clear.getByRole('status')).toContainText('5 answered.');
  expect(fixture.answers.map(row => row.session)).toEqual([fixture.elsewhere[2], fixture.elsewhere[3], fixture.elsewhere[0], fixture.elsewhere[4], fixture.elsewhere[1]].map(item => item.session));
  expect(fixture.answers.some(row => row.session === origin)).toBe(false);
  await clear.getByRole('button', { name: 'Back to Config stack' }).click();
  await returned(page);
  await expect(pill(page)).toHaveCount(0);
 });

 test(`${theme}: pill starts; Skip sends the front to the back; Escape restores the start`, async ({ page }) => {
  const fixture = await openNextUp(page, theme);
  await pill(page).click();
  await current(page, 'Brand copy', 'rose');
  const skip = tray(page).getByRole('button', { name: 'Skip', exact: true });
  await expect.poll(() => skip.evaluate(el => el.getBoundingClientRect().height)).toBeCloseTo(30, 1);
  await skip.click();
  for (const [title, tint] of [['Build parser', 'tide'], ['Ledger schema', 'sand'], ['Ledger import', 'sand'], ['Remove notes', 'rose']]) {
   await current(page, title, tint);
   await answer(page);
  }
  await current(page, 'Brand copy', 'rose');
  expect(fixture.answers).toHaveLength(4);
  await page.keyboard.press('Escape');
  await returned(page);
  await counted(page, 1);
 });

 test(`${theme}: Accept 3 suggestions offers Undo before any engine answer; settles only reversible picks`, async ({ page }) => {
  await page.clock.install();
  const fixture = await openNextUp(page, theme);
  await pill(page).hover();
  await popover(page).getByRole('button', { name: 'Accept 3 suggestions' }).click();
  const toast = page.locator('.toast');
  await expect(toast).toContainText('Accepted 3 suggestions');
  expect(fixture.answers).toHaveLength(0);
  await toast.getByRole('button', { name: 'Undo', exact: true }).click();
  await page.clock.fastForward(design.interaction.toastDuration + 1);
  expect(fixture.answers).toHaveLength(0);
  await counted(page, 5);
  await pill(page).hover();
  await popover(page).getByRole('button', { name: 'Accept 3 suggestions' }).click();
  await expect(toast).toContainText('Accepted 3 suggestions');
  await page.mouse.move(0, 0);
  await page.getByRole('textbox', { name: 'Message' }).focus();
  await page.clock.fastForward(design.interaction.toastDuration + 1);
  await expect.poll(() => fixture.answers.length).toBe(3);
  expect(fixture.answers.map(row => row.session).sort()).toEqual([fixture.elsewhere[0], fixture.elsewhere[2], fixture.elsewhere[4]].map(item => item.session).sort());
  for (const { answer } of fixture.answers) expect(answer).toMatchObject({ key: 'sqlite', picked: ['sqlite'], decidedBy: 'person' });
  await page.clock.runFor(1100);
  await counted(page, 2);
 });

 test(`${theme}: a new blocking item banners for four seconds, then folds into the pill`, async ({ page }) => {
  const fixture = await openNextUp(page, theme, { arrival: true });
  await counted(page, 4);
  await expect.poll(fixture.connected).toBe(true);
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  await page.evaluate(() => {
   // CSS transitions use the browser's paint clock; retain the fold even after its fade ends.
   new MutationObserver(records => {
    if (records.some(record => (record.target as Element).matches('.nextup-banner[data-phase="into-pill"]'))) {
     document.body.dataset.nextUpBannerFolded = 'true';
    }
   }).observe(document.body, { subtree: true, attributes: true, attributeFilter: ['data-phase'] });
  });
  fixture.arrive();
  const banner = page.locator('.nextup-banner');
  await expect(banner).toBeAttached();
  await page.clock.runFor(32);
  await expect(banner).toHaveAttribute('data-phase', 'rest');
  await expect(banner).toContainText('Use the shared lexer?');
  await counted(page, 5);
  // The frozen clock starts at arrival; the two paint frames consumed 32ms of the hold.
  await page.clock.fastForward(3967);
  await expect(banner).toHaveAttribute('data-phase', 'rest');
  await page.clock.fastForward(1);
  await expect(page.locator('body')).toHaveAttribute('data-next-up-banner-folded', 'true');
  await page.clock.fastForward(Number.parseFloat(design.foundation['i2-banner-duration']));
  await expect(banner).toHaveCount(0);
  await counted(page, 5);
 });
}
