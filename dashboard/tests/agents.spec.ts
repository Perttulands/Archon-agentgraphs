import { expect, test } from '@playwright/test'
import { agentsFixture } from './agents-fixture'

test.use({ viewport: { width: 1920, height: 1080 } })

test('Boards and Agents share one current board across view switches, reloads and fresh opens', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?board=wayfinding')
  await expect(page.getByTestId('board-picker')).toHaveValue('wayfinding')

  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await expect(agents.getByRole('combobox', { name: 'Board' })).toHaveValue('wayfinding')
  await expect(agents.locator('section.formation .tt')).toHaveText(['Map'])

  await agents.getByRole('combobox', { name: 'Board' }).selectOption('delivery')
  await expect(page).toHaveURL(/\?board=delivery$/)
  await page.getByRole('button', { name: 'Boards', exact: true }).click()
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')

  await page.reload()
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')

  // A fresh open with no query lands on the last used board, not the first by name.
  await page.goto('/')
  await expect(page.getByTestId('board-picker')).toHaveValue('delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await expect(agents.getByRole('combobox', { name: 'Board' })).toHaveValue('delivery')
})

test('a mission is not ready while a slot in its judge chain is open', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?board=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')

  await expect(agents.locator('.missioncard')).toContainText('3/4 slots staffed · 1 open')
  await expect(agents.locator('.missioncard')).not.toContainText('ready')
  await expect(agents.locator('.gatecard')).toContainText('judged by Beads reviewer → Second opinion')
  await expect(agents.locator('section.formation', { hasText: 'Second opinion' })).toContainText('judges Beads review')
  await expect(agents.getByRole('complementary', { name: 'Agent roster' }).locator('.roster-hd .s')).toContainText('2 on mission')

  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByText('Slots on this mission')).toBeVisible()
  await expect(inspector.locator('.tool-detail-row', { hasText: 'Beads reviewer' })).toBeVisible()
  await expect(inspector.getByText('No slots on this mission.')).toHaveCount(0)
})

test('a persona\'s model and effort are shown and edited per harness variant, with the command seats run', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?board=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')

  await agents.getByRole('button', { name: 'Inspect Builder' }).click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  const codex = inspector.getByRole('form', { name: 'openai-codex harness variant' })
  const claude = inspector.getByRole('form', { name: 'claude-code harness variant' })
  await expect(inspector.getByText('harness default · effort medium (default)')).toBeVisible()
  await expect(codex.getByLabel('openai-codex model', { exact: true })).toHaveAttribute('placeholder', 'harness default')
  await expect(codex.getByLabel('openai-codex effort', { exact: true })).toHaveValue('')
  await expect(codex.getByTestId('seat-launch-openai-codex')).toHaveText(`exec '/usr/local/bin/codex' -c 'model_reasoning_effort="medium"' -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox`)

  // Each variant offers only the efforts its harness takes.
  await expect(codex.getByLabel('openai-codex effort', { exact: true }).locator('option', { hasText: 'ultra' })).toHaveCount(1)
  await expect(claude.getByLabel('claude-code effort', { exact: true }).locator('option', { hasText: 'ultra' })).toHaveCount(0)

  await claude.getByLabel('claude-code model', { exact: true }).fill('claude-opus-5')
  await claude.getByLabel('claude-code effort', { exact: true }).selectOption('high')
  await claude.getByLabel('claude-code model', { exact: true }).press('Enter')
  await expect(claude.getByRole('status')).toHaveText('Saved')
  await expect(claude.getByTestId('seat-launch-claude-code')).toHaveText(`exec '/usr/local/bin/claude' --model 'claude-opus-5' --effort 'high' --dangerously-skip-permissions`)
  expect(fixture.patches).toEqual([{ variants: [{ id: 'claude-code', model: 'claude-opus-5', effort: 'high' }] }])

  // A legacy launch string is read and explained, never offered as a field.
  await agents.getByRole('button', { name: 'Inspect Brief critic' }).click()
  const critic = inspector.getByRole('form', { name: 'claude-code harness variant' })
  await expect(critic.getByLabel('claude-code model', { exact: true })).toHaveValue('claude-opus-5')
  await expect(critic.getByLabel('claude-code effort', { exact: true })).toHaveValue('low')
  await expect(critic.locator('.ph-legacy')).toContainText('legacy launch string, claude. Seats do not use it')
  await inspector.getByRole('button', { name: 'Edit persona' }).click()
  const editor = page.getByTestId('persona-editor')
  await expect(editor.getByLabel('Agent display name')).toHaveValue('Brief critic')
  await expect(editor.getByLabel('Agent launch command')).toHaveCount(0)
  await editor.getByLabel('claude-code effort', { exact: true }).selectOption('')
  await editor.getByRole('button', { name: 'Save agent override' }).click()
  await expect(editor).toHaveCount(0)
  await expect(critic.getByTestId('seat-launch-claude-code')).toContainText(`--effort 'medium'`)
  expect(fixture.patches.at(-1)).toMatchObject({ displayName: 'Brief critic', variants: [{ id: 'claude-code', effort: '' }] })
})

test('a hermes persona keeps an editable launch command, the one archon agent spawn runs', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?board=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await agents.getByRole('button', { name: 'Inspect Hermes spawner' }).click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await expect(inspector.getByText('Archon cannot start hermes seats.')).toBeVisible()
  await inspector.getByRole('button', { name: 'Edit persona' }).click()
  const editor = page.getByTestId('persona-editor')
  const launch = editor.getByLabel('hermes launch command (archon agent spawn)')
  await expect(launch).toHaveValue("hermes --profile '/profiles/old'")
  await expect(editor.getByLabel('hermes model', { exact: true })).toHaveCount(0)
  await launch.fill("hermes --profile '/profiles/new'")
  await editor.getByRole('button', { name: 'Save agent override' }).click()
  await expect(editor).toHaveCount(0)
  expect(fixture.patches.at(-1)).toMatchObject({ variants: [{ id: 'hermes', launch: "hermes --profile '/profiles/new'" }] })
  await expect(inspector.getByLabel('hermes launch command (archon agent spawn)')).toHaveValue("hermes --profile '/profiles/new'")
})
