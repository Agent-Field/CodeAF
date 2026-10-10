import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp } from './support/conversation';

async function render(page: Page, text: string) {
  await installMockEngine(page, plainReply());
  await openApp(page);
  await expect(page.locator('.conversation-scroll > .conversation-column')).toBeAttached();
  await page.evaluate(async (text) => {
    const markdownPath = '/src/components/ui/Markdown.tsx';
    const resources = performance.getEntriesByType('resource').map(entry => entry.name);
    const reactPath = resources.find(url => /\/react\.js\?/.test(url))!;
    const clientPath = resources.find(url => /\/react-dom_client\.js\?/.test(url))!;
    const themePath = '/src/design/ThemeProvider.tsx';
    const [{ Markdown }, { default: React }, { default: ReactDOM }, { ThemeProvider }] = await Promise.all([import(markdownPath), import(reactPath), import(clientPath), import(themePath)]);
    const container = document.createElement('section');
    container.id = 'markdown-specimen';
    container.setAttribute('aria-label', 'Markdown specimen');
    document.querySelector('.conversation-scroll > .conversation-column')!.replaceChildren(container);
    const root = ReactDOM.createRoot(container);
    root.render(React.createElement(ThemeProvider, null, React.createElement(Markdown, null, text)));
  }, text);
  const link = page.locator('#markdown-specimen').getByRole('link', { name: 'source' });
  await expect(link).toBeVisible();
  await link.evaluate(el => el.addEventListener('click', event => event.preventDefault()));
  return link;
}

test('links are accent and underline on hover', async ({ page }) => {
  const link = await render(page, 'Read the [source](https://example.com/reference).');
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(theme => {
      document.documentElement.dataset.theme = theme;
      delete document.documentElement.dataset.input;
    }, theme);
    await link.evaluate(el => { (el as HTMLElement).blur(); });
    await page.mouse.move(0, 0);

    const rest = await link.evaluate(el => {
      const style = getComputedStyle(el);
      const probe = document.createElement('span');
      probe.style.color = 'var(--accent)';
      document.body.append(probe);
      const accent = getComputedStyle(probe).color;
      probe.remove();
      return { color: style.color, accent, line: style.textDecorationLine, background: style.backgroundColor };
    });
    expect(rest.color, theme).toBe(rest.accent);
    expect(rest.line, theme).toBe('none');
    expect(rest.background, theme).toBe('rgba(0, 0, 0, 0)');

    await link.hover();
    const hovered = await link.evaluate(el => {
      const style = getComputedStyle(el);
      return { line: style.textDecorationLine, decoration: style.textDecorationColor, color: style.color, background: style.backgroundColor, shadow: style.boxShadow };
    });
    expect(hovered.line, theme).toBe('underline');
    expect(hovered.decoration, theme).toBe(hovered.color);
    expect(hovered.background, theme).toBe('rgba(0, 0, 0, 0)');
    expect(hovered.shadow, theme).toBe('none');

    await page.mouse.down();
    const pressed = await link.evaluate(el => getComputedStyle(el).backgroundColor);
    await page.mouse.up();
    expect(pressed, theme).toBe('rgba(0, 0, 0, 0)');

    await page.mouse.move(0, 0);
    await link.evaluate(el => {
      const sentinel = document.createElement('button');
      sentinel.type = 'button';
      sentinel.id = 'link-focus-sentinel';
      sentinel.textContent = 'Before link';
      el.before(sentinel);
    });
    await page.locator('#link-focus-sentinel').focus();
    await page.keyboard.press('Tab');
    const focused = await link.evaluate(el => {
      const style = getComputedStyle(el);
      const probe = document.createElement('span');
      probe.style.boxShadow = '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)';
      document.body.append(probe);
      const ring = getComputedStyle(probe).boxShadow;
      probe.remove();
      document.getElementById('link-focus-sentinel')?.remove();
      return { line: style.textDecorationLine, decoration: style.textDecorationColor, color: style.color, shadow: style.boxShadow, ring, visible: el.matches(':focus-visible') };
    });
    expect(focused.visible, theme).toBe(true);
    expect(focused.line, theme).toBe('underline');
    expect(focused.decoration, theme).toBe(focused.color);
    expect(focused.shadow, theme).toBe(focused.ring);
  }
});
