import { test, expect, type Locator } from '@playwright/test';

/** A point on the row the cursor can actually press. The Home dock covers the middle of a chat, and a row below the fold is not under the cursor until it is scrolled into view. */
async function pointOn(locator: Locator) {
  const find = () => locator.evaluate(el => {
    const r = el.getBoundingClientRect();
    for (let y = r.top + 2; y < r.bottom - 1; y += 4) {
      for (const x of [r.left + 16, r.left + r.width / 2]) {
        const top = document.elementsFromPoint(x, y)[0];
        if (top && (top === el || el.contains(top))) return { x, y };
      }
    }
    return null;
  });
  let point = await find();
  if (!point) {
    await locator.evaluate(el => el.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
    point = await find();
  }
  if (!point) throw new Error('no point on the dragged row is under the cursor');
  return point;
}

for (const theme of ['light', 'dark']) {
  test(`${theme}: inline creation and drag-target cleanup work together`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=place');
    const source = page.locator('[data-chat-id="chat_commas"]');
    const first = page.locator('[data-place-id="pl_software"]');
    const second = page.locator('[data-place-id="pl_marketing"]');
    await page.getByRole('button', { name: 'New place', exact: true }).click();
    const name = page.getByRole('textbox', { name: 'Place name' });
    await name.fill('Talks');
    await page.getByRole('radio', { name: 'Iris' }).click();
    const start = await pointOn(source);
    const firstBox = await pointOn(first);
    const secondBox = await pointOn(second);
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(start.x + 8, start.y + 4, { steps: 3 });
    await page.mouse.move(firstBox.x, firstBox.y, { steps: 6 });
    await expect(first).toHaveAttribute('data-drop', 'true');
    await page.mouse.move(secondBox.x, secondBox.y, { steps: 6 });
    await expect(first).not.toHaveAttribute('data-drop', 'true');
    await expect(second).toHaveAttribute('data-drop', 'true');
    await expect(second.locator('.places-tile-drop')).toHaveText('Add here');
    await page.keyboard.press('Escape');
    await page.mouse.up();
    await expect(second).not.toHaveAttribute('data-drop', 'true');
    await expect(name).toHaveValue('Talks');

    await name.press('Enter');
    await expect(page.getByRole('list', { name: 'Callback log' }).locator('li').last()).toHaveText('create:Talks:iris:pl_codeaf');
    await expect(name).toHaveCount(0);
  });
}
