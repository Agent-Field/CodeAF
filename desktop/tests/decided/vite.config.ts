import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Serves only the decided-rows harness, on this lane's port, so it never shares the app dev server.
const desktop = fileURLToPath(new URL('../..', import.meta.url));
const port = Number(process.env.CODEAF_UI_PORT ?? 1894);
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: `${desktop}node_modules/.cache/decided-rows`,
  server: { port, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: null },
});
