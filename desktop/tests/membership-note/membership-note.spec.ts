import { test, expect } from '@playwright/test';
import { installMockPlaces } from '../ui/support/mock-places';

const path = '/tests/membership-note/harness/';
for (const theme of ['light', 'dark']) {
  test(`recorded addition, removal, replay and exact membership Undo ${theme}`, async ({ page }) => {
    const places = await installMockPlaces(page, { places: [{ name: 'Release' }], chats: [{ id: 'chat-1' }] });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto(path);
    const id = places.state().places[0].id;
    const receipts = await page.evaluate(async id => {
      const { createPlacesClient } = await import('/src/features/places/client.ts');
      const receipt = await createPlacesClient().addChats(id, ['chat-1']);
      sessionStorage.setItem('membership-note-fixture', JSON.stringify([
        { text: 'Now also using Release: brand-voice.md', undoReceipts: receipt.undo },
        { text: 'Now also using External' }, { text: '' },
      ]));
      return receipt.undo;
    }, id);
    await page.reload();
    const note = page.getByRole('note').filter({ hasText: 'Release' });
    await expect(note).toHaveText('Now also using Release: brand-voice.md · Undo');
    await expect(page.getByRole('note')).toHaveCount(2);
    await expect(page.getByRole('note').filter({ hasText: 'External' }).getByRole('button')).toHaveCount(0);
    // Theme changes animate shared controls; measure only after that transition settles.
    await expect.poll(() => note.evaluate(el => getComputedStyle(el.querySelector('button')!).color === getComputedStyle(el.querySelector('.membership-note-place')!).color)).toBe(true);
    const styles = await note.evaluate(el => {
      const css = getComputedStyle(el), icon = el.querySelector('.app-icon')!, button = el.querySelector('button')!;
      return { size: css.fontSize, gap: css.gap, leading: css.lineHeight, color: css.color,
        icon: [icon.getBoundingClientRect().width, icon.getBoundingClientRect().height],
        button: { color: getComputedStyle(button).color, fill: getComputedStyle(button).backgroundColor, height: button.getBoundingClientRect().height },
        nameColor: getComputedStyle(el.querySelector('.membership-note-place')!).color };
    });
    expect(styles).toMatchObject({ size: '12px', gap: '8px', leading: 'normal', icon: [12, 12], button: { fill: 'rgba(0, 0, 0, 0)', height: 14 } });
    expect(styles.button.color).toBe(styles.nameColor);
    expect(styles.color).not.toBe(styles.nameColor);
    await page.reload();
    await expect(note).toHaveText('Now also using Release: brand-voice.md · Undo');
    await page.setViewportSize({ width: 320, height: 560 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    await expect(note.getByRole('button')).toBeVisible();
    await page.keyboard.press('Tab');
    await expect(note.getByRole('button', { name: 'Undo', exact: true })).toBeFocused();
    expect(await note.getByRole('button').evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');
    await page.keyboard.press('Enter');
    await expect(note.getByRole('button')).toHaveCount(0);
    expect(places.posts('/undo').at(-1)?.body.receipts).toEqual(receipts);
    expect(places.state().members).toHaveLength(0);
    await expect(note).toHaveText('Now also using Release: brand-voice.md');

    await page.evaluate(async id => {
      const { createPlacesClient } = await import('/src/features/places/client.ts');
      const client = createPlacesClient();
      await client.addChats(id, ['chat-1']);
      const receipt = await client.removeChats(id, ['chat-1']);
      sessionStorage.setItem('membership-note-fixture', JSON.stringify([{ text: 'No longer using Release', undoReceipts: receipt.undo }]));
    }, id);
    await page.reload();
    await page.getByRole('button', { name: 'Undo', exact: true }).click();
    await expect(page.getByRole('note')).toHaveText('No longer using Release');
    await expect.poll(() => places.state().members.length).toBe(1);
    await page.setViewportSize({ width: 320, height: 560 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  });

  test(`pending Undo prevents duplicate writes and refusal stays retryable ${theme}`, async ({ page }) => {
    const places = await installMockPlaces(page);
    await page.addInitScript(value => {
      localStorage.setItem('codeaf-theme', value);
      sessionStorage.setItem('membership-note-fixture', JSON.stringify([{ text: 'No longer using Release', undoReceipts: ['rc_expired'] }]));
    }, theme);
    let release!: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    let calls = 0;
    await page.route('**/api/engine/places/undo', async route => {
      calls++;
      await held;
      await route.fallback();
    });
    await page.goto(path);
    const undo = page.getByRole('button', { name: 'Undo', exact: true });
    await undo.click();
    await expect(undo).toBeDisabled();
    await undo.evaluate(el => { (el as HTMLButtonElement).click(); });
    expect(calls).toBe(1);
    release();
    await expect(page.locator('.toast')).toContainText('can no longer be undone');
    await expect(undo).toBeEnabled();
    expect(places.posts('/undo').at(-1)?.body.receipts).toEqual(['rc_expired']);
    await undo.click();
    await expect.poll(() => calls).toBe(2);
    await expect(undo).toBeEnabled();
  });
}
