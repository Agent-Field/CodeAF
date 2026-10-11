import { expect, test } from "@playwright/test"

// Queue -> job -> approve -> "recorded as evt_…". The approve button opens a
// confirm dialog; only its confirm records the approval, so the spec clicks both.
test.beforeEach(() => {
  test.skip(process.env.FACTORY_APPROVE !== "1", "needs the approve-smoke server")
})

test("approve a job from the browser", async ({ page }) => {
  await page.goto("/mine")
  await page.getByText("budget caps per task").first().click()
  await expect(page).toHaveURL(/\/jobs\/1$/)
  await page.screenshot({ path: process.env.FACTORY_SHOTS ? `${process.env.FACTORY_SHOTS}/job-before.png` : undefined, fullPage: true })
  await page.getByTestId("approve").click()
  await page.getByTestId("approve-confirm").click()
  await expect(page.getByTestId("verb-ok")).toContainText(/Approved · recorded as evt_/)
  await page.screenshot({ path: process.env.FACTORY_SHOTS ? `${process.env.FACTORY_SHOTS}/job-after.png` : undefined, fullPage: true })
})
