import { expect, test } from "@playwright/test"

// The real-model run: one job on a real repository, both approvals clicked in
// the browser. Opt-in (FACTORY_LIVE=1, FACTORY_JOB=<id>); it needs a factory,
// a worker and a model key, so it never runs from `go test`.
test.beforeEach(() => {
  test.skip(process.env.FACTORY_LIVE !== "1", "needs a live factory with a worker")
})

const shots = process.env.FACTORY_SHOTS ?? "."

test("run a job and approve plan and proof in the browser", async ({ page }) => {
  test.setTimeout(60 * 60_000)
  const id = process.env.FACTORY_JOB
  await page.goto(`/jobs/${id}`)
  // Run only a job still in the backlog; a started job is left to its run.
  const state = ((await (await page.request.get(`/v1/jobs/${id}`)).json()) as { state: string }).state
  if (state === "backlog") expect((await page.request.post(`/v1/jobs/${id}/run`)).ok()).toBeTruthy()

  for (const gate of ["plan", "proof"]) {
    const approve = page.getByTestId("approve")
    await expect(approve).toBeEnabled({ timeout: 40 * 60_000 })
    await expect(approve).toContainText(gate === "plan" ? /Approve plan/ : /Approve →/, { timeout: 40 * 60_000 })
    await page.screenshot({ path: `${shots}/${gate}-before.png`, fullPage: true })
    await approve.click()
    await page.getByTestId("approve-confirm").click()
    await expect(page.getByTestId("verb-ok")).toContainText(/recorded as evt_/)
    await page.screenshot({ path: `${shots}/${gate}-after.png`, fullPage: true })
    // The next gate shows once the job moves on; wait for the label to leave.
    await expect(approve).toBeDisabled({ timeout: 60_000 }).catch(() => undefined)
  }
})
