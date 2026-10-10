import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The Home harness, on this lane's port, so the breadcrumb spec does not take the shared 1745 server.
const desktop = fileURLToPath(new URL('../..', import.meta.url));
const port = Number(process.env.LANE_PORT ?? 1757);
export default defineConfig({
  root: fileURLToPath(new URL('../places-home/harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: `${desktop}node_modules/.cache/places-breadcrumb`,
  server: { port, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: { usePolling: true } },
});
