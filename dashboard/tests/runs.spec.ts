import { expect, test, type Page } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

/* Run bar journeys (form-n7u.5-.8). The fixture answers as archond does: an
 * abort of a waiting run returns the canceled projection with endedBy, its
 * events end with run_canceled, and the problems route serves the cancel with
 * the requester and reason; a limit block projects resumeAllowed false. */

const text = (value: string) => ({ text: value, bytes: value.length })

async function cancelableRun(page: Page) {
  const aborts: unknown[] = []
  let canceled = false
  const waiting = { runId: 'run_browser', status: 'waiting_human', final: false, boardSlug: 'browser', missionId: 'mission', eventCount: 3, beadId: 'form-mnf', waitingGates: [{ gateId: 'loose', requestedSeq: 3 }], onCallSeats: [] }
  const ended = () => ({ ...waiting, status: 'canceled', final: true, eventCount: 4, waitingGates: [], endedBy: 'agent:ui' })
  const events = [{ seq: 1, type: 'run_started' }, { seq: 2, type: 'node_output', nodeId: 'execution' }, { seq: 3, type: 'human_input_requested', nodeId: 'loose', gateId: 'loose' }]
  await page.route('**/api/formations/runs/run_browser**', async route => {
    const path = new URL(route.request().url()).pathname
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data } })
    if (path.endsWith('/abort')) {
      aborts.push(route.request().postDataJSON())
      canceled = true
      return respond(ended())
    }
    if (path === '/api/formations/runs/run_browser') return respond(canceled ? ended() : waiting)
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
  await expect(confirm).toContainText('Peer and judge · form-mnf · run …rowser, waiting for you at Disconnected gate')
  await expect(confirm).toContainText('Disconnected gate stops waiting for you, and your unsent answer is not sent.')
  await expect(confirm).toContainText('No agent seats are kept on call for this run.')
  await expect(confirm.getByRole('button', { name: 'Keep running' })).toBeFocused()
  // The dialog is above every window, including the answer window.
  const box = (await confirm.boundingBox())!
  const topmost = await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest('[role="alertdialog"]') !== null, { x: box.x + box.width / 2, y: box.y + 40 })
  expect(topmost).toBe(true)

  await page.keyboard.press('Escape')
  await expect(confirm).toHaveCount(0)
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
  await expect(answer.getByRole('button', { name: 'Approve and end the run' })).toBeVisible()
  await expect(answer.getByRole('list', { name: 'Where your answer leads' })).toContainText('Approve ends the run.')
  await expect(answer.getByRole('list', { name: 'Where your answer leads' })).toContainText('Send back blocks the run: this gate has no send-back route.')

  // It opens clear of the gate it answers.
  const gate = (await page.locator('[data-node="loose"]').boundingBox())!
  const before = (await answer.boundingBox())!
  const overlaps = (a: typeof gate, b: typeof gate) => a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y
  expect(overlaps(before, gate), 'answer window over its gate').toBe(false)

  const title = answer.locator('.fwin-head')
  const start = (await title.boundingBox())!
  await page.mouse.move(start.x + 120, start.y + start.height / 2)
  await page.mouse.down()
  await page.mouse.move(start.x + 20, start.y + 200, { steps: 6 })
  await page.mouse.up()
  const moved = (await answer.boundingBox())!
  expect(Math.abs(moved.x - (before.x - 100))).toBeLessThan(2)
  expect(moved.y).not.toBe(before.y)

  await answer.getByRole('button', { name: 'Close Answer gate Disconnected gate' }).click()
  await expect(answer).toHaveCount(0)
  await page.getByTestId('run-point').click()
  await expect(answer).toBeVisible()
  await expect(answer.getByLabel('Your response')).toBeFocused()
  await page.waitForTimeout(600)
  await expect(page.locator('[data-window-id="node:loose"]')).toHaveCount(0)
})

test('a block that cannot resume offers no Resume and says so', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { run: true, blockedAtJudge: true })
  await page.goto('/')
  await expect(page.getByTestId('run-point')).toContainText('blocked at Review gate')
  await expect(page.getByRole('button', { name: 'Resume run' })).toHaveCount(0)
  await expect(page.getByTestId('run-not-resumable')).toHaveText('Can’t resume. Start a new run.')
})
