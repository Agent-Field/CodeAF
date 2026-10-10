import { test, expect } from '@playwright/test';

test('useJobs shares the world stream and follows session changes', async ({ page }) => {
  await page.route('**/jobs-client-contract', route => route.fulfill({ contentType: 'text/html', body: '<html><body><div id="root"></div></body></html>' }));
  await page.goto('/jobs-client-contract');
  await page.evaluate(async () => {
    const refreshPath = '/@react-refresh';
    const { default: RefreshRuntime } = await import(/* @vite-ignore */ refreshPath);
    RefreshRuntime.injectIntoGlobalHook(window);
    Object.assign(window, { $RefreshReg$: () => {}, $RefreshSig$: () => (type: unknown) => type, __vite_plugin_react_preamble_installed__: true });
    const original = window.fetch;
    const paths: string[] = [];
    let controller: ReadableStreamDefaultController<Uint8Array>;
    window.fetch = async (url, init) => {
      if (!String(url).includes('/api/engine/')) return original(url, init);
      paths.push(String(url));
      return new Response(new ReadableStream<Uint8Array>({ start(c) {
        controller = c;
        init?.signal?.addEventListener('abort', () => c.close(), { once: true });
      } }), { headers: { 'Content-Type': 'text/event-stream' } });
    };
    const reactPath = '/.vite-cache/deps/react.js';
    const domPath = '/.vite-cache/deps/react-dom_client.js';
    const clientPath = '/src/features/jobs/client.ts';
    const { default: React } = await import(/* @vite-ignore */ reactPath);
    const { default: { createRoot } } = await import(/* @vite-ignore */ domPath);
    const { useJobs } = await import(/* @vite-ignore */ clientPath);
    function Reader({ file }: { file: string }) {
      return React.createElement('output', null, JSON.stringify(useJobs(file)));
    }
    const root = createRoot(document.getElementById('root'));
    const mount = (file: string) => root.render(React.createElement(React.Fragment, null,
      React.createElement(Reader, { file }), React.createElement(Reader, { file })));
    Object.assign(window, {
      jobsPaths: paths,
      mountJobs: mount,
      pushJobs: (seq: number, chatId: string, jobs: unknown[]) => controller.enqueue(new TextEncoder().encode(
        `data: ${JSON.stringify({ epoch: 'engine', seq, type: 'jobs', payload: { chatId, jobs } })}\n\n`)),
      unmountJobs: () => root.unmount(),
    });
    mount('/projects/chat-a/session.jsonl');
  });
  await expect(page.locator('output')).toHaveText(['[]', '[]']);
  await expect.poll(() => page.evaluate(() => (window as any).jobsPaths)).toEqual(['/api/engine/events?after=0']);
  await page.evaluate(() => (window as any).pushJobs(1, 'chat-a', [{ id: 1, state: 'running', future: true }]));
  await expect(page.locator('output')).toHaveText(['[{"id":1,"state":"running"}]', '[{"id":1,"state":"running"}]']);
  await page.evaluate(() => (window as any).pushJobs(2, 'chat-a', [{ id: 1, state: 'done', exitCode: 0 }]));
  await expect(page.locator('output')).toHaveText(['[{"id":1,"state":"done","exitCode":0}]', '[{"id":1,"state":"done","exitCode":0}]']);
  await page.evaluate(() => (window as any).mountJobs('/projects/chat-b/session.jsonl'));
  await expect(page.locator('output')).toHaveText(['[]', '[]']);
  expect(await page.evaluate(() => (window as any).jobsPaths)).toEqual(['/api/engine/events?after=0']);
  await page.evaluate(() => (window as any).unmountJobs());
});
