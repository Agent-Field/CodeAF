import { readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
let connection: {url?: string; token?: string} = {};
try { connection = JSON.parse(readFileSync(process.env.CODEAF_DESKTOP_CONNECTION || new URL('./node_modules/.cache/codeaf-engine-connection.json', import.meta.url), 'utf8')); } catch { /* UI fixtures work without an engine. */ }
export default defineConfig({
 plugins: [react()], clearScreen: false,
 server: { port: 1420, strictPort: true, host: '127.0.0.1', proxy: { '/api/engine': { target: process.env.CODEAF_DESKTOP_URL || connection.url || 'http://127.0.0.1:1423', changeOrigin: true, headers: (process.env.CODEAF_DESKTOP_TOKEN || connection.token) ? { Authorization: `Bearer ${process.env.CODEAF_DESKTOP_TOKEN || connection.token}` } : {} } }, watch: { ignored: ['**/src-tauri/**', '**/engine/**'] } },
});
