import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { expectNoUnstyledControls } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };

// The group suggestion (Shell 2b), driven on the Design system specimen: the real GroupOffer, TabStrip and workspace reducer
// over a fixture of three tabs from one session. The live workspace mounts the same component (tab-group-offer-api.md).
const f = design.foundation;
const px = (name: string) => parseFloat(f[name as keyof typeof f] as string);
const memoryKey = 'codeaf.desktop.groupOffers.v1';
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

async function openDesignSystem(page: Page) {
  if (page.viewportSize()!.width <= design.breakpoints.small) await page.getByRole('button', { name: 'Show sidebar' }).click();
  await openPage(page, 'Design system');
}
// Axe over the specimen alone: other specimens on the same page are not this feature's to certify.
async function expectSpecimenAccessible(page: Page) {
  await page.evaluate(async () => { await Promise.all(document.getAnimations().filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => undefined))); });
  const result = await new AxeBuilder({ page }).include('.group-offer-specimen').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  expect(result.violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => n.target) }))).toEqual([]);
}

async function openSpecimen(page: Page) {
  await page.goto('/');
  await openDesignSystem(page);
  const sheet = page.locator('.group-offer-specimen');
  await sheet.scrollIntoViewIfNeeded();
  return sheet;
}
const pill = (page: Page) => page.getByRole('group', { name: 'Group suggestion' });

test('the pill is the defined 34px surface with the accent Group button and a quiet X, light and dark', async ({ page }) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    const sheet = await openSpecimen(page);
    const offer = pill(page);
    await expect(offer).toHaveText('Group the 3 tabs as Benchmarks?Group');
    await expect(offer.locator('b')).toHaveText('Benchmarks');
    expect((await offer.boundingBox())!.height).toBe(px('tab-strip-height') - 12);
    await expect(offer).toHaveCSS('border-top-left-radius', '99px');
    await expect(offer).toHaveCSS('font-size', '12px');
    await expect(offer).toHaveCSS('box-shadow', /.+/);
    const accept = offer.getByRole('button', { name: 'Group', exact: true });
    await expect(accept).toHaveCSS('height', '24px');
    const accent = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--accent').trim());
    expect(accent).not.toBe('');
    expect((await accept.evaluate(el => getComputedStyle(el).backgroundColor))).not.toBe('rgba(0, 0, 0, 0)');
    await expect(offer.getByRole('button', { name: 'Dismiss group suggestion' })).toHaveCSS('width', '24px');
    // Centred in its card, below the strip.
    const card = (await sheet.locator('.group-offer-specimen-card').boundingBox())!, box = (await offer.boundingBox())!;
    expect(Math.abs(card.x + card.width / 2 - (box.x + box.width / 2))).toBeLessThan(2);
    expect(box.y - card.y).toBe(px('tab-strip-height') + px('space-compact'));
    await expectSpecimenAccessible(page); await expectNoUnstyledControls(page);
  }
});

test('Group makes one named group through the reducer, and the pill does not come back', async ({ page }) => {
  const sheet = await openSpecimen(page);
  await pill(page).getByRole('button', { name: 'Group', exact: true }).click();
  await expect(pill(page)).toHaveCount(0);
  const capsule = sheet.locator('.workspace-tab-group');
  await expect(capsule).toHaveCount(1);
  await expect(capsule).toContainText('Benchmarks');
  await expect(capsule.getByRole('tab')).toHaveCount(3);
  // Tabs added later from the same session, and a reload, do not bring the question back for a decided set.
  await sheet.getByRole('button', { name: 'Add a tab from this session' }).click();
  await expect(pill(page)).toHaveCount(0);
  expect(await page.evaluate(key => Object.keys(JSON.parse(localStorage.getItem(key) ?? '{}')), memoryKey)).toEqual(['conversation:specimen/group-offer.jsonl']);
  await page.reload();
  await openDesignSystem(page);
  await expect(pill(page)).toHaveCount(0);
});

test('the X changes nothing, is remembered across reload, and Start over forgets it', async ({ page }) => {
  const sheet = await openSpecimen(page);
  await pill(page).getByRole('button', { name: 'Dismiss group suggestion' }).click();
  await expect(pill(page)).toHaveCount(0);
  await expect(sheet.locator('.workspace-tab-group')).toHaveCount(0);
  await expect(sheet.getByRole('tab')).toHaveCount(3);
  await sheet.getByRole('button', { name: 'Add a tab from this session' }).click();
  await expect(pill(page)).toHaveCount(0);
  await page.reload();
  await openDesignSystem(page);
  await expect(pill(page)).toHaveCount(0);
  await page.locator('.group-offer-specimen').getByRole('button', { name: 'Start over' }).click();
  await expect(pill(page)).toHaveCount(1);
});

test('a set that shrinks below three withdraws the pill and is not offered again this launch', async ({ page }) => {
  const sheet = await openSpecimen(page);
  await expect(pill(page)).toHaveCount(1);
  await sheet.getByRole('tab', { name: 'Fixtures' }).hover();
  await sheet.getByRole('button', { name: 'Close Fixtures' }).click();
  await expect(pill(page)).toHaveCount(0);
  await sheet.getByRole('button', { name: 'Add a tab from this session' }).click();
  await expect(sheet.getByRole('tab')).toHaveCount(3);
  await expect(pill(page)).toHaveCount(0);
  // Nothing was decided, so the next launch asks once.
  await page.reload();
  await openDesignSystem(page);
  await expect(pill(page)).toHaveCount(1);
});

test('the pill stays reachable at 320px and never overflows the page', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await openSpecimen(page);
  const box = (await pill(page).boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(320);
  await expect(pill(page).getByRole('button', { name: 'Group', exact: true })).toBeVisible();
  await expect(pill(page).getByRole('button', { name: 'Dismiss group suggestion' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
