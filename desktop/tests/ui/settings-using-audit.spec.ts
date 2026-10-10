import { writeFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp } from './support/conversation';

// This review records computed evidence without making today's design gaps permanent test expectations.
for (const theme of ['light', 'dark'] as const) {
  test(`Using audit records source policy and geometry · ${theme}`, async ({ page }, info) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    await page.goto('/tests/using/harness/index.html?scenario=full');
    const chip = page.getByRole('button', { name: 'Using 3 places · 5 sources' });
    await chip.click();
    const sheet = page.getByRole('dialog', { name: 'What this conversation is using' });
    await expect(sheet.getByRole('list', { name: 'Sources refused' })).toContainText('credentials');
    await expect(sheet.getByRole('list', { name: 'Sources refused' }).getByRole('button')).toHaveCount(0);
    await expect(sheet.getByRole('list', { name: 'Sources the model reads' })).toContainText('Release · added by the AI');
    await expect(sheet.locator('[data-field="permissions"]')).toContainText('Running on Ask now.');
    const measurements = await page.evaluate(() => {
      const read = (selector: string) => {
        const element = document.querySelector(selector)!;
        const style = getComputedStyle(element);
        const rect = element.getBoundingClientRect();
        return { width: rect.width, height: rect.height, color: style.color, background: style.backgroundColor, radius: style.borderRadius, fontSize: style.fontSize, padding: style.padding, boxShadow: style.boxShadow };
      };
      return { chip: read('.using-chip'), sheet: read('.using-sheet'), refusal: read('[data-status="refused"] .using-source-note'), ordinaryNote: read('.using-note'), policy: read('.using-policy-title') };
    });
    expect(measurements.chip.height).toBe(24);
    writeFileSync(info.outputPath('computed-styles.json'), JSON.stringify(measurements, null, 2));
    await info.attach('computed-styles', { body: JSON.stringify(measurements, null, 2), contentType: 'application/json' });
    await page.screenshot({ path: info.outputPath(`using-${theme}.png`) });
    for (const width of [320, 800]) {
      await page.setViewportSize({ width, height: 700 });
      await chip.click();
      await expect(sheet).toBeVisible();
      const rect = (await sheet.boundingBox())!;
      expect(rect.x).toBeGreaterThanOrEqual(0);
      expect(rect.x + rect.width).toBeLessThanOrEqual(width);
      expect(await sheet.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath(`using-${theme}-${width}.png`) });
    }
    await page.keyboard.press('Escape');
    await expect(chip).toBeFocused();
    expect(await page.evaluate(() => window.__usingCalls.every(call => call.kind === 'using'))).toBe(true);
  });

  test(`Working folder audit records successful selection and fallback · ${theme}`, async ({ page }, info) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const engine = await installMockEngine(page, { initial: {
      workspace: '/work/fixture-project', workingFolder: { from: 'place', path: '/work/fixture-project', label: 'fixture-project' },
      entries: [{ Role: 'user', Text: 'Read the project.' }, { Role: 'assistant', Text: 'The project is ready.', Answer: true }],
    } });
    await openApp(page);
    // The first-send door attaches this mock session without making a provider call.
    await page.getByRole('textbox', { name: 'Message', exact: true }).fill('Show the folder.');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.locator('.conversation-view')).toContainText('The project is ready.');
    const selected = await page.locator('.conversation-view').innerText();
    writeFileSync(info.outputPath('working-folder-selection.json'), JSON.stringify({ engine: engine.snapshot().workingFolder, visibleFolder: selected.includes('fixture-project') }, null, 2));
    await info.attach('working-folder-selection', { body: JSON.stringify({ engine: engine.snapshot().workingFolder, visibleFolder: selected.includes('fixture-project') }, null, 2), contentType: 'application/json' });
    await page.screenshot({ path: info.outputPath(`working-folder-selected-${theme}.png`) });
    const note = "The place's folders can't be used, so this chat works where codeaf was started.";
    engine.update({ workingFolder: { from: 'launch', note } });
    await expect(page.getByRole('status').filter({ hasText: note })).toBeVisible();
    await page.screenshot({ path: info.outputPath(`working-folder-fallback-${theme}.png`) });
  });
}
