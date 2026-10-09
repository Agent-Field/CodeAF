import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Serves only the workspace-sync harness, on 1771, so it never collides with the app (1420), the UI suite (1422)
// or the Places harness (1711). /api/engine goes to the real bridge started by bridge.mjs, with its token added
// here exactly as the app's own dev proxy adds it.
const BRIDGE = 'http://127.0.0.1:17712';
const TOKEN = 'workspace-sync-browser-suite-token-0123456789abcdef';
const desktop = fileURLToPath(new URL('../..', import.meta.url));
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  // One optimizer cache per checkout and per harness (parallel worktrees share nothing).
  cacheDir: `${desktop}node_modules/.cache/workspace-sync`,
  server: {
    port: 1771, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: null,
    // The bridge refuses a page Origin it does not know (1771 is not a shell origin). This loopback proxy is the
    // trusted party, as the app's own is, so it forwards no Origin at all.
    proxy: { '/api/engine': { target: BRIDGE, changeOrigin: true, headers: { Authorization: `Bearer ${TOKEN}` }, configure: proxy => proxy.on('proxyReq', request => request.removeHeader('origin')) } },
  },
});
