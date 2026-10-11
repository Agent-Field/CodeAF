import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// The dashboard is served by the factory itself at `/`, with the API on the
// same origin at `/v1`; `npm run dev` proxies /v1 to a local factory so the
// page can be worked on with hot reload against real data.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "./src") } },
  server: { proxy: { "/v1": process.env.FACTORY_URL ?? "http://127.0.0.1:7420" } },
  build: { outDir: "dist", emptyOutDir: true },
})
