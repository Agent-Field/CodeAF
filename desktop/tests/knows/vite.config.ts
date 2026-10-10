import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';
export default defineConfig({ cacheDir: '/tmp/codeaf-knows-vite-211', root: fileURLToPath(new URL('./harness', import.meta.url)), plugins: [react()], server: { host: '127.0.0.1', strictPort: true, fs: { strict: false }, watch: { usePolling: true } } });
