import { expect, test } from '@playwright/test'
import { agentsFixture } from './agents-fixture'
import { cockpitFixture } from './cockpit-fixture'

test.use({ viewport: { width: 1920, height: 1080 } })

// archon-vps.2: wherever a slot appears it says what it runs, in plain words.
test('the canvas, Flow and the node window show every slot\'s harness, model and effort, and its role', async ({ page }) => {
  const fixture = await cockpitFixture(page, { vanillaWorker: true })
  await page.goto('/')
  const controller = page.getByTestId('slot-execution-controller')
  const worker = page.getByTestId('slot-execution-worker')
  const reviewer = page.getByTestId('slot-peer-peer_1')
  await expect(controller.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · medium')
  await expect(worker.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Claude Code · opus · low')
  await expect(reviewer.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · medium')
  // On the card: the role (or "vanilla") over harness · model · effort, with the slot's label above.
  await expect(worker).toContainText('Worker 1')
  await expect(worker.locator('.slot-cap-role')).toHaveText('vanilla')
  await expect(worker.locator('.slot-cap-line')).toHaveText('Claude Code · opus · low')
  await expect(controller.locator('.slot-cap-role')).toHaveText('Claude controller')
  await expect(controller.locator('.slot-cap-line')).toHaveText('Claude Code · default · medium')
  await expect(worker).toHaveAttribute('title', 'Worker 1 is vanilla on Claude Code · opus · low.')
  // Every caption fits its card at the default fit: no word leaves the card, the effort is never cut.
  for (const slot of await page.locator('.formation .slot').all()) {
    const fits = await slot.evaluate(element => {
      const card = element.closest('.formation')!.getBoundingClientRect()
      const effort = element.querySelector('.slot-w-effort') as HTMLElement | null
      const words = [...element.querySelectorAll('.slot-w')].map(word => word.getBoundingClientRect())
      return words.every(box => box.left >= card.left && box.right <= card.right) && (!effort || effort.scrollWidth <= effort.clientWidth)
    })
    expect(fits).toBe(true)
  }

  // The roster lists roles by name; roles carry no harness.
  const roster = page.getByTestId('agent-roster')
  await expect(roster.locator('.roster-group-label')).toHaveText(['Roles'])
  await expect(roster.locator('.ragent .n')).toHaveText(['Claude controller', 'Codex builder'])
  await expect(roster.locator('.ragent .av svg')).toHaveCount(0)

  await page.getByTestId('formation-node-execution').locator('.fhead .tt').click()
  const execution = page.getByRole('dialog', { name: 'Formation · Execution' })
  const staffing = execution.getByRole('region', { name: 'Staffing' })
  await expect(staffing).toContainText('Controller is Claude controller on Claude Code · default model · medium.')
  await expect(staffing).toContainText('Worker 1 is vanilla on Claude Code · opus · low.')
  await page.keyboard.press('Escape')

  await page.getByRole('radio', { name: 'Flow' }).click()
  const flowStep = page.getByTestId('flow-step-execution')
  await expect(flowStep).toContainText('Controller is Claude controller on Claude Code · default model · medium. Worker 1 is vanilla on Claude Code · opus · low.')
  await expect(page.getByTestId('flow-step-peer')).toContainText('Reviewer is Codex builder on Codex · default model · medium.')
  expect(fixture.writes).toEqual([])
})

test('the Agents view slot tiles and slot inspector show harness, model, effort and role', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  const build = agents.getByTestId('agents-slot-build-build_seat')
  await expect(build.getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Builder | Codex · default model · medium')
  await expect(agents.getByTestId('agents-slot-recheck-recheck_seat').getByTestId('slot-caption')).toHaveAttribute('data-staffing', '')
  await expect(agents.getByTestId('agents-slot-recheck-recheck_seat')).toContainText('+ Agent')

  await build.click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByTestId('slot-staffing-words')).toHaveText('Build (controller) is Builder on Codex · default model · medium.')
  for (const [term, value] of [['Role', 'Builder'], ['Harness', 'Codex'], ['Model', 'default model'], ['Effort', 'medium']]) {
    await expect(inspector.locator('.tool-detail-identity div', { hasText: term }).locator('dd')).toHaveText(value)
  }
  // The roster lists roles without grouping them by harness.
  const roster = agents.getByRole('complementary', { name: 'Agent roster' })
  await expect(roster.locator('.roster-group-label')).toHaveText(['Roles'])
  await expect(roster.locator('.ragent .n')).toHaveText(['Brief critic', 'Builder', 'Hermes spawner'])
})
