import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Node = { id: string; title: string }

test('Flow shows all 7 Wayfinding steps readable at 1440x900 without horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
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
  await expect(steps.nth(6)).toContainText('→ run ends')

  // A row opens its node window beside it, and the Flow view is remembered for the board.
  await steps.nth(4).getByRole('button', { name: '5 Draft the brief' }).click()
  await expect(page.getByRole('dialog', { name: 'Formation · Draft the brief' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.reload()
  await expect(page.getByTestId('flow-view')).toBeVisible()
  const critic = wayfinding.board.formations.find((node: Node) => node.title === 'Brief critic')
  await expect(page.getByTestId(`flow-step-${critic.id}`)).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})

test('Flow opens windows beside the clicked title or route link, and its run point says it opens the step', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await wayfindingFixture(page)
  const gate = wayfinding.board.gates.find((node: Node) => node.title === 'Adversarial review')
  const run = { runId: 'run_wayfinding', status: 'blocked', final: false, resumeAllowed: false, boardSlug: 'wayfinding', missionId: wayfinding.board.missions[0].id, eventCount: 2 }
  const reply = (data: unknown) => ({ json: { success: true, data } })
  // A blocked run on Adversarial review, added after the fixture so these routes answer first.
  await page.route(url => url.pathname.startsWith('/api/formations/runs'), route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/formations/runs') return route.fulfill(reply([run]))
    if (path === `/api/formations/runs/${run.runId}`) return route.fulfill(reply({ status: run }))
    if (path.endsWith('/events')) return route.fulfill(reply({ events: [
      { runId: run.runId, seq: 1, type: 'gate_evaluating', nodeId: gate.id, gateId: gate.id },
      { runId: run.runId, seq: 2, type: 'run_blocked', nodeId: gate.id, gateId: gate.id },
    ] }))
    if (path.endsWith('/evidence/problems')) return route.fulfill(reply({ problems: [
      { seq: 2, type: 'run_blocked', nodeIds: [gate.id], reason: { text: 'invalid judge result: missing verdict block', bytes: 42 }, resumeAllowed: false },
    ] }))
    if (path.endsWith('/escalations')) return route.fulfill(reply({ escalations: [] }))
    return route.fulfill({ status: 404, json: { success: false, error: { message: `Fixture has no ${path}` } } })
  })
  await page.goto('/?board=wayfinding')
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
