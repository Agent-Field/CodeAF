import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
 root: fileURLToPath(new URL('./harness', import.meta.url)), plugins: [react()],
 cacheDir: fileURLToPath(new URL('./.vite-cache', import.meta.url)),
 server: { strictPort: true, host: '127.0.0.1', fs: { strict: false }, watch: null },
});
