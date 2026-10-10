import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Serves only the Overturn flow harness, on 1717, so it never collides with the app dev server (1420), the main UI suite (1422) or the primitives harness (1711).
// Polling watch is on because this box's file events are unreliable (CHOKIDAR_USEPOLLING=1).
const desktop = fileURLToPath(new URL('../..', import.meta.url));
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: `${desktop}node_modules/.cache/overturn-flow`,
  server: { port: 1717, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: { usePolling: true } },
});
