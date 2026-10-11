import { defineConfig, devices } from "@playwright/test"

// The Go smoke starts the factory and sets FACTORY_WEB. Chrome is the
// browser already on the machine, so the suite does not download one.
export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  use: {
    baseURL: process.env.FACTORY_WEB,
    ...devices["Desktop Chrome"],
    channel: "chrome",
    launchOptions: { args: ["--no-sandbox", "--disable-dev-shm-usage"] },
  },
})
