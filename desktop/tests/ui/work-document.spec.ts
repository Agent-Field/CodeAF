import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls } from './contracts';

async function sample(page: Page, label = 'Load Pricing research sample') {
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitem', { name: label, exact: true }).click();
}

test('work sections separate result disclosure from literal instruction disclosure', async ({ page }) => {
 await page.goto('/'); await sample(page);
 const title = 'Public pricing table';
 const open = page.getByRole('button', { name: `Open ${title}`, exact: true });
 await expect(open).toHaveAttribute('aria-expanded', 'false');
 await open.click();
 await expect(page.getByRole('button', { name: `Fold ${title}`, exact: true })).toHaveAttribute('aria-expanded', 'true');
 const original = page.getByRole('button', { name: `Show original instruction for ${title}`, exact: true });
 await expect(original).toHaveAttribute('aria-expanded', 'false');
 await expect(page.getByText('Pull everyone’s public pricing into one table. Use monthly list prices.', { exact: true })).not.toBeVisible();
 await original.click();
 await expect(page.getByText('Pull everyone’s public pricing into one table. Use monthly list prices.', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: `Hide original instruction for ${title}`, exact: true })).toHaveAttribute('aria-expanded', 'true');
 await page.getByRole('button', { name: `Hide original instruction for ${title}`, exact: true }).click();
 await expect(original).toHaveAttribute('aria-expanded', 'false');
 await page.getByRole('button', { name: `Fold ${title}`, exact: true }).click();
 await expect(open).toHaveAttribute('aria-expanded', 'false');
 await expectAccessible(page); await expectNoUnstyledControls(page);
});

test('drafts remain isolated across tab switches, reload and document folds', async ({ page }) => {
 await page.goto('/');
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Unsaved work in first tab');
 await page.getByRole('button', { name: 'New tab', exact: true }).first().click();
 await expect(draft).toHaveValue('');
 await draft.fill('Independent second draft');
 await page.getByRole('tab', { name: 'New conversation', exact: true }).click();
 await expect(draft).toHaveValue('Unsaved work in first tab');
 await page.reload(); await expect(draft).toHaveValue('Unsaved work in first tab');
 await sample(page);
 await expect(draft).toHaveValue('Unsaved work in first tab');
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitem', { name: /^Fold all sections/ }).click();
 await expect(page.getByRole('button', { name: 'Open Price bands in EUR', exact: true })).toHaveAttribute('aria-expanded', 'false');
 await expect(draft).toHaveValue('Unsaved work in first tab');
});

for (const theme of ['light', 'dark']) {
 test(`${theme}: document and instruction footer fit narrow windows with reduced motion`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ colorScheme: theme === 'dark' ? 'dark' : 'light', reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/');
  await sample(page);
  const draft = page.getByRole('textbox', { name: /Draft for/ });
  await draft.fill(Array.from({ length: 20 }, (_, index) => `Long instruction line ${index}`).join('\n'));
  await expect(draft).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expectAccessible(page); await expectNoUnstyledControls(page);
 });
}


test('steering is an amendment while primary Enter creates a separate section', async ({ page }) => {
 await page.goto('/'); await sample(page);
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitemcheckbox', { name: 'Preview working state', exact: true }).click();
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Use 40 seats for this estimate'); await draft.press('Enter');
 await expect(page.getByRole('region', { name: 'Work section Price bands in EUR', exact: true }).locator('.work-amendments')).toContainText('Use 40 seats for this estimate');
 await expect(page.getByRole('region', { name: 'Work section Price bands in EUR', exact: true }).locator('.work-amendments')).toContainText('Staged locally · no request made');
 await page.getByRole('button', { name: 'Fold Price bands in EUR', exact: true }).click();
 const section = page.locator('.work-section').filter({ has: page.getByRole('button', { name: 'Open Price bands in EUR', exact: true }) });
 await expect(section).toContainText('amended ×3');
 for (const literal of await page.getByText('Use 40 seats for this estimate', { exact: true }).all()) await expect(literal).not.toBeVisible();
 await draft.fill('Design the next pricing experiment');
 const primary = await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control';
 await draft.press(`${primary}+Enter`);
 await expect(page.getByRole('button', { name: 'Fold Instruction 4', exact: true })).toBeVisible();
 await expect(page.getByText('Design the next pricing experiment', { exact: true })).not.toBeVisible();
 await page.getByRole('button', { name: 'Show original instruction for Instruction 4', exact: true }).click();
 await expect(page.getByText('Design the next pricing experiment', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Open Price bands in EUR', exact: true })).toBeVisible();
 await expect(page.getByRole('region', { name: 'Work document', exact: true })).toContainText('Instruction staged locally; no request made.');
 await expect(draft).toHaveValue('');
});

test('decision controls are explicit samples and preserve an unrelated draft', async ({ page }) => {
 await page.goto('/');
 await expect(page.getByRole('button', { name: 'EUR', exact: true })).not.toBeVisible();
 const draft = page.getByRole('textbox', { name: /Draft for/ }); await draft.fill('Unsent unrelated instruction');
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitemcheckbox', { name: 'Preview decision state', exact: true }).click();
 await expect(page.getByText('Which currency should the table and chart use?', { exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'EUR', exact: true }).click();
 await expect(page.getByRole('status').filter({ hasText: 'Decision recorded' })).toContainText('Decision recorded in this sample: EUR. No request made.');
 await expect(page.getByRole('button', { name: 'EUR', exact: true })).not.toBeVisible();
 await expect(draft).toHaveValue('Unsent unrelated instruction');
 await expectAccessible(page);
});

test('each tab restores document reading position across switching and reload', async ({ page }) => {
 await page.setViewportSize({ width: 800, height: 560 }); await page.goto('/'); await sample(page);
 const document = page.getByRole('region', { name: 'Work document', exact: true });
 for (const title of ['Shortlist of direct competitors', 'Public pricing table']) {
  await page.getByRole('button', { name: `Open ${title}`, exact: true }).click();
  await page.getByRole('button', { name: `Show original instruction for ${title}`, exact: true }).click();
 }
 await document.evaluate(element => { element.scrollTop = element.scrollHeight; });
 await expect.poll(() => document.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
 const position = await document.evaluate(element => element.scrollTop);
 await page.getByRole('button', { name: 'New tab', exact: true }).first().click();
 await expect(document).not.toBeVisible();
 await page.getByRole('tab', { name: 'New conversation', exact: true }).click();
 await expect.poll(() => document.evaluate(element => element.scrollTop)).toBeCloseTo(position, 0);
 await page.reload();
 await expect.poll(() => document.evaluate(element => element.scrollTop)).toBeCloseTo(position, 0);
});
