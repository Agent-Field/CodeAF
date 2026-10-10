import { expect, test } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces } from './support/mock-places';
import type { PlaceProposal } from '../../src/features/places/proposals-client';

const chats = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];
const create = (count: number): PlaceProposal => ({
  id: 'prop_abcdef0123456789', kind: 'create', status: 'pending', chatIds: chats.slice(0, count),
  name: 'Launch week', reason: '7 chats across Marketing and Release', offerVersion: 'card-v1',
});

async function openAllPlaces(page: import('@playwright/test').Page, proposal: PlaceProposal) {
  await installMockEngine(page, { initial: { entries: [], title: '' }, turns: [] });
  const places = await installMockPlaces(page, {
    places: [{ name: 'Marketing', tint: 'rose' }, { name: 'Release', tint: 'tide' }],
    chats: chats.map(id => ({ id, title: `Chat ${id}` })),
    proposals: [proposal],
  });
  await page.goto('/');
  await page.getByRole('button', { name: /^All places/ }).click();
  await expect(page.getByRole('heading', { name: 'All places' })).toBeVisible();
  return places;
}

test.describe('suggested place card', () => {
  for (const theme of ['light', 'dark'] as const) {
    test(`matches the measured card in ${theme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
      await page.setViewportSize({ width: 1200, height: 800 });
      await openAllPlaces(page, create(7));
      const card = page.getByRole('group', { name: 'Suggested place' });
      await expect(card).toBeVisible();
      await expect(card.getByText('Launch week', { exact: true })).toBeVisible();
      await expect(card.getByText('7 chats across Marketing and Release', { exact: true })).toBeVisible();
      await expect(card.getByRole('button')).toHaveText(['Create', 'Not now']);
      const box = await card.evaluate(element => {
        const style = getComputedStyle(element);
        const probe = document.createElement('div');
        probe.style.background = 'var(--surface)';
        document.body.append(probe);
        const surface = getComputedStyle(probe).backgroundColor;
        probe.remove();
        const title = element.querySelector('.suggestion-title');
        const detail = element.querySelector('.suggestion-detail');
        const kicker = element.querySelector('.suggestion-kicker');
        const icon = element.querySelector('.suggestion-kicker .app-icon');
        const createButton = element.querySelector('.suggestion-action-field');
        const dismiss = element.querySelector('.suggestion-action-quiet');
        return {
          width: element.getBoundingClientRect().width,
          padding: style.padding, radius: style.borderRadius, gap: style.gap, background: style.backgroundColor, surface,
          titleSize: title ? getComputedStyle(title).fontSize : '',
          titleWeight: title ? getComputedStyle(title).fontWeight : '',
          detailLeading: detail ? getComputedStyle(detail).lineHeight : '',
          detailSize: detail ? getComputedStyle(detail).fontSize : '',
          kickerSize: kicker ? getComputedStyle(kicker).fontSize : '',
          icon: icon ? icon.getBoundingClientRect().width : 0,
          createHeight: createButton ? createButton.getBoundingClientRect().height : 0,
          createPad: createButton ? getComputedStyle(createButton).padding : '',
          createRadius: createButton ? getComputedStyle(createButton).borderRadius : '',
          dismissHeight: dismiss ? dismiss.getBoundingClientRect().height : 0,
          dismissPad: dismiss ? getComputedStyle(dismiss).padding : '',
        };
      });
      expect(box.width).toBe(212);
      expect(box.padding).toBe('10px 12px');
      expect(box.radius).toBe('10px');
      expect(box.gap).toBe('8px');
      expect(box.background).toBe(box.surface);
      expect(box.kickerSize).toBe('11px');
      expect(box.icon).toBe(11);
      expect(box.titleSize).toBe('13px');
      expect(box.titleWeight).toBe('500');
      expect(box.detailSize).toBe('12px');
      expect(box.detailLeading).toBe('18px');
      expect(box.createHeight).toBe(24);
      expect(box.dismissHeight).toBe(24);
      expect(box.createPad).toBe('0px 10px');
      expect(box.dismissPad).toBe('0px 8px');
      expect(box.createRadius).toBe('6px');
      const createButton = card.getByRole('button', { name: 'Create' });
      const rest = await createButton.evaluate(element => getComputedStyle(element).backgroundColor);
      await createButton.hover();
      await expect.poll(() => createButton.evaluate(element => getComputedStyle(element).backgroundColor)).not.toBe(rest);
      await createButton.focus();
      await page.keyboard.press('Tab');
      const focused = card.getByRole('button', { name: 'Not now' });
      await expect(focused).toBeFocused();
      const ring = await focused.evaluate(element => getComputedStyle(element).boxShadow);
      expect(ring).not.toBe('none');
    });
  }

  test('Not now hides the card for a reload, and four chats draw no card', async ({ page }) => {
    const places = await openAllPlaces(page, create(7));
    const card = page.getByRole('group', { name: 'Suggested place' });
    await card.getByRole('button', { name: 'Not now' }).click();
    await expect(card).toHaveCount(0);
    expect(places.posts('/decline')).toHaveLength(1);
    await page.reload();
    await page.getByRole('button', { name: /^All places/ }).click();
    await expect(page.getByRole('group', { name: 'Suggested place' })).toHaveCount(0);

    await page.goto('about:blank');
    await openAllPlaces(page, create(4));
    await expect(page.getByRole('group', { name: 'Suggested place' })).toHaveCount(0);
    await expect(page.getByText('Launch week', { exact: true })).toHaveCount(0);
  });

  test('at 320px the card stays in the column and both actions are on screen', async ({ page }) => {
    await openAllPlaces(page, create(7));
    await page.setViewportSize({ width: 320, height: 700 });
    const card = page.getByRole('group', { name: 'Suggested place' });
    await expect(card).toBeVisible();
    await card.scrollIntoViewIfNeeded();
    const box = await card.evaluate(element => {
      const rect = element.getBoundingClientRect();
      return { width: rect.width, left: rect.left, right: rect.right, viewport: window.innerWidth };
    });
    expect(box.width).toBeLessThanOrEqual(212);
    expect(box.width).toBeGreaterThan(0);
    expect(box.left).toBeGreaterThanOrEqual(0);
    expect(box.right).toBeLessThanOrEqual(box.viewport);
    for (const name of ['Create', 'Not now']) await expect(card.getByRole('button', { name })).toBeInViewport();
  });
});
