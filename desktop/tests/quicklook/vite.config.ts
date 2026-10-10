import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// This isolated harness exercises Home tile activation without starting an engine.
// Polling watch is on because this box's file events are unreliable (CHOKIDAR_USEPOLLING=1).
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: '/tmp/codeaf-quicklook-439-vite',
  server: { port: 1739, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: { usePolling: true } },
});
