import type { Page } from '@playwright/test';

/** Fixture IPC transport for the official HTTP plugin. Requests still traverse the real browser fixture routes.
 * This exercises the native adapter contract; it is not evidence of OS networking or native window behavior. */
export async function installNativeHttpMock(page: Page) {
 await page.addInitScript(() => {
  type RequestRecord = { config: { method: string; url: string; headers: [string,string][]; data?: number[] }; abort: AbortController };
  const requests = new Map<number, RequestRecord>();
  const bodies = new Map<number, ReadableStreamDefaultReader<Uint8Array>>();
  let next = 1;
  Object.assign(window, { __engineHttpInvoke: async (command: string, args: Record<string, unknown>) => {
   const rid = args.rid as number;
   if (command === 'plugin:http|fetch') {
    const id = next++;
    requests.set(id, { config: args.clientConfig as RequestRecord['config'], abort: new AbortController() });
    return id;
   }
   if (command === 'plugin:http|fetch_send') {
    const request = requests.get(rid)!;
    const response = await fetch(request.config.url, { method: request.config.method, headers: request.config.headers,
     body: request.config.data?.length ? Uint8Array.from(request.config.data) : undefined, signal: request.abort.signal, redirect: 'error' });
    const id = next++;
    if (response.body) bodies.set(id, response.body.getReader());
    return { status: response.status, statusText: response.statusText, url: response.url, headers: [...response.headers], rid: id };
   }
   if (command === 'plugin:http|fetch_read_body') {
    const reader = bodies.get(rid);
    const chunk = reader ? await reader.read() : { done: true, value: undefined };
    if (chunk.done) { bodies.delete(rid); return Uint8Array.of(1).buffer; }
    return Uint8Array.from([...(chunk.value ?? []), 0]).buffer;
   }
   if (command === 'plugin:http|fetch_cancel') { requests.get(rid)?.abort.abort(); requests.delete(rid); return; }
   if (command === 'plugin:http|fetch_cancel_body') { const reader = bodies.get(rid); bodies.delete(rid); await reader?.cancel(); return; }
   throw new Error(`Unexpected HTTP fixture command: ${command}`);
  } });
 });
}
