import { expect, test } from "@playwright/test"

// The settings smoke serves a factory with no record and no new-job door.
// This spec needs both, and skips there so that smoke stays green.
test.beforeEach(() => {
  test.skip(process.env.FACTORY_AUDIT !== "1", "needs the factory audit server")
})

test("audit tabs, csv export, and send a job", async ({ page }) => {
  await page.goto("/audit")
  await expect(page.getByRole("heading", { name: "Audit log" })).toBeVisible()
  const table = page.getByTestId("audit-table")
  await expect(table).toContainText("approved #1661")
  await expect(table).toContainText("$1.50")
  await expect(table).toContainText("answered #1538 · yes")
  await expect(table).toContainText("auto-started #1660 by rule agent-ready")
  await expect(table).toContainText("took #1662")
  await expect(table).toContainText("dropped #1540 · duplicate of #1490")
  const started = page.getByRole("row", { name: /started #1551/ })
  await expect(started).toContainText("factory")
  await expect(started).not.toContainText("$")

  await page.getByTestId("audit-tab-approvals").click()
  await expect(page.getByTestId("audit-row")).toHaveCount(1)
  await expect(table).toContainText("approved #1661")
  await expect(table).not.toContainText("took")

  await page.getByTestId("audit-tab-answers").click()
  await expect(table).toContainText("answered #1538 · yes")
  await expect(page.getByTestId("audit-row")).toHaveCount(1)

  await page.getByTestId("audit-tab-starts").click()
  await expect(page.getByTestId("audit-row")).toHaveCount(2)
  await expect(table).toContainText("started #1551")
  await expect(table).toContainText("auto-started #1660 by rule agent-ready")

  await page.getByTestId("audit-tab-takes").click()
  await expect(table).toContainText("took #1662")
  await expect(table).toContainText("@priya")
  await expect(page.getByTestId("audit-row")).toHaveCount(1)

  await page.getByTestId("audit-tab-all").click()
  await expect(page.getByTestId("audit-row")).toHaveCount(6)

  const csv = page.waitForResponse((res) => res.url().includes("format=csv"))
  await page.getByTestId("audit-export").click()
  const exported = await csv
  expect(exported.status()).toBe(200)
  expect(exported.headers()["content-type"]).toContain("text/csv")
  expect(exported.headers()["content-disposition"]).toContain("factory-record.csv")
  const body = await exported.text()
  expect(body).toContain("id,at,kind,job,handle,cost,detail")
  expect(body).toContain("auto_started")
  expect(body).toContain("agent-ready")

  await page.goto("/new")
  await expect(page.getByRole("heading", { name: "New job" })).toBeVisible()
  await expect(page.getByText("Send a job to the factory")).toBeVisible()
  await expect(page.getByTestId("factory-address")).toContainText("7420")
  const autostart = page.getByTestId("job-autostart")
  await expect(autostart).toBeChecked()
  await autostart.click()
  await expect(autostart).not.toBeChecked()
  await autostart.click()
  await expect(autostart).toBeChecked()

  await page.getByTestId("job-repo").click()
  await page.getByRole("option", { name: "agentfield/codeaf", exact: true }).click()
  await page.getByTestId("job-title").fill("meter the midnight rollover")
  await page.getByTestId("job-text").fill("it crashes at 00:00")

  const posted = page.waitForRequest((req) => req.method() === "POST" && req.url().endsWith("/v1/jobs"))
  await page.getByTestId("send-job").click()
  expect((await posted).postDataJSON()).toEqual({
    repo: "agentfield/codeaf",
    title: "meter the midnight rollover",
    text: "it crashes at 00:00",
    autostart: true,
  })
  await expect(page.getByTestId("new-job-sent")).toContainText("Sent to factory")
  await expect(page.getByTestId("new-job-sent")).toContainText("job 42 is in your Backlog")
  await expect(page.getByTestId("new-job-error")).toHaveCount(0)
})
