import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { installMockEngine, MODEL, GLM, GLM_FLASH } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openPage } from './support/shell-navigation';

// Browser fixtures only: every role write keeps the same DS model. No provider or profile is contacted.
const models = [{ id: MODEL, name: 'DS Flash', efforts: ['low', 'medium', 'high'] }, { id: GLM, name: 'GLM' }, { id: GLM_FLASH, name: 'GLM Flash' }];
const gate = () => { let release!: () => void; const wait = new Promise<void>(resolve => { release = resolve; }); return { wait, release }; };
const role = (id: string, effort: string) => ({ id, name: id === 'naming' ? 'Chat titles' : 'Summaries', controls: '', category: 'naming', model: MODEL, default: MODEL, chosen: true, live: true, effort });
const open = async (page: Page) => { await page.goto('/'); await openPage(page, 'Settings'); };
const shots = process.env.SETTINGS_SHOTS;
if (shots) mkdirSync(shots, { recursive: true });

for (const theme of ['light', 'dark'] as const) {
  test(`role saves keep the last intent instead of accepting an older response · ${theme}`, async ({ page }, info) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { ...plainReply(), models });
    const held = gate();
    const writes: string[] = [];
    await page.route(/\/models\/roles\/naming$/, async route => {
      const body = route.request().postDataJSON();
      expect(body.model).toBe(MODEL);
      writes.push(body.effort);
      if (body.effort === 'high') await held.wait;
      await route.fulfill({ json: role('naming', body.effort) });
    });
    await open(page);
    const effort = page.getByRole('radiogroup', { name: 'Effort for Chat titles' });
    await effort.getByRole('radio', { name: 'High' }).click();
    await expect.poll(() => writes).toEqual(['high']);
    await effort.getByRole('radio', { name: 'Low' }).click();
    await expect(page.locator('.settings-receipt')).toHaveText('Saving…');
    expect(writes).toEqual(['high']);
    held.release();
    await expect.poll(() => writes).toEqual(['high', 'low']);
    await expect(effort.getByRole('radio', { name: 'Low' })).toHaveAttribute('aria-checked', 'true');
    await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
    if (shots) await page.locator('[data-role=naming]').screenshot({ path: `${shots}/role-low-${theme}-${info.project.name}.png` });
  });

  test(`duplicate Enter and blur share a failure, then the same choice can retry · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { ...plainReply(), models });
    const held = gate();
    let writes = 0;
    const setting = { key: 'minClusterChats', name: 'Chats before a new place is offered', group: 'limits', kind: 'number', min: 3, max: 50, default: 5, value: 5, chosen: false, design: true, explain: 'A fixture choice.', unit: 'chats' };
    await page.route(/\/places\/policy$/, route => route.fulfill({ json: { settings: [setting] } }));
    await page.route(/\/places\/policy\/minClusterChats$/, async route => {
      expect(route.request().postDataJSON()).toEqual({ value: 6 });
      writes++;
      if (writes === 1) { await held.wait; await route.fulfill({ status: 503, json: { error: 'Fixture save unavailable' } }); }
      else await route.fulfill({ json: { ...setting, value: 6, chosen: true } });
    });
    await open(page);
    const input = page.getByRole('spinbutton', { name: setting.name });
    await input.fill('6');
    await input.press('Enter');
    await input.press('Tab');
    await expect.poll(() => writes).toBe(1);
    held.release();
    await expect(page.locator('.settings-receipt')).toContainText('Not saved');
    await expect(input).toHaveValue('5');
    await input.fill('6');
    await input.press('Enter');
    await expect.poll(() => writes).toBe(2);
    await expect(input).toHaveValue('6');
    await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
  });

  test(`an older failed numeric save preserves a newer draft · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { ...plainReply(), models });
    const held = gate();
    let writes = 0;
    const setting = { key: 'minClusterChats', name: 'Chats before a new place is offered', group: 'limits', kind: 'number', min: 3, max: 50, default: 5, value: 5, chosen: false, design: true, explain: 'A fixture choice.', unit: 'chats' };
    await page.route(/\/places\/policy$/, route => route.fulfill({ json: { settings: [setting] } }));
    await page.route(/\/places\/policy\/minClusterChats$/, async route => {
      writes++;
      if (writes === 1) { await held.wait; await route.fulfill({ status: 503, json: { error: 'Fixture save unavailable' } }); }
      else await route.fulfill({ json: { ...setting, value: 7, chosen: true } });
    });
    await open(page);
    const input = page.getByRole('spinbutton', { name: setting.name });
    await input.fill('6');
    await input.press('Enter');
    await expect.poll(() => writes).toBe(1);
    await input.fill('7');
    held.release();
    await expect(page.locator('.settings-receipt')).toContainText('Not saved');
    await expect(input).toHaveValue('7');
    await input.press('Enter');
    await expect.poll(() => writes).toBe(2);
    await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
    await expect(input).toHaveValue('7');
  });

  test(`independent roles save without overwriting each other or claiming all saves finished · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { ...plainReply(), models });
    const held = gate();
    const writes: string[] = [];
    await page.route(/\/models\/roles\/(naming|summaries)$/, async route => {
      const id = route.request().url().split('/').at(-1)!;
      const body = route.request().postDataJSON();
      expect(body.model).toBe(MODEL);
      writes.push(id);
      if (id === 'naming') await held.wait;
      await route.fulfill({ json: role(id, body.effort) });
    });
    await open(page);
    const titles = page.getByRole('radiogroup', { name: 'Effort for Chat titles' });
    const summaries = page.getByRole('radiogroup', { name: 'Effort for Summaries' });
    await titles.getByRole('radio', { name: 'High' }).click();
    await summaries.getByRole('radio', { name: 'Low' }).click();
    await expect(summaries.getByRole('radio', { name: 'Low' })).toHaveAttribute('aria-checked', 'true');
    await expect(page.locator('.settings-receipt')).toHaveText('Saving…');
    expect(writes).toEqual(['naming', 'summaries']);
    held.release();
    await expect(titles.getByRole('radio', { name: 'High' })).toHaveAttribute('aria-checked', 'true');
    await expect(summaries.getByRole('radio', { name: 'Low' })).toHaveAttribute('aria-checked', 'true');
    await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
  });

  test(`queued pinned-slot changes use the last saved list instead of losing the earlier change · ${theme}`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await installMockEngine(page, { ...plainReply(), models });
    const held = gate();
    const writes: string[][] = [];
    await page.route(/\/models\/pinned$/, async route => {
      if (route.request().method() === 'GET') return route.fallback();
      const ids = route.request().postDataJSON().models;
      writes.push(ids);
      if (writes.length === 1) await held.wait;
      await route.fulfill({ json: { chosen: true, pinned: ids.map((id: string) => ({ id, label: models.find(model => model.id === id)!.name })) } });
    });
    await open(page);
    await page.getByRole('button', { name: 'Pinned model 1' }).click();
    await page.getByRole('option', { name: 'DS Flash', exact: true }).click();
    await expect.poll(() => writes.length).toBe(1);
    await page.getByRole('button', { name: 'Pinned model 3' }).click();
    await page.getByRole('option', { name: 'DS Flash', exact: true }).click();
    expect(writes).toEqual([[MODEL, GLM_FLASH, GLM]]);
    held.release();
    await expect.poll(() => writes).toEqual([[MODEL, GLM_FLASH, GLM], [GLM, GLM_FLASH, MODEL]]);
    await expect(page.getByRole('button', { name: 'Pinned model 1' })).toHaveText('GLM');
    await expect(page.getByRole('button', { name: 'Pinned model 2' })).toHaveText('GLM Flash');
    await expect(page.getByRole('button', { name: 'Pinned model 3' })).toHaveText('DS Flash');
  });
}
