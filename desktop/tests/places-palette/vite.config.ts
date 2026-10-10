import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Serves only the Go to palette harness, on 1738, so it never collides with the app dev server (1420) or the main UI suite (1422).
const desktop = fileURLToPath(new URL('../..', import.meta.url));
export default defineConfig({
  root: fileURLToPath(new URL('./harness', import.meta.url)),
  plugins: [react()],
  clearScreen: false,
  cacheDir: `${desktop}node_modules/.cache/places-palette`,
  server: { port: 1738, strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: null },
});
