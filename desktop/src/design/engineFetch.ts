import { isTauri } from '@tauri-apps/api/core';

/** Engine traffic uses native HTTP in packaged windows so long-lived streams never starve other windows.
 * Browser development retains its same-origin proxy and test routes. External pages never receive this capability. */
export async function engineFetch(url: string, init?: RequestInit): Promise<Response> {
 if (!isTauri()) return globalThis.fetch(url, init);
 const target = new URL(url);
 if (target.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(target.hostname)
  || target.username || target.password || !target.pathname.startsWith('/api/engine/')) {
  throw new TypeError('The local engine announced an invalid connection.');
 }
 const { fetch } = await import('@tauri-apps/plugin-http');
 // Never forward authenticated engine traffic through a redirect, including to another loopback service.
 return fetch(target.href, { ...init, redirect: 'error', maxRedirections: 0 });
}
