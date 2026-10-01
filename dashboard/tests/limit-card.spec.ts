import { expect, test, type Page } from '@playwright/test'
import { cockpitFixture, limitReason, timeLimitReason } from './cockpit-fixture'

// Limit cards (archon-o7p.8.1, .8.2): created from the Limit token and the
// canvas menu, wired by dragging their tether handle onto a step or the Input
// card, their rounds, time and warning edited in their window, deleted and
// restored, each step with undo; Flow states the limit on what it covers, and a
// run blocked at a spent card offers one more round or the card's time again.

// Screenshots go to the test's own output directory (test-results/), so the
// spec stays host-neutral.
async function shot(page: Page, name: string) {
  await page.screenshot({ path: test.info().outputPath(name) })
}

async function center(page: Page, selector: string) {
  const box = await page.locator(selector).boundingBox()
  expect(box, selector).not.toBeNull()
  return { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 }
}

async function drag(page: Page, from: { x: number; y: number }, to: { x: number; y: number }) {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(to.x, to.y, { steps: 12 })
  await page.mouse.up()
}

test('Limit cards are created, wired, edited, deleted and restored on the canvas with undo, and Flow states them', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const execution = page.getByTestId('formation-node-execution')
  await expect(execution).toBeVisible()
  const limits = page.locator('.limitcard')
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  // Drop the Limit token on Execution: the card covers it and its window opens for its rounds.
  await drag(page, await center(page, '[data-testid="limit-token"]'), await center(page, '[data-testid="formation-node-execution"]'))
  await expect(limits).toHaveCount(1)
  const cap = fixture.board().limits![0]
  expect(cap).toMatchObject({ title: 'Limit', target: 'execution' })
  expect(cap.rounds).toBeUndefined()
  const card = page.getByTestId(`limit-node-${cap.id}`)
  await expect(card).toContainText('Covers Execution')
  await expect(card).toContainText('sets no limit yet')
  // A card that sets no rounds is a draft, with its finding.
  await expect(page.getByTestId(`draft-marker-${cap.id}`)).toHaveAttribute('title', 'Limit Limit sets no limit: give it rounds or time, or delete it')
  const window = page.getByRole('dialog', { name: 'Limit card · Limit' })
  await expect(window).toBeVisible()
  await expect(window.getByLabel('Covers')).toHaveValue('execution')

  // Its rounds and title are edited in the window.
  await window.getByRole('button', { name: 'Edit rounds' }).click()
  await window.getByRole('textbox', { name: 'Rounds' }).fill('0')
  await window.getByRole('button', { name: 'Save rounds' }).click()
  await expect(window.getByRole('alert')).toHaveText('Enter a positive whole number of rounds, or leave it blank for no limit.')
  await window.getByRole('textbox', { name: 'Rounds' }).fill('3')
  await window.getByRole('button', { name: 'Save rounds' }).click()
  await expect(card).toContainText('at most 3 rounds')
  await window.getByRole('button', { name: 'Edit title' }).click()
  await window.getByRole('textbox', { name: 'Title' }).fill('Cap')
  await window.getByRole('button', { name: 'Save title' }).click()
  const capWindow = page.getByRole('dialog', { name: 'Limit card · Cap' })
  await expect(capWindow.getByTestId(`limit-meaning-${cap.id}`)).toHaveText('Execution may run at most 3 times, send-backs and resumed re-runs included. When its rounds are spent the run blocks before it starts again, until you grant one more round.')
  expect(fixture.board().limits![0]).toMatchObject({ title: 'Cap', target: 'execution', rounds: 3 })
  await expect(page.getByTestId(`draft-marker-${cap.id}`)).toHaveCount(0)
  // Its tether runs to Execution.
  await expect(page.getByTestId(`limit-tether-${cap.id}`)).toHaveCount(1)
  await shot(page, 'limit-card-window.png')
  await capWindow.getByRole('button', { name: /close/i }).first().click()

  // Undo takes back the title, then the rounds.
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.board().limits![0].title).toBe('Limit')
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.board().limits![0].rounds).toBeUndefined()
  await expect(card).toContainText('sets no limit yet')
  await page.keyboard.press('Control+z')
  await expect(limits).toHaveCount(0)

  // The canvas menu makes one wired to nothing, a draft with its finding.
  const box = (await execution.boundingBox())!
  await page.mouse.click(box.x + box.width / 2, box.y + box.height + 160, { button: 'right' })
  await menuItem('Limit card').click()
  await expect(limits).toHaveCount(1)
  const loose = fixture.board().limits![0]
  expect(loose).toMatchObject({ title: 'Limit', target: '' })
  const looseCard = page.getByTestId(`limit-node-${loose.id}`)
  await expect(looseCard).toContainText('Wired to nothing yet')
  await expect(page.getByTestId(`draft-marker-${loose.id}`)).toHaveAttribute('title', /Limit Limit is wired to nothing: wire it to a step, or to the Input card for the whole mission/)
  await page.getByRole('dialog', { name: 'Limit card · Limit' }).getByRole('button', { name: /close/i }).first().click()

  // Dragging the handle onto a gate covers nothing; onto the Input card, the whole mission.
  const handle = `[data-testid="limit-handle-${loose.id}"]`
  await drag(page, await center(page, handle), await center(page, '[data-testid="gate-node-gate"]'))
  await expect(looseCard).toContainText('Wired to nothing yet')
  expect(fixture.board().limits![0].target).toBe('')
  await drag(page, await center(page, handle), await center(page, '[data-testid="mission-node-mission"]'))
  await expect(looseCard).toContainText('Covers the mission')
  expect(fixture.board().limits![0].target).toBe('mission')
  await expect(page.getByTestId(`limit-tether-${loose.id}`)).toHaveCount(1)
  // And onto a step, Execution.
  await drag(page, await center(page, handle), await center(page, '[data-testid="formation-node-execution"]'))
  await expect(looseCard).toContainText('Covers Execution')
  await shot(page, 'limit-card-wired.png')
  // Undo puts the tether back on the Input card.
  await page.keyboard.press('Control+z')
  await expect(looseCard).toContainText('Covers the mission')

  // Its window sets the rounds of the mission card.
  await looseCard.click()
  const missionWindow = page.getByRole('dialog', { name: 'Limit card · Limit' })
  await missionWindow.getByRole('button', { name: 'Edit rounds' }).click()
  await missionWindow.getByRole('textbox', { name: 'Rounds' }).fill('20')
  await missionWindow.getByRole('button', { name: 'Save rounds' }).click()
  await expect(looseCard).toContainText('at most 20 step runs')
  await missionWindow.getByRole('button', { name: /close/i }).first().click()

  // Delete from its menu; undo brings it back as it was.
  await looseCard.click({ button: 'right' })
  await menuItem('Delete Limit card').click()
  await expect(limits).toHaveCount(0)
  await page.keyboard.press('Control+z')
  await expect(page.getByTestId(`limit-node-${loose.id}`)).toBeVisible()
  expect(fixture.board().limits).toEqual([{ id: loose.id, title: 'Limit', target: 'mission', rounds: 20 }])
  await expect(page.getByTestId('formations-error')).toHaveCount(0)

  // Flow states the limit on the Input card, and the Input card's window too.
  await page.getByRole('radio', { name: 'Flow' }).click()
  const flow = page.getByTestId('flow-view')
  await expect(flow.getByRole('region', { name: 'Input card Delivery' })).toContainText('Limit: the whole mission may make at most 20 step runs')
  await shot(page, 'limit-card-flow.png')
  await flow.getByRole('button', { name: 'Limit: the whole mission may make at most 20 step runs' }).click()
  await expect(page.getByRole('dialog', { name: 'Limit card · Limit' })).toBeVisible()
})

test('a step\'s Limit card reads in Flow and in the step\'s window', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  await cockpitFixture(page, { blockedAtLimit: true })
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('limit-node-lim_cap')).toContainText('Covers Execution')
  await page.getByTestId('formation-node-execution').getByText('Execution', { exact: true }).click()
  const execution = page.getByRole('dialog', { name: 'Formation · Execution' })
  await expect(execution.getByRole('button', { name: 'Limit Cap: at most 2 rounds' })).toBeVisible()
  await execution.getByRole('button', { name: /close/i }).first().click()
  await page.getByRole('radio', { name: 'Flow' }).click()
  await expect(page.getByTestId('flow-step-execution')).toContainText('LimitCap: at most 2 rounds')
})

test('a run blocked at a spent Limit card offers one more round, which posts a grant', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { blockedAtLimit: true })
  const resumes: unknown[] = []
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().endsWith('/api/runs/run_browser/resume')) resumes.push(request.postDataJSON())
  })
  await page.goto('/?mission=browser')
  const banner = page.getByTestId('run-banner')
  await expect(banner.getByTestId('run-point')).toHaveText(`blocked at Execution: ${limitReason}`)
  await expect(banner.getByRole('button', { name: 'Resume run' })).toHaveCount(0)
  const grant = banner.getByRole('button', { name: 'Grant one more round' })
  await expect(grant).toBeVisible()
  await shot(page, 'limit-run-blocked.png')
  await grant.click()
  await expect.poll(() => resumes).toEqual([{ actor: 'agent:ui', mode: 'reattach', reason: 'one more round granted in the cockpit', grant: true }])
  await expect(grant).toHaveCount(0)
  await expect(banner.getByTestId('run-point')).toHaveText('running Execution (attempt 3)')
  expect(fixture.writes).toContain('POST /api/runs/run_browser/resume')
  await shot(page, 'limit-run-granted.png')
})

test('a Limit card\'s time and warning are set in its window and read on the card, the step and Flow, with undo', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('formation-node-execution')).toBeVisible()
  await drag(page, await center(page, '[data-testid="limit-token"]'), await center(page, '[data-testid="formation-node-execution"]'))
  await expect(page.locator('.limitcard')).toHaveCount(1)
  const cap = fixture.board().limits![0]
  const card = page.getByTestId(`limit-node-${cap.id}`)
  const window = page.getByRole('dialog', { name: 'Limit card · Limit' })
  await expect(window).toBeVisible()

  // A warning needs time first.
  await window.getByRole('button', { name: 'Edit warning' }).click()
  await window.getByRole('textbox', { name: 'Warning' }).fill('5m')
  await window.getByRole('button', { name: 'Save warning' }).click()
  await expect(window.getByRole('alert')).toHaveText('A warning needs time: give the card time first.')
  await window.getByRole('button', { name: 'Cancel' }).click()

  await window.getByRole('button', { name: 'Edit time' }).click()
  await window.getByRole('textbox', { name: 'Time' }).fill('half an hour')
  await window.getByRole('button', { name: 'Save time' }).click()
  await expect(window.getByRole('alert')).toHaveText('Enter a time such as 45s, 30m or 1h30m (whole seconds), or leave it blank for no time limit.')
  await window.getByRole('textbox', { name: 'Time' }).fill('30m')
  await window.getByRole('button', { name: 'Save time' }).click()
  await expect(card).toContainText('at most 30 min of work')
  expect(fixture.board().limits![0]).toMatchObject({ seconds: 1800 })
  // A card with time is no longer a draft.
  await expect(page.getByTestId(`draft-marker-${cap.id}`)).toHaveCount(0)

  await window.getByRole('button', { name: 'Edit warning' }).click()
  await window.getByRole('textbox', { name: 'Warning' }).fill('5m')
  await window.getByRole('button', { name: 'Save warning' }).click()
  await expect(card).toContainText('warns at 5 min left')
  expect(fixture.board().limits![0]).toMatchObject({ seconds: 1800, warnSeconds: 300 })
  await expect(window.getByTestId(`limit-meaning-${cap.id}`)).toHaveText('Execution may work at most 30 min over all its attempts, counted only while one runs. Waiting on a human gate does not count, so a send-back resumes it with the time it has left. When the time runs out the step stops and the run blocks until you grant 30 min more. With 5 min left, Archon pastes a warning into its seats.')

  await window.getByRole('button', { name: 'Edit rounds' }).click()
  await window.getByRole('textbox', { name: 'Rounds' }).fill('3')
  await window.getByRole('button', { name: 'Save rounds' }).click()
  await expect(card).toContainText('at most 3 rounds · 30 min')
  await shot(page, 'limit-card-time-window.png')
  await window.getByRole('button', { name: /close/i }).first().click()

  // Flow and the step's window state the time with the rounds.
  await page.getByRole('radio', { name: 'Flow' }).click()
  await expect(page.getByTestId('flow-step-execution')).toContainText('LimitLimit: at most 3 rounds and 30 min of work, warns at 5 min left')
  await shot(page, 'limit-card-time-flow.png')
  await page.getByRole('radio', { name: 'Canvas' }).click()

  // Each change undoes on its own: the rounds, then the warning, then the time.
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.board().limits![0].rounds).toBeUndefined()
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.board().limits![0].warnSeconds).toBeUndefined()
  await expect(card).not.toContainText('warns at')
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.board().limits![0].seconds).toBeUndefined()
  await expect(card).toContainText('sets no limit yet')
})

test('a run blocked at a spent time card offers the card\'s time again, which posts a grant', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { blockedAtTime: true })
  const resumes: unknown[] = []
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().endsWith('/api/runs/run_browser/resume')) resumes.push(request.postDataJSON())
  })
  await page.goto('/?mission=browser')
  await expect(page.getByTestId('limit-node-lim_cap')).toContainText('at most 30 min of work')
  await expect(page.getByTestId('limit-node-lim_cap')).toContainText('warns at 5 min left')
  const banner = page.getByTestId('run-banner')
  await expect(banner.getByTestId('run-point')).toHaveText(`blocked at Execution: ${timeLimitReason}`)
  await expect(banner.getByRole('button', { name: 'Resume run' })).toHaveCount(0)
  const grant = banner.getByRole('button', { name: 'Grant 30 min more' })
  await expect(grant).toBeVisible()
  // The step's window lists the warning its seat was given.
  await page.getByTestId('formation-node-execution').getByText('Execution', { exact: true }).click()
  const execution = page.getByRole('dialog', { name: 'Formation · Execution' })
  await expect(execution.getByTestId('limit-warnings-execution')).toHaveText('#3 · attempt 1 · time warning pasted into Worker 1')
  await shot(page, 'limit-run-blocked-time.png')
  await execution.getByRole('button', { name: /close/i }).first().click()
  await grant.click()
  await expect.poll(() => resumes).toEqual([{ actor: 'agent:ui', mode: 'reattach', reason: '30 min more granted in the cockpit', grant: true }])
  await expect(grant).toHaveCount(0)
  await expect(banner.getByTestId('run-point')).toHaveText('running Execution (attempt 2)')
  expect(fixture.writes).toContain('POST /api/runs/run_browser/resume')
})
