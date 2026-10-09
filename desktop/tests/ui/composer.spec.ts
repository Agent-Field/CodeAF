import { test, expect } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, expectThemedSurface } from './contracts';

test('instruction line preserves multiline and IME input while presets remain tab scoped', async ({ page }) => {
 await page.goto('/');
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await expect(page.getByRole('button', { name: 'Send', exact: true })).not.toBeVisible();
 await draft.fill('An unfinished instruction');
 await draft.press('End'); await draft.press('Shift+Enter');
 await expect(draft).toHaveValue('An unfinished instruction\n');
 await draft.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true });
 await expect(draft).toHaveValue('An unfinished instruction\n');
 await page.getByRole('button', { name: 'Model preset: Auto. Click to cycle.', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Model preset: Fast. Click to cycle.', exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'New tab', exact: true }).first().click();
 await expect(draft).toHaveValue('');
 await draft.fill('A second tab instruction');
 await expect(page.getByRole('button', { name: 'Model preset: Auto. Click to cycle.', exact: true })).toBeVisible();
 await page.getByRole('tab', { name: 'New conversation', exact: true }).click();
 await expect(draft).toHaveValue('An unfinished instruction\n');
 await expect(page.getByRole('button', { name: 'Model preset: Fast. Click to cycle.', exact: true })).toBeVisible();
 await page.reload();
 await expect(draft).toHaveValue('An unfinished instruction\n');
 await expect(page.getByRole('button', { name: 'Model preset: Fast. Click to cycle.', exact: true })).toBeVisible();
 await expectAccessible(page);
});

test('model preset and advanced picker preserve draft and restore focus', async ({ page }) => {
 await page.goto('/'); const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Keep this instruction unchanged');
 const trigger = page.getByRole('button', { name: 'Choose model preset', exact: true });
 await trigger.click();
 await expectThemedSurface(page, page.getByRole('menu', { name: 'Choose model preset', exact: true }));
 await page.getByRole('menuitemcheckbox', { name: 'Thorough · Deeper reasoning · preview', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Model preset: Thorough. Click to cycle.', exact: true })).toBeVisible();
 await trigger.click(); await page.getByRole('menuitem', { name: 'Choose model & effort…', exact: true }).click();
 const picker = page.getByRole('dialog', { name: 'Choose preview model', exact: true });
 await expect(picker).toBeVisible(); await expectThemedSurface(page, picker);
 await page.getByRole('textbox', { name: 'Search models', exact: true }).fill('fast');
 await page.getByRole('button', { name: 'Use Fast', exact: true }).click();
 await expect(draft).toBeFocused(); await expect(draft).toHaveValue('Keep this instruction unchanged');
 await expectAccessible(page); await expectNoUnstyledControls(page);
});

test('large pasted context remains inspectable and removable without losing the draft', async ({ page }) => {
 await page.goto('/'); const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Compare this document');
 const pasted = 'A preserved reference paragraph with context. '.repeat(40);
 await draft.evaluate((element, text) => {
  const transfer = new DataTransfer(); transfer.setData('text/plain', text);
  element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }));
 }, pasted);
 await expect(draft).toHaveValue('Compare this document');
 await page.getByRole('button', { name: /^Inspect Pasted text/ }).click();
 const dialog = page.getByRole('dialog', { name: 'Attached context preview', exact: true });
 await expect(dialog).toContainText(pasted.trim());
 await page.keyboard.press('Escape'); await expect(dialog).not.toBeVisible();
 await page.getByRole('button', { name: /^Remove Pasted text/ }).click();
 await expect(page.getByRole('button', { name: /^Inspect Pasted text/ })).not.toBeVisible();
 await expect(draft).toHaveValue('Compare this document');
 await expectAccessible(page);
});

test('queue and stop remain explicit and preserve unsent instructions', async ({ page }) => {
 await page.goto('/');
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitemcheckbox', { name: 'Preview working state', exact: true }).click();
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Follow-up for later');
 await page.getByRole('button', { name: 'Submission actions', exact: true }).click();
 await page.getByRole('menuitem', { name: /^Queue after this turn/ }).click();
 await expect(page.getByLabel('Queued preview messages', { exact: true })).toContainText('Follow-up for later');
 await expect(draft).toHaveValue('');
 await draft.fill('Preserve this unfinished correction');
 await page.getByRole('button', { name: 'Stop', exact: true }).click();
 await expect(draft).toHaveValue('Preserve this unfinished correction');
 await expect(page.getByLabel('Queued preview messages', { exact: true })).toContainText('Follow-up for later');
 await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
 await expectAccessible(page);
});

test('Escape closes the model menu before stopping working preview', async ({ page }) => {
 await page.goto('/');
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitemcheckbox', { name: 'Preview working state', exact: true }).click();
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Preserve this steering draft');
 await page.getByRole('button', { name: 'Choose model preset', exact: true }).click();
 await expect(page.getByRole('menu', { name: 'Choose model preset', exact: true })).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(page.getByRole('menu', { name: 'Choose model preset', exact: true })).not.toBeVisible();
 await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
 await draft.focus(); await draft.press('Escape');
 await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
 await expect(draft).toHaveValue('Preserve this steering draft');
});

test('instruction footer reveals suggestions and tools only when needed', async ({ page }) => {
 await page.goto('/');
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Load Pricing research sample', exact: true }).click();
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 const document = page.getByRole('region', { name: 'Work document', exact: true });
 await document.focus();
 await expect(draft).toHaveAttribute('placeholder', 'Break down by plan tier');
 await expect(page.getByRole('button', { name: 'Choose model preset', exact: true })).not.toBeVisible();
 await expect(page.getByRole('button', { name: 'Attach', exact: true })).not.toBeVisible();
 await expect(page.getByRole('button', { name: 'Break down by plan tier', exact: true })).not.toBeVisible();
 await draft.focus();
 await expect(page.getByRole('button', { name: 'Break down by plan tier', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Choose model preset', exact: true })).not.toBeVisible();
 await expect(page.getByRole('button', { name: 'Attach', exact: true })).not.toBeVisible();
 await draft.press('Tab');
 await expect(draft).toHaveValue('Break down by plan tier');
 await expect(draft).toBeFocused();
 await expect(page.getByRole('button', { name: 'Choose model preset', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Attach', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Continue', exact: true })).toHaveClass(/button-primary/);
 await expect(page.getByRole('button', { name: 'New section', exact: true })).toHaveClass(/button-secondary/);
 await expectAccessible(page);
});

test('first submission docks immediately when reduced motion is requested', async ({ page }) => {
 await page.emulateMedia({ reducedMotion: 'reduce' }); await page.goto('/');
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Start this work');
 const before = (await page.locator('.work-document-dock').boundingBox())!.y;
 await page.evaluate(() => {
  const state = window as typeof window & { dockAnimationRequests: number };
  state.dockAnimationRequests = 0;
  const animate = Element.prototype.animate;
  Element.prototype.animate = function (...args: Parameters<typeof animate>) {
   if (this.matches('.work-document-dock')) state.dockAnimationRequests++;
   return animate.apply(this, args);
  };
 });
 await draft.press('Enter');
 await expect(page.getByRole('button', { name: 'Fold Instruction 1', exact: true })).toBeVisible();
 await expect.poll(async () => (await page.locator('.work-document-dock').boundingBox())!.y).toBeGreaterThan(before);
 expect(await page.evaluate(() => (window as typeof window & { dockAnimationRequests: number }).dockAnimationRequests)).toBe(0);
 await expect(draft).toHaveValue('');
 await expectAccessible(page);
});
