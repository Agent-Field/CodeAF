import { expect, test, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { pendingQuestion, plainReply } from './support/scenarios';

async function openSplit(page: Page, scenario: Scenario) {
  const engine = await installMockEngine(page, scenario);
  await page.addInitScript(() => {
    const pane = (id: string) => ({ id, kind: 'conversation', title: id, draft: '', titleSource: 'manual', sessionFile: `${id}.jsonl` });
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
      tabs: [{ id: 'split', title: 'Split', kind: 'conversation', draft: '', pinned: false, split: { layout: '1x2', focus: 0, panes: [pane('Left'), pane('Right')] } }],
      groups: [], closed: [], activeId: 'split', nextNumber: 2, recentIds: ['split'],
    }));
  });
  await page.goto('/');
  return { pane: page.locator('.workspace-pane').nth(1), engine };
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme, reducedMotion: 'reduce' });

    test('CV-160: mini-tray matches C-SPLIT-4 and Review opens the focused tray', async ({ page }) => {
      const { pane, engine } = await openSplit(page, pendingQuestion());
      const card = pane.locator('.pane-mini-tray');
      await expect(card).toBeVisible();
      await expect(card.locator('.pane-mini-tray-title')).toHaveText('Pick a database');
      const measured = await card.evaluate(el => {
        const style = getComputedStyle(el);
        const title = getComputedStyle(el.querySelector('.pane-mini-tray-title')!);
        const dot = getComputedStyle(el.querySelector('.pane-mini-tray-dot')!);
        const button = getComputedStyle(el.querySelector('button')!);
        return { height: el.getBoundingClientRect().height, radius: style.borderRadius, shadow: style.boxShadow,
          
          weight: title.fontWeight, titleColor: title.color, color: style.color, mask: title.maskImage,
          dotWidth: dot.width, dotHeight: dot.height, buttonHeight: button.height, buttonRadius: button.borderRadius };
      });
      expect(measured).toMatchObject({ height: 38, radius: '12px', weight: '500', dotWidth: '6px', dotHeight: '6px', buttonHeight: '26px', buttonRadius: '7px' });
      expect(measured.shadow).not.toBe('none');
      expect(measured.titleColor).toBe(measured.color);
      expect(measured.mask).toContain('linear-gradient');
      const field = pane.getByRole('textbox', { name: 'Reply to Right' });
      const [cardBox, fieldBox] = await Promise.all([card.boundingBox(), field.boundingBox()]);
      expect(cardBox!.y + cardBox!.height).toBeLessThan(fieldBox!.y);
      await card.getByRole('button', { name: 'Review' }).click();
      await expect(pane).toHaveAttribute('data-focused', 'true');
      await expect(pane.getByRole('region', { name: 'Waiting on you' })).toBeVisible();
      await expect(card).toHaveCount(0);
      expect(engine.calls.filter(call => call.path.endsWith('/answer'))).toHaveLength(0);
    });

    for (const withdrawn of [false, true]) {
      test(`CV-160: ${withdrawn ? 'withdrawn questions' : 'no questions'} leaves no mini-tray`, async ({ page }) => {
        const scenario = plainReply();
        if (withdrawn) scenario.initial.questions = pendingQuestion().initial.questions!.map(q => ({ ...q, withdrawn: true }));
        const { pane } = await openSplit(page, scenario);
        await expect(pane.getByRole('textbox', { name: 'Reply to Right' })).toBeVisible();
        await expect(pane.locator('.pane-mini-tray')).toHaveCount(0);
      });
    }

    for (const input of ['pointer', 'keyboard'] as const) {
      test(`CV-160: ${input} Review restores a tray after scrolling away`, async ({ page }) => {
        const scenario = pendingQuestion();
        scenario.initial.entries!.push({ Role: 'assistant', Answer: true, Text: Array.from({ length: 100 }, (_, i) => `Paragraph ${i}: a long engine answer to keep the reader away from the bottom.`).join('\n\n') });
        const { pane } = await openSplit(page, scenario);
        const review = pane.locator('.pane-mini-tray').getByRole('button', { name: 'Review' });
        await expect(review).toBeVisible();
        await pane.locator('.conversation-scroll').evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')); });
        await expect(pane.locator('.conversation-scroll')).not.toHaveAttribute('data-scrolled');
        if (input === 'pointer') await review.click();
        else { await page.keyboard.press('Tab'); await review.focus(); await page.keyboard.press('Enter'); }
        await expect(pane).toHaveAttribute('data-focused', 'true');
        await expect(pane.getByRole('region', { name: 'Waiting on you' })).toBeVisible();
        await expect(pane.locator('.pane-mini-tray')).toHaveCount(0);
      });
    }
  });
}
