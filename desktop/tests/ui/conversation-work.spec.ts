import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { HANDOFF_ANSWER, PNG_1X1, handoffReply, richReply } from './support/scenarios-v2';
import { message, openApp, posts, send } from './support/conversation';

const gets = (engine: MockEngine, part: string) => engine.calls.filter((c) => c.method === 'GET' && c.path.includes(part));

async function openRich(page: Page): Promise<MockEngine> {
  const engine = await installMockEngine(page, richReply());
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
  return engine;
}

test('an interim update is set off from the final answer', async ({ page }) => {
  await openRich(page);
  const update = page.locator('.update-block');
  await expect(update).toContainText('Update');
  await expect(update).toContainText('Found it: the test reads the real clock.');
  await expect(update.getByRole('button', { name: 'Copy answer' })).toHaveCount(0);
  const answer = page.locator('.answer-block');
  await expect(answer).toHaveCount(1);
  await expect(answer).toContainText('the suite passes');
});

test('work folds to one line, opens to titled steps, an edit shows its diff and a command its terminal', async ({ page }) => {
  const engine = await openRich(page);
  const summary = page.getByRole('button', { name: /^Worked \d+s · 2 steps · 5 calls$/ });
  await expect(summary).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('button', { name: /^Reading the login test/ })).toHaveCount(0);
  await summary.click();
  await expect(summary).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('button', { name: /^Reading the login test/ })).toBeVisible();
  const step = page.getByRole('button', { name: /^Pinning the clock/ });
  await step.click();
  await expect(step).toHaveAttribute('aria-expanded', 'true');

  await page.getByRole('button', { name: 'edit internal/auth/auth_test.go' }).click();
  const diff = page.getByRole('group', { name: 'Changes' });
  await expect(diff.locator('[data-kind="remove"]')).toContainText('now := time.Now()');
  await expect(diff.locator('[data-kind="add"]')).toContainText('now := fixedClock()');

  await page.getByRole('button', { name: /^bash / }).click();
  const terminal = page.getByLabel('Terminal');
  await expect(page.locator('.work-call-text[data-mono]')).toContainText('$ go test ./internal/auth');
  await expect(terminal).not.toContainText('$ go test ./internal/auth');
  await expect(terminal).not.toContainText('line 60: test output');
  await page.getByRole('button', { name: 'Show full output' }).click();
  await expect(terminal).toContainText('line 60: test output');
  expect(gets(engine, '/tools/b1')).toHaveLength(1);
});

test('a workspace path becomes a file chip that previews through the engine; a bare URL becomes a link chip', async ({ page }) => {
  const engine = await openRich(page);
  const answer = page.locator('.answer-block');
  const chip = answer.getByRole('button', { name: 'auth_test.go' });
  await expect(chip).toBeVisible();
  expect(posts(engine, '/files/stat').length).toBeGreaterThan(0);
  await chip.click();
  const preview = page.getByRole('dialog', { name: 'Preview of auth_test.go' });
  await expect(preview).toContainText('now := fixedClock()');
  expect(gets(engine, '/files').some((c) => c.path.endsWith('/files'))).toBe(true);
  await preview.getByRole('button', { name: 'Close preview' }).click();
  await expect(preview).toHaveCount(0);

  const link = answer.locator('a[href="https://pkg.go.dev/time"]');
  await expect(link).toHaveClass(/link-chip/);
  await expect(link.locator('.site-icon-mono')).toHaveText('G');
  await expect(link).toContainText('pkg.go.dev');
});

test('a generated image is promoted above the answer, read through the engine', async ({ page }) => {
  const engine = await openRich(page);
  const figure = page.getByRole('group', { name: 'a small lighthouse logo' });
  await expect(figure).toContainText('1024×1024 png');
  await expect(figure.locator('img')).toHaveAttribute('src', /^data:image\/png;base64,/);
  expect(engine.calls.some((c) => c.path.endsWith('/files'))).toBe(true);
  const order = await page.locator('.turn-block').evaluateAll((blocks) => blocks.map((b) => b.getAttribute('data-kind')));
  expect(order.indexOf('deliverable')).toBeLessThan(order.indexOf('answer'));
  await expect(page.getByRole('button', { name: /^Changed 1 file/ })).toBeVisible();
});

test('a picture sent with the message goes to the engine and shows in the bubble', async ({ page }) => {
  const engine = await installMockEngine(page, richReply());
  await openApp(page);
  await message(page).fill('Fix the flaky login test and draw a logo');
  await page.getByTestId('composer-file-input').setInputFiles([{ name: 'failure.png', mimeType: 'image/png', buffer: Buffer.from(PNG_1X1, 'base64') }]);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  const body = posts(engine, '/turn')[0].body as { text: string; files: { name: string; dataBase64: string }[] };
  expect(body.text).toBe('Fix the flaky login test and draw a logo');
  expect(body.files.map((f) => [f.name, f.dataBase64])).toEqual([['failure.png', PNG_1X1]]);
  const thumb = page.locator('.user-message').getByRole('button', { name: 'Open image: .codeaf/attachments/failure.png' });
  await expect(thumb.locator('img')).toHaveAttribute('src', /^data:image\/png;base64,/);
});

test('a handed-off read shows its answer and a target, never raw JSON or the engine hand-off notes', async ({ page }) => {
  await installMockEngine(page, handoffReply());
  await openApp(page);
  await send(page, 'Summarise the sandbox');
  await expect(page.getByText('The sandbox is a scratch workspace.')).toBeVisible();
  await page.getByRole('button', { name: /^Worked \d+s/ }).click();
  await page.getByRole('button', { name: /^Reading the sandbox/ }).click();
  await page.getByRole('button', { name: /^read .*README\.md$/ }).click();
  await expect(page.locator('pre[aria-label="Excerpt"]')).toContainText(HANDOFF_ANSWER);
  await page.getByRole('button', { name: /^ls .*notes$/ }).click();
  await page.getByRole('button', { name: /^mystery probe/ }).click();

  const detail = page.locator('.work-detail');
  await expect(page.getByText('Read together with the call above.')).toHaveCount(1);
  for (const gone of ['handed to quick task', 'Covered by the handoff', '"path":', '{']) {
    await expect(detail.filter({ hasText: gone })).toHaveCount(0);
  }
  // A tool the row cannot name keeps its extra input, as plain words.
  await expect(page.locator('pre[aria-label="Input"]')).toHaveText('path: /home/santosh/sandbox/notes\ndepth: 2');
});
