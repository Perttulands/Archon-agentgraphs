import { expect, test, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

/* Run bar journeys (archon-n7u.5-.8). The fixture answers as archond does: an
 * abort of a waiting run returns the canceled projection with endedBy, its
 * events end with run_canceled, and the problems route serves the cancel with
 * the requester and reason; a limit block projects resumeAllowed false. */

const text = (value: string) => ({ text: value, bytes: value.length })

async function cancelableRun(page: Page) {
  const aborts: unknown[] = []
  let canceled = false
  const waiting = { runId: 'run_browser', status: 'waiting_human', final: false, missionSlug: 'browser', inputCardId: 'mission', eventCount: 3, beadId: 'archon-mnf', waitingGates: [{ gateId: 'loose', requestedSeq: 3 }], onCallSeats: [] }
  const ended = () => ({ ...waiting, status: 'canceled', final: true, eventCount: 4, waitingGates: [], endedBy: 'agent:ui' })
  const events = [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_output', nodeId: 'execution' }, { seq: 3, type: 'human_input_requested', nodeId: 'loose', gateId: 'loose' }]
  await page.route('**/api/runs/run_browser**', async route => {
    const path = new URL(route.request().url()).pathname
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
    if (path.endsWith('/abort')) {
      aborts.push(route.request().postDataJSON())
      canceled = true
      return respond(ended())
    }
    if (path === '/api/runs/run_browser') return respond(canceled ? ended() : waiting)
    if (path.endsWith('/events')) return respond({ events: canceled ? [...events, { seq: 4, type: 'run_canceled' }] : events })
    if (path.endsWith('/evidence/problems')) {
      const reason = (aborts[aborts.length - 1] as { reason?: string } | undefined)?.reason || ''
      return respond({ problems: canceled ? [{ seq: 4, type: 'run_canceled', nodeIds: ['loose'], reason: text(reason), actor: 'agent:ui' }] : [] })
    }
    return route.fallback()
  })
  return { aborts }
}

test('Stop asks first, Escape keeps the run, and the canceled run says who stopped it and why', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { waitingHuman: true })
  const { aborts } = await cancelableRun(page)
  await page.goto('/')

  const answer = page.getByRole('dialog', { name: 'Answer gate Disconnected gate' })
  await answer.getByLabel('Your response').fill('half an answer')
  const stop = page.getByRole('button', { name: 'Stop run' })
  await stop.click()
  const confirm = page.getByRole('alertdialog', { name: 'Stop run …rowser?' })
  await expect(confirm).toContainText('Peer and judge · archon-mnf · run …rowser, waiting for you at Disconnected gate')
  await expect(confirm).toContainText('Disconnected gate stops waiting for you, and your unsent answer is not sent.')
  await expect(confirm).toContainText('No agent seats are kept on call for this run.')
  await expect(confirm.getByRole('button', { name: 'Keep running' })).toBeFocused()
  // The dialog is above every window, including the answer window.
  const box = (await confirm.boundingBox())!
  const topmost = await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('[role="alertdialog"]') !== null, { x: box.x + box.width / 2, y: box.y + 40 })
  expect(topmost).toBe(true)

  // Modal: the page behind is inert while the dialog is open.
  await expect(page.locator('.fmx[data-testid="formations-view"]').locator('xpath=ancestor-or-self::*[@inert]')).toHaveCount(1)
  await page.keyboard.press('Escape')
  await expect(confirm).toHaveCount(0)
  // Escape closed only the dialog: the answer window beneath it is still open.
  await expect(answer).toBeVisible()
  await expect(stop).toBeFocused()
  expect(aborts).toEqual([])

  await stop.click()
  await page.getByLabel('Why (optional)').fill('The brief was wrong')
  await page.getByRole('alertdialog').getByRole('button', { name: 'Stop run' }).click()
  await expect.poll(() => aborts).toEqual([{ reason: 'The brief was wrong', requestedBy: 'agent:ui' }])
  await expect(page.getByTestId('run-point')).toHaveText('canceled at Disconnected gate by the operator in the cockpit: The brief was wrong')
  await expect(page.getByRole('button', { name: 'Stop run' })).toHaveCount(0)
})

test('the answer window moves, names where the answer leads, and comes back from the run bar', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { waitingHuman: true })
  await page.goto('/')
  const answer = page.getByRole('dialog', { name: 'Answer gate Disconnected gate' })
  await expect(answer.getByRole('button', { name: 'Approve', exact: true })).toBeVisible()
  await expect(answer.getByRole('list', { name: 'Where your answer leads' })).toContainText('Approve: this path ends (done); the run goes on with its other work.')
  await expect(answer.getByRole('list', { name: 'Where your answer leads' })).toContainText('Send back: this path ends (rejected); the run goes on with its other work.')

  // It opens clear of the gate it answers.
  const gate = (await page.locator('[data-node="loose"]').boundingBox())!
  const before = (await answer.boundingBox())!
  const overlaps = (a: typeof gate, b: typeof gate) => a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y
  expect(overlaps(before, gate), 'answer window over its gate').toBe(false)

  // Drag it 100px toward the side of the canvas with room, so the workspace clamp never interferes.
  const title = answer.locator('.fwin-head')
  const start = (await title.boundingBox())!
  const dx = before.x + before.width / 2 > 960 ? -100 : 100
  await page.mouse.move(start.x + 120, start.y + start.height / 2)
  await page.mouse.down()
  await page.mouse.move(start.x + 120 + dx, start.y + 200, { steps: 6 })
  await page.mouse.up()
  const moved = (await answer.boundingBox())!
  expect(Math.abs(moved.x - (before.x + dx))).toBeLessThan(2)
  expect(moved.y).not.toBe(before.y)

  await answer.getByRole('button', { name: 'Close Answer gate Disconnected gate' }).click()
  await expect(answer).toHaveCount(0)
  // The gate waits while Execution still runs, and the run bar says both (archon-o7p.11).
  await expect(page.getByTestId('run-points')).toHaveText('waiting for you at Disconnected gate·running Execution')
  await page.getByRole('button', { name: 'waiting for you at Disconnected gate' }).click()
  await expect(answer).toBeVisible()
  await expect(answer.getByLabel('Your response')).toBeFocused()
  await page.waitForTimeout(600)
  await expect(page.locator('[data-window-id="node:loose"]')).toHaveCount(0)
})

test('two gates wait at once: the run bar names both and each opens its own answer (archon-o7p.11)', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { waitingHuman: true, secondGate: true })
  const verdicts: Array<{ path: string; body: unknown }> = []
  await page.route('**/api/runs/run_browser/gates/*/verdict', route => {
    verdicts.push({ path: new URL(route.request().url()).pathname, body: route.request().postDataJSON() })
    return route.fulfill({ status: 202, json: { success: true, data: { runId: 'run_browser' } } })
  })
  await page.goto('/')
  await expect(page.getByTestId('run-points')).toHaveText('waiting for you at Disconnected gate·waiting for you at Second look·running Execution')
  // The oldest waiting gate's answer opens first.
  const first = page.getByRole('dialog', { name: 'Answer gate Disconnected gate' })
  await expect(first).toBeVisible()
  await page.getByRole('button', { name: 'waiting for you at Second look' }).click()
  const second = page.getByRole('dialog', { name: 'Answer gate Second look' })
  await expect(second).toBeVisible()
  await expect(first).toHaveCount(0)
  await expect(second.getByRole('list', { name: 'Where your answer leads' })).toContainText('Approve: this path ends (done)')
  await second.getByLabel('Your response').fill('looks right')
  await second.getByRole('button', { name: 'Approve', exact: true }).click()
  await expect.poll(() => verdicts).toEqual([{ path: '/api/runs/run_browser/gates/second/verdict', body: expect.objectContaining({ requestedSeq: 4, verdict: 'pass', reason: 'looks right' }) }])
  await page.screenshot({ path: test.info().outputPath('two-gates-run-bar.png') })
})

test('a block that cannot resume offers no Resume and says so', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { run: true, blockedAtJudge: true })
  await page.goto('/')
  await expect(page.getByTestId('run-point')).toContainText('blocked at Review gate')
  await expect(page.getByRole('button', { name: 'Resume run' })).toHaveCount(0)
  await expect(page.getByTestId('run-not-resumable')).toHaveText(/^Can’t resume: .+\.$/)
  await expect(page.getByTestId('run-not-resumable')).not.toContainText('new run')
})
