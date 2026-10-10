import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The Home tab harness, on this lane's port, so it never shares the app dev server.
const desktop = fileURLToPath(new URL('../..', import.meta.url));
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: `${desktop}node_modules/.cache/home-tab`,
  server: { port: 1778, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: null },
});
