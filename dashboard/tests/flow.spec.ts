import { cockpitFixture, flowInputs } from './cockpit-fixture'
import { expect, test } from '@playwright/test'
import { scouting, scoutingFixture } from './scouting-fixture'

type Node = { id: string; title: string }

test('Flow shows all 7 Scouting steps readable at 1440x900 without horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  await expect(page.locator('.formation').first()).toBeVisible()
  await page.getByRole('radio', { name: 'Flow' }).click()
  const flow = page.getByTestId('flow-view')
  await expect(flow).toBeVisible()
  expect(await flow.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)

  const steps = flow.locator('[data-testid^="flow-step-"]')
  await expect(steps).toHaveCount(7)
  const titles = ['Map the territory', 'Framing review', 'Question peers', 'Answer questions', 'Draft the brief', 'Adversarial review', 'Brief sign-off']
  for (const [index, title] of titles.entries()) {
    const step = steps.nth(index)
    await step.scrollIntoViewIfNeeded()
    const heading = step.getByRole('button', { name: `${index + 1} ${title}` })
    await expect(heading).toBeInViewport()
    expect(await heading.evaluate(element => element.scrollWidth <= element.clientWidth), `${title} title fits`).toBe(true)
    expect(await step.evaluate(element => element.scrollWidth <= element.clientWidth), `${title} row fits`).toBe(true)
    const fontSize = await step.locator('.flow-text').first().evaluate(element => parseFloat(getComputedStyle(element).fontSize))
    expect(fontSize, `${title} text size`).toBeGreaterThanOrEqual(11)
  }
  await expect(steps.nth(1)).toContainText('↺ back to 1 Map the territory')
  await expect(steps.nth(5)).toContainText('Decided by a judge')
  await expect(steps.nth(5).getByRole('list', { name: 'Judges of Adversarial review' })).toContainText('Brief critic')
  await expect(steps.nth(6)).toContainText('→ this path ends (done)')

  // A row opens its node window beside it, and the Flow view is remembered for the board.
  await steps.nth(4).getByRole('button', { name: '5 Draft the brief' }).click()
  await expect(page.getByRole('dialog', { name: 'Formation · Draft the brief' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.reload()
  await expect(page.getByTestId('flow-view')).toBeVisible()
  const critic = scouting.mission.formations.find((node: Node) => node.title === 'Brief critic')
  await expect(page.getByTestId(`flow-step-${critic.id}`)).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})

test('Flow opens windows beside the clicked title or route link, and its run point says it opens the step', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await scoutingFixture(page)
  const gate = scouting.mission.gates.find((node: Node) => node.title === 'Adversarial review')
  const run = { missionId: scouting.mission.id, inputExcerpt: '', sessions: [], runId: 'run_scouting', status: 'blocked', final: false, resumeAllowed: false, missionSlug: 'scouting', inputCardId: scouting.mission.inputCards[0].id, eventCount: 2 }
  const events = [
    { runId: run.runId, seq: 1, type: 'gate_evaluating', nodeId: gate.id, gateId: gate.id },
    { runId: run.runId, seq: 2, type: 'run_blocked', nodeId: gate.id, gateId: gate.id },
  ]
  const reply = (data: unknown) => ({ json: { success: true, data } })
  // A blocked run on Adversarial review, added after the fixture so these routes answer first.
  await page.route(url => url.pathname.startsWith('/api/runs'), route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/runs') return route.fulfill(reply([run]))
    if (path === `/api/runs/${run.runId}`) return route.fulfill(reply({ ...run, events }))
    if (path.endsWith('/events')) return route.fulfill(reply({ events }))
    if (path.endsWith('/evidence/problems')) return route.fulfill(reply({ problems: [
      { seq: 2, type: 'run_blocked', nodeIds: [gate.id], reason: { text: 'invalid judge result: missing verdict block', bytes: 42 }, resumeAllowed: false },
    ] }))
    if (path.endsWith('/escalations')) return route.fulfill(reply({ escalations: [] }))
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  await page.goto('/?mission=scouting')
  await expect(page.locator('.formation').first()).toBeVisible()
  await page.getByRole('radio', { name: 'Flow' }).click()
  const flow = page.getByTestId('flow-view')
  const steps = flow.locator('[data-testid^="flow-step-"]')
  await expect(steps).toHaveCount(7)

  const beside = async (control: typeof flow, window: typeof flow) => {
    const a = (await control.boundingBox())!
    const b = (await window.boundingBox())!
    const covers = b.x < a.x + a.width && b.x + b.width > a.x && b.y < a.y + a.height && b.y + b.height > a.y
    return !covers
  }

  const title = steps.nth(4).getByRole('button', { name: '5 Draft the brief' })
  await title.click()
  const draft = page.getByRole('dialog', { name: 'Formation · Draft the brief' })
  await expect(draft).toBeVisible()
  expect(await beside(title, draft), 'Draft the brief window leaves its title uncovered').toBe(true)
  expect((await draft.boundingBox())!.x, 'window opens right of the title').toBeGreaterThanOrEqual((await title.boundingBox())!.x + (await title.boundingBox())!.width - 1)
  await page.keyboard.press('Escape')

  const loop = steps.nth(1).getByRole('button', { name: '↺ back to 1 Map the territory' })
  await loop.click()
  const map = page.getByRole('dialog', { name: 'Formation · Map the territory' })
  await expect(map).toBeVisible()
  expect(await beside(loop, map), 'Map the territory window leaves the route link uncovered').toBe(true)
  expect(await beside(steps.nth(1).getByRole('button', { name: '2 Framing review' }), map), 'and the row title').toBe(true)
  await page.keyboard.press('Escape')

  const point = steps.nth(5).getByTestId('run-point')
  await expect(point).toHaveText('blocked at Adversarial review: invalid judge result: missing verdict block')
  await expect(point).toHaveAttribute('title', /\. Open the step\.$/)
  await expect(page.getByTestId('run-banner').getByTestId('run-point')).toHaveAttribute('title', /\. Open the step\.$/)
  await point.click()
  const review = page.getByRole('dialog', { name: 'Gate · Adversarial review' })
  await expect(review).toBeVisible()
  expect(await beside(point, review), 'the gate window leaves the run point uncovered').toBe(true)
  expect(await beside(steps.nth(5).getByRole('button', { name: '6 Adversarial review' }), review), 'and the gate title').toBe(true)
  expect(fixture.writes).toEqual([])
})

test('Flow labels fan-out siblings as parallel with a shared step number', async ({ page }) => {
  await scoutingFixture(page)
  const board = structuredClone(scouting.mission)
  const first = board.formations[0]
  const sibling = { ...first, id: 'fmn_parallel', title: 'Parallel work' }
  board.formations.push(sibling)
  const feed = board.connections.find((route: { to: string }) => route.to.startsWith(first.id + ':'))!
  board.connections.push({ id: 'edge_parallel', from: feed.from, to: `${sibling.id}:${first.inputs[0].id}` })
  await page.route('**/api/missions/scouting', route => route.fulfill({ json: { success: true, data: { mission: board } }, headers: { ETag: 'fanout' } }))
  await page.goto('/?mission=scouting')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  for (const id of [first.id, sibling.id]) {
    const row = page.getByTestId(`flow-step-${id}`)
    await expect(row.locator('.flow-number')).toHaveText('1 ∥')
    await expect(row).toContainText('Parallel with')
  }
})


test('Flow reads declared values without a saved layout and keeps rows still when a window opens', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const fixture = await cockpitFixture(page, { succeeded: true, declaredInputs: true })
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  const flow = page.getByTestId('flow-view')
  const inputs = flow.getByLabel('Inputs', { exact: true })
  await expect(inputs).toContainText('subject · text · required')
  await expect(inputs).toContainText('facts · file · required')
  await expect(inputs).toContainText('previous · folder · optional')
  await expect(inputs).not.toContainText('Keep the final detail.')
  const row = page.getByTestId('flow-step-execution')
  const before = await row.boundingBox()
  await inputs.getByRole('button').first().click()
  await expect(page.getByRole('dialog', { name: 'file subject', exact: true })).toContainText(flowInputs[0].value)
  expect(await row.boundingBox()).toEqual(before)
  await page.keyboard.press('Escape')
  expect(await row.boundingBox()).toEqual(before)
  await inputs.getByRole('button', { name: '/workspace/identity.md', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'file identity.md', exact: true })).toContainText('Keep the character consistent.')
  await page.keyboard.press('Escape')
  await inputs.getByRole('button', { name: '/workspace/previous', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'file previous/', exact: true })).toContainText('index.html')
  await page.keyboard.press('Escape')
  const outputs = flow.getByLabel('Outputs', { exact: true })
  await expect(outputs).toContainText('summary · text · required')
  await outputs.getByRole('button').click()
  await expect(page.getByRole('dialog', { name: 'file summary', exact: true })).toContainText('All tests pass.')
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('canvas-dock')).toHaveCount(0)
  await expect(page.getByTestId('flow-step-gate').locator('.flow-state')).toHaveText('pass')
  await expect(page.getByTestId('run-banner').locator('.run-when')).toHaveText('by agent:archon')
  const reason = flow.getByRole('button', { name: 'The reviewed implementation passes all checks.' })
  await expect(reason).toBeVisible()
  await reason.click()
  await expect(page.getByRole('dialog', { name: /Review gate/ })).toBeVisible()
  expect(fixture.writes).toEqual([])
})

for (const width of [1920, 2560]) {
  test(`failed gate uses FAIL on Canvas, Flow and attempts at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1920 ? 1080 : 1440 })
    await cockpitFixture(page, { succeeded: true, failedGate: true })
    await page.goto('/?mission=browser&run=run_browser')
    await expect(page.getByTestId('run-chip-gate')).toHaveText('fail')
    await expect(page.getByTestId('run-banner').locator('.badge')).toHaveText('Failed')
    await page.getByTestId('inspect-node-gate').click()
    await expect(page.getByTestId('node-evidence-state')).toHaveText('FAIL')
    const evidenceBox = (await page.locator('.run-evidence').boundingBox())!
    expect(evidenceBox.width).toBeGreaterThanOrEqual(640)
    expect(evidenceBox.height).toBeGreaterThanOrEqual(Math.ceil((width === 1920 ? 1080 : 1440) * .7))
    await page.keyboard.press('Escape')
    await page.getByRole('radio', { name: 'Flow', exact: true }).click()
    await expect(page.getByTestId('flow-step-gate').locator('.flow-state')).toHaveText('fail')
    await page.getByTestId('flow-step-execution').getByTestId('attempts-open-execution').click()
    const attempts = page.getByRole('dialog', { name: 'Attempts of Execution' })
    await expect(attempts.locator('.attempt-verdict-head')).toContainText('fail')
    await expect(attempts).not.toContainText('failed')
  })
}

test('choosing another mission run keeps Flow and Canvas across reloads', async ({ page }) => {
  const fixture = await cockpitFixture(page, { succeeded: true })
  const other = { ...fixture.board(), id: 'other-mission', slug: 'other', title: 'Other mission' }
  const run = { runId: 'run_other', missionId: other.id, missionSlug: 'other', inputCardId: 'mission', status: 'succeeded', final: true, eventCount: 0, inputExcerpt: '', sessions: [] }
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    const respond = (data: unknown) => route.fulfill({ json: { success: true, data }, headers: { ETag: 'other-etag' } })
    if (url.pathname === '/api/missions') return respond({ missions: [fixture.board(), other] })
    if (url.pathname === '/api/missions/other') return respond({ mission: other })
    if (url.pathname === '/api/runs') return respond([run])
    if (url.pathname === '/api/runs/run_other') return respond({ ...run, events: [] })
    if (url.pathname === '/api/runs/run_other/events') return respond({ events: [] })
    return route.fallback()
  })
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  await page.getByTestId('nav-group-other').getByRole('button', { name: /Other mission/ }).click()
  await page.getByTestId('nav-run-run_other').click()
  await expect(page).toHaveURL(/mission=other&run=run_other/)
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'other')
  await expect(page.getByRole('radio', { name: 'Flow', exact: true })).toBeChecked()
  await expect(page.getByTestId('flow-view')).toBeVisible()
  await page.reload()
  await expect(page.getByTestId('flow-view')).toBeVisible()
  await page.getByRole('radio', { name: 'Canvas', exact: true }).click()
  await page.getByTestId('nav-group-browser').locator('.nav-group-toggle').click()
  await page.getByTestId('nav-mission-browser').click()
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'browser')
  await expect(page.getByRole('radio', { name: 'Canvas', exact: true })).toBeChecked()
  await page.reload()
  await expect(page.getByRole('radio', { name: 'Canvas', exact: true })).toBeChecked()
  expect(fixture.writes).toEqual([])
})

for (const width of [1920, 2560]) test(`canceled step chip stays clear of its input header at ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: width === 1920 ? 1080 : 1440 })
  await cockpitFixture(page, { succeeded: true, canceled: true })
  await page.goto('/?mission=browser&run=run_browser')
  const chip = page.getByTestId('run-chip-execution')
  await expect(chip).toHaveText('Canceled')
  const chipBox = (await chip.boundingBox())!
  const inputBox = (await page.locator('[data-node="execution"] .fio.in').boundingBox())!
  expect(chipBox.y + chipBox.height).toBeLessThan(inputBox.y)
})

for (const width of [1920, 2560]) test(`passed gate shows PASS on canvas and evidence at ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: width === 1920 ? 1080 : 1440 })
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser&run=run_browser')
  await expect(page.getByTestId('run-chip-gate')).toHaveText('pass')
  await page.getByTestId('inspect-node-gate').click()
  await expect(page.getByTestId('node-evidence-state')).toHaveText('PASS')
})

for (const width of [1920, 2560]) test(`Flow rows stay still while produced evidence loads at ${width}`, async ({ page }) => {
  await page.setViewportSize({ width, height: width === 1920 ? 1080 : 1440 })
  await cockpitFixture(page, { succeeded: true })
  let releaseEvidence!: () => void
  const held = new Promise<void>(resolve => { releaseEvidence = resolve })
  await page.route('**/api/runs/run_browser/evidence/nodes/execution', async route => {
    await held
    await route.fallback()
  })
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  const output = page.getByTestId('flow-step-execution').getByTestId('produced-execution')
  await expect(output).toBeAttached()
  await expect(output.getByRole('button')).toHaveCount(0)
  const positions = () => page.locator('[data-flow-node]').evaluateAll(rows => rows.map(row => {
    const rect = row.getBoundingClientRect()
    return { id: row.getAttribute('data-flow-node'), y: rect.y, height: rect.height }
  }))
  const before = await positions()
  releaseEvidence()
  await expect(output.getByRole('button').first()).toBeVisible()
  expect(await positions()).toEqual(before)
})
