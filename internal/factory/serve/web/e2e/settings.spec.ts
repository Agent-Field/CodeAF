import { expect, test } from "@playwright/test"

// Toggle a watched repository, then open its pipeline. Both go through the
// real factory: the switch is PUT /v1/repos, and the open row is the pipeline read.
test("toggle a repo and open its pipeline", async ({ page }) => {
  await page.goto("/settings")
  await expect(page.getByTestId("github-status")).toContainText("connected as @santosh via gh")
  await expect(page.getByTestId("access-mode")).toHaveText("localhost only")
  await expect(page.getByTestId("sign-out")).toHaveCount(0)

  const sw = page.getByTestId("repo-switch-acme/web")
  await expect(sw).not.toBeChecked()
  await sw.click()
  await expect(sw).toBeChecked()

  const listed = await page.request.get("/v1/repos")
  expect(listed.ok()).toBeTruthy()
  const body = (await listed.json()) as { watched: string[] }
  expect(body.watched).toContain("acme/web")

  await page.getByTestId("repo-open-acme/web").click()
  const pipeline = page.getByTestId("pipeline-acme/web")
  await expect(pipeline).toContainText("no file · using defaults")
  await expect(pipeline).toContainText("plan")
  await expect(page.getByTestId("dryrun-acme/web")).toHaveText("not available")
  const command = page.getByTestId("attach-command")
  await expect(command).toContainText("codeaf attach")
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"])
  await page.getByTestId("attach-copy").click()
  await expect(page.getByTestId("attach-copied")).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(await command.innerText())
})
