import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls } from './contracts';

async function render(page: Page, text: string) {
 await page.goto('/');
 const draft = page.getByRole('textbox', { name: /Draft for/ });
 await draft.fill('Markdown renderer specimen'); await draft.press('Enter');
 await expect(page.locator('.work-document')).toHaveAttribute('data-fresh', 'false');
 await page.evaluate(async (text) => {
  const markdownPath = '/src/components/ui/Markdown.tsx';
  const reactPath = '/node_modules/.vite/deps/react.js';
  const clientPath = '/node_modules/.vite/deps/react-dom_client.js';
  const themePath = '/src/design/ThemeProvider.tsx';
  const [{ Markdown }, { default: React }, { default: ReactDOM }, { ThemeProvider }] = await Promise.all([import(markdownPath), import(reactPath), import(clientPath), import(themePath)]);
  const container = document.createElement('section'); container.id = 'markdown-specimen'; container.setAttribute('aria-label', 'Markdown specimen');
  document.querySelector('.work-document-scroll')!.replaceChildren(container);
  const root = ReactDOM.createRoot(container);
  root.render(React.createElement(ThemeProvider, null, React.createElement(Markdown, null, text)));
  (window as unknown as { updateMarkdown: (text: string) => void }).updateMarkdown = (text: string) => root.render(React.createElement(ThemeProvider, null, React.createElement(Markdown, null, text)));
 }, text);
 return page.locator('#markdown-specimen');
}

test('assistant Markdown renders GFM structure with the shared typography and theme', async ({ page }) => {
 const prose = await render(page, '# Engine review\n\nA **clear** result with `code`.\n\n- First result\n- Second result\n\n1. Inspect\n2. Verify\n\n> A quoted observation.\n\n| Item | Result |\n| --- | --- |\n| Engine | Shared |\n\n- [x] Verified\n- [ ] Pending\n\n```go\nfunc main() {}\n```\n\n[Source](https://example.com/reference)');
 await expect(prose.getByRole('heading', { name: 'Engine review' })).toBeVisible();
 await expect(prose.locator('strong')).toHaveText('clear');
 await expect(prose.locator('table th')).toHaveText(['Item', 'Result']);
 await expect(prose.locator('table td')).toHaveText(['Engine', 'Shared']);
 await expect(prose.getByRole('region', { name: 'Response table' })).toHaveAttribute('tabindex', '0');
 await expect(prose.locator('pre code')).toHaveText('func main() {}\n');
 await expect(prose.getByRole('img', { name: 'Completed item' })).toHaveAttribute('data-checked', 'true');
 await expect(prose.getByRole('img', { name: 'Incomplete item' })).toHaveAttribute('data-checked', 'false');
 await expect(prose.getByRole('link', { name: 'Source' })).toHaveAttribute('rel', 'noopener noreferrer');
 for (const theme of ['light', 'dark']) {
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
  const styles = await prose.locator('.markdown').evaluate(el => ({ color: getComputedStyle(el).color, expected: (() => { const probe = document.createElement('span'); probe.style.color = 'var(--text)'; document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color; })(), family: getComputedStyle(el).fontFamily, rootFamily: getComputedStyle(document.documentElement).getPropertyValue('--font-sans').trim() }));
  expect(styles.color).toBe(styles.expected); expect(styles.family).toContain('system-ui');
 }
 await expectAccessible(page); await expectNoUnstyledControls(page);
});

test('HTML and unsafe links stay inert and images do not fetch remote content', async ({ page }) => {
 const remote: string[] = []; page.on('request', request => { if (request.url().includes('remote-image.example')) remote.push(request.url()); });
 const prose = await render(page, '<script>window.markdownExecuted = true</script>\n\n[Unsafe](javascript:alert(1))\n\n[Data](data:text/html,hello)\n\n[Relative](./private-file)\n\n[Safe](https://example.com)\n\n![Diagram](https://remote-image.example/chart.png)\n\n<iframe src="https://example.com"></iframe>');
 await expect(prose.getByText('Unsafe', { exact: true })).toBeVisible();
 await expect(prose.getByRole('link', { name: 'Unsafe' })).not.toBeVisible();
 await expect(prose.getByRole('link', { name: 'Data' })).not.toBeVisible();
 await expect(prose.getByRole('link', { name: 'Relative' })).not.toBeVisible();
 await expect(prose.getByRole('link', { name: 'Safe' })).toHaveAttribute('href', 'https://example.com');
 await expect(prose.getByRole('link', { name: 'Image: Diagram' })).toBeVisible();
 await expect(prose.locator('script,iframe,img')).toHaveCount(0);
 expect(await page.evaluate(() => 'markdownExecuted' in window)).toBe(false);
 expect(remote).toHaveLength(0);
});

test('partial streaming Markdown and long content fit a narrow document', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 560 });
 const prose = await render(page, 'Starting **a result\n\n```ts\nconst veryLongIdentifier = "' + 'x'.repeat(300) + '";');
 await expect(prose).toContainText('Starting');
 await expect(prose.locator('pre code')).toContainText('veryLongIdentifier');
 const complete = '## Result\n\n**Finished**\n\n| ' + 'Column'.repeat(20) + ' | Second |\n| --- | --- |\n| ' + 'value'.repeat(80) + ' | done |\n\n```ts\nconst longValue = "' + 'x'.repeat(300) + '";\n```';
 await page.evaluate(text => (window as unknown as { updateMarkdown: (text: string) => void }).updateMarkdown(text), complete);
 await expect(prose.locator('strong')).toHaveText('Finished');
 await expect(prose.locator('table')).toBeVisible();
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 const bounds = await prose.boundingBox(); expect(bounds!.width).toBeLessThanOrEqual(320);
 expect(await prose.locator('pre').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});


test('document headings and explicit code use semantic hierarchy and centralized type tokens', async ({ page }) => {
 const prose = await render(page, '# Objective\n\nReadable prose with `src/main.ts`.\n\n## Workstream\n\n### Task\n\n```sh\nnpm run check\n```');
 const tokens = await page.evaluate(() => { const style = getComputedStyle(document.documentElement); return Object.fromEntries(['font-size-prose', 'font-size-code', 'font-size-document-h1', 'font-size-document-h2', 'font-size-document-h3'].map(name => [name, style.getPropertyValue(`--${name}`).trim()])); });
 for (const [level, title] of [[1, 'Objective'], [2, 'Workstream'], [3, 'Task']] as const) {
  const heading = prose.getByRole('heading', { level, name: title, exact: true });
  await expect(heading).toBeVisible(); await expect(heading).toHaveCSS('font-size', tokens[`font-size-document-h${level}`]);
 }
 await expect(prose.locator('.markdown > p')).toHaveCSS('font-size', tokens['font-size-prose']);
 for (const code of await prose.locator('code').all()) {
  await expect(code).toHaveCSS('font-size', tokens['font-size-code']);
  await expect(code).toHaveCSS('font-weight', '400');
  expect(await code.evaluate(el => getComputedStyle(el).fontFamily)).toContain('monospace');
 }
 await expect(prose.locator('code').first()).toHaveText('src/main.ts');
 await expect(prose.locator('pre code')).toHaveText('npm run check\n');
});

test('literal user instructions preserve backticks, Markdown and HTML as original words', async ({ page }) => {
 await page.goto('/');
 const original = 'Keep `src/main.ts` literal, **not bold**, and <b>not HTML</b>.';
 const draft = page.getByRole('textbox', { name: /Draft for/ }); await draft.fill(original); await draft.press('Enter');
 await page.getByRole('button', { name: 'Show original instruction for Instruction 1', exact: true }).click();
 const provenance = page.getByRole('group', { name: 'Original instructions for Instruction 1', exact: true });
 await expect(provenance).toHaveText(original);
 await expect(provenance.locator('code,strong,b,.markdown')).toHaveCount(0);
 const size = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--font-size-prose').trim());
 await expect(provenance.locator('p')).toHaveCSS('font-size', size);
});
