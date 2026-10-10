import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark']) {
  test(`${theme}: inline creation and drag-target cleanup work together`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/?scenario=place');
    const source = page.locator('[data-chat-id="chat_commas"]');
    const first = page.locator('[data-place-id="pl_software"]');
    const second = page.locator('[data-place-id="pl_marketing"]');
    const data = await page.evaluateHandle(() => new DataTransfer());

    await page.getByRole('button', { name: 'New place', exact: true }).click();
    const name = page.getByRole('textbox', { name: 'Place name' });
    await name.fill('Talks');
    await page.getByRole('radio', { name: 'Iris' }).click();
    await source.dispatchEvent('dragstart', { dataTransfer: data });
    await first.dispatchEvent('dragenter', { dataTransfer: data });
    await expect(first).toHaveAttribute('data-drop', 'true');
    await second.dispatchEvent('dragenter', { dataTransfer: data });
    // Browsers can deliver the previous tile's leave after entering its neighbour.
    await first.dispatchEvent('dragleave', { dataTransfer: data });
    await expect(first).not.toHaveAttribute('data-drop', 'true');
    await expect(second).toHaveAttribute('data-drop', 'true');
    await expect(second.locator('.places-tile-drop')).toHaveText('Add here');
    await source.dispatchEvent('dragend', { dataTransfer: data });
    await expect(second).not.toHaveAttribute('data-drop', 'true');
    await expect(name).toHaveValue('Talks');

    await name.press('Enter');
    await expect(page.getByRole('list', { name: 'Callback log' }).locator('li').last()).toHaveText('create:Talks:iris:pl_codeaf');
    await expect(name).toHaveCount(0);
  });
}
