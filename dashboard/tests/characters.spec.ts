import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

/* A character is a saved staffing with a name and a picture, stamped onto a
 * slot: the slot takes its values and keeps no link to it (archon-fh5c.7). */

test.use({ viewport: { width: 1920, height: 1080 } })

const careful = { id: 'careful-builder', name: 'Careful builder', description: 'Builds slowly', agentId: 'codex', harness: 'openai-codex', effort: 'high' }
const assignments = (patches: Array<Record<string, unknown>>) => patches.map(patch => patch.assignSlot).filter(Boolean)

test('a character stamps a slot from its step window, and editing it afterwards changes no mission', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, characters: [careful] })
  await page.goto('/')
  await page.locator('[data-node="execution"] .tt').click()
  const step = page.getByRole('dialog', { name: /Execution/ })
  await step.getByLabel('Staff Worker 1 as a character').selectOption('careful-builder')
  await expect.poll(() => assignments(fixture.patches)).toEqual([
    { formationId: 'execution', slotId: 'worker', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'high' },
  ])
  await expect(step.getByLabel(/Change the effort of Worker 1: high/)).toBeVisible()

  await page.getByTestId('roles-toggle').click()
  const row = page.getByTestId('character-careful-builder')
  await expect(row).toContainText('Careful builder')
  await expect(row).toContainText('Codex builder · default model · high')
  await row.click()
  const editor = page.getByTestId('character-editor')
  await editor.getByLabel('Character effort').selectOption('low')
  await editor.getByRole('button', { name: 'Save character' }).click()
  await expect(editor).toHaveCount(0)
  await expect(row).toContainText('· low')
  expect(fixture.characterWrites).toEqual([{ method: 'PUT', id: 'careful-builder', body: { ...careful, effort: 'low' } }])
  expect(assignments(fixture.patches)).toHaveLength(1)
  await expect(step.getByLabel(/Change the effort of Worker 1: high/)).toBeVisible()
})

test('dragging a character from the roster onto a slot staffs it', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, characters: [careful] })
  await page.goto('/')
  await page.getByTestId('roles-toggle').click()
  const from = (await page.getByTestId('character-careful-builder').boundingBox())!
  const to = (await page.getByTestId('slot-execution-worker').boundingBox())!
  await page.mouse.move(from.x + 20, from.y + 12)
  await page.mouse.down()
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 })
  await page.mouse.up()
  await expect.poll(() => assignments(fixture.patches)).toEqual([
    { formationId: 'execution', slotId: 'worker', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'high' },
  ])
})
