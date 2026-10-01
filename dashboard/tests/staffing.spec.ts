import { expect, test, type Page } from '@playwright/test'
import { agentsFixture } from './agents-fixture'
import { cockpitFixture } from './cockpit-fixture'

/* Staffing a slot is a sentence at the slot (archon-o7p.17, variant A of
 * archon-n7u.56): every word its own control, typing, 1-6 for the effort and
 * Enter to accept, on the mission's own slots. */

test.use({ viewport: { width: 1920, height: 1080 } })

const roles = [
  { id: 'critic', displayName: 'Critic Judge', kind: 'reviewer', summary: 'Reviews against acceptance.' },
  { id: 'scout', displayName: 'Repo Scout', kind: 'scout', summary: 'Explores a codebase.' },
  { id: 'final', displayName: 'Final Reviewer', kind: 'reviewer' },
  { id: 'codex-reviewer', displayName: 'Codex Reviewer', kind: 'reviewer' },
]
const caption = (page: Page, testId: string) => page.getByTestId(testId).getByTestId('slot-caption')
const assignments = (patches: Array<Record<string, unknown>>) => patches.map(patch => patch.assignSlot).filter(Boolean)

test('an empty slot staffs in two inputs, says so in place, and one Ctrl+Z empties it again', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, roles })
  await page.goto('/')
  const worker = page.getByTestId('slot-execution-worker')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', '')
  await expect(worker).toContainText('+ Agent')

  await worker.click()
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  // The sentence already says what Enter staffs: vanilla on the first harness and model, at the step's policy effort.
  await expect(sentence.getByRole('textbox', { name: /Role, or type/ })).toHaveAttribute('placeholder', 'vanilla')
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Claude Code', 'opus', 'medium'])
  await expect(sentence.getByTestId('staffing-policy')).toContainText('The step “Execution” makes things, so medium.')
  // The slot previews the draft while it is composed.
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Claude Code · opus · medium')
  await expect(worker.locator('.slot-caption.pending')).toHaveCount(1)

  await page.keyboard.press('Enter')
  await expect(sentence).toHaveCount(0)
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Claude Code · opus · medium')
  await expect(worker.locator('.slot-caption.landed')).toHaveCount(1)
  await expect.poll(() => assignments(fixture.patches)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: '', harness: 'claude-code', model: 'opus', effort: 'medium' }])

  await page.mouse.click(1000, 950)
  await page.keyboard.press('Control+z')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', '')
  expect(assignments(fixture.patches).at(-1)).toEqual({ formationId: 'execution', slotId: 'worker', agentId: '', harness: '', model: '', effort: '' })
})

test('the keyboard path: N reaches the empty slot, typed words fill the sentence, and Enter staffs it with the policy\'s reason', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, roles })
  await page.goto('/')
  await expect(page.getByTestId('staffing-keyhint')).toContainText('N next empty slot · 1–6 effort on a focused slot')
  await page.mouse.click(1000, 950)
  await page.keyboard.press('n')
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  await expect(sentence).toBeVisible()
  await expect(page.getByTestId('staffing-keyhint')).toHaveCount(0)
  await page.keyboard.type('cri ast')
  await expect(sentence.getByTestId('staffing-read').locator('.staffing-read-w')).toHaveText(['ast model gpt-6-astra', 'cri role Critic Judge'])
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Codex', 'gpt-6-astra', 'xhigh'])
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Critic Judge | Codex · gpt-6-astra · xhigh')
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Harness is now Codex: gpt-6-astra runs there, not on Claude Code. Effort xhigh: Critic Judge is review or architecture work, so xhigh.')
  await expect.poll(() => assignments(fixture.patches)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: 'critic', harness: 'openai-codex', model: 'gpt-6-astra', effort: 'xhigh' }])
  // N on a mission with no empty slot says so.
  await page.mouse.click(1000, 950)
  await page.keyboard.press('n')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('No empty slot in this mission.')
})

test('a word of a staffed slot opens only its list; the effort list offers the policy first and names what a harness refuses', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const reviewer = page.getByTestId('slot-peer-peer_1')
  await reviewer.locator('[data-part=effort]').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Reviewer' })
  const list = sentence.getByRole('listbox', { name: 'Choose effort' })
  // The role outranks its step: Codex builder makes things, so the list opens on medium.
  await expect(list.getByRole('option')).toHaveText([/^low\s*errands/, /^medium\s*suggested\s*making things/, /^high/, /^xhigh\s*architecture and review/, /^max\s*consequential reviews/, /^ultra/])
  await expect(list.getByRole('option', { selected: true })).toHaveAttribute('data-row', 'medium')
  await expect(sentence.getByTestId('staffing-policy')).toHaveText('Policy suggests medium: Codex builder makes things, so medium.')
  await list.locator('[data-row="xhigh"]').click()
  await expect(caption(page, 'slot-peer-peer_1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · xhigh')
  await expect.poll(() => assignments(fixture.patches).at(-1)).toEqual({ formationId: 'peer', slotId: 'peer_1', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'xhigh' })

  // Claude Code goes up to max: ultra is struck through with its reason, and its digit is refused in words.
  const controller = page.getByTestId('slot-execution-controller')
  await controller.locator('[data-part=effort]').click()
  const ultra = page.getByRole('dialog', { name: 'Staff Controller' }).locator('[data-row="ultra"]')
  await expect(ultra).toHaveAttribute('aria-disabled', 'true')
  await expect(ultra).toContainText('Claude Code goes up to max; ultra is Codex only')
  await page.keyboard.press('6')
  await expect(page.getByRole('dialog', { name: 'Staff Controller' }).getByTestId('staffing-policy')).toHaveText('Claude Code goes up to max; ultra is Codex only.')
  await page.keyboard.press('Escape')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · medium')
})

test('a known Codex model narrows the efforts offered to its own (archon-n7u.50), and only harnesses Archon starts are offered (archon-n7u.13)', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const reviewer = page.getByTestId('slot-peer-peer_1')
  await reviewer.locator('[data-part=harness]').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Reviewer' })
  await expect(sentence.getByRole('listbox', { name: 'Choose harness' }).getByRole('option')).toHaveText([/^Claude Code/, /^Codex/])
  await page.keyboard.press('Escape')

  await reviewer.locator('[data-part=model]').click()
  const models = page.getByRole('dialog', { name: 'Staff Reviewer' }).getByRole('listbox', { name: 'Choose model' })
  await expect(models.getByRole('option')).toHaveText([/^gpt-6-astra/, /^gpt-5.6-sol/, /^gpt-5.5/, /^default model/, /^opus.*switches harness/, /^sonnet/, /^haiku/, /^fable/])
  await models.locator('[data-row="openai-codex:gpt-5.5"]').click()
  await expect(caption(page, 'slot-peer-peer_1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · gpt-5.5 · medium')

  await reviewer.locator('[data-part=effort]').click()
  const efforts = page.getByRole('dialog', { name: 'Staff Reviewer' }).getByRole('listbox', { name: 'Choose effort' })
  for (const refused of ['max', 'ultra']) {
    await expect(efforts.locator(`[data-row="${refused}"]`)).toHaveAttribute('aria-disabled', 'true')
    await expect(efforts.locator(`[data-row="${refused}"]`)).toContainText('gpt-5.5 goes up to xhigh')
  }
  await expect(efforts.locator('[data-row="xhigh"]')).toHaveAttribute('aria-disabled', 'false')
  await page.keyboard.press('Escape')
  expect(assignments(fixture.patches)).toEqual([{ formationId: 'peer', slotId: 'peer_1', agentId: 'codex', harness: 'openai-codex', model: 'gpt-5.5', effort: 'medium' }])
})

test('a role landing follows the policy, but an effort picked by hand stays with a standing offer that one click takes', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  // A seeded effort follows the policy as the role lands, and the note says why.
  const judge = page.getByTestId('slot-judge-judge_1')
  await judge.locator('[data-part=role]').click()
  const grid = page.getByRole('dialog', { name: 'Staff Judge' }).getByRole('listbox', { name: 'Choose role' })
  await expect(grid.locator('[data-row="critic"]')).toContainText('xhigh')
  await grid.locator('[data-row="critic"]').click()
  await expect(caption(page, 'slot-judge-judge_1')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · default model · xhigh')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Effort xhigh: Critic Judge is review or architecture work, so xhigh.')

  // A hand-picked effort stays; the policy's suggestion waits on the slot.
  const controller = page.getByTestId('slot-execution-controller')
  await controller.focus()
  await page.keyboard.press('3')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · high')
  await controller.locator('[data-part=role]').click()
  await page.getByRole('dialog', { name: 'Staff Controller' }).locator('[data-row="critic"]').click()
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · default model · high')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('high stays: you picked it by hand. Policy suggests xhigh: Critic Judge is review or architecture work, so xhigh.')
  const offer = controller.getByTestId('staffing-offer')
  await expect(offer).toHaveText('use xhigh?')
  await page.waitForTimeout(9500)
  await expect(offer).toBeVisible()
  await offer.click()
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · default model · xhigh')
  await expect(offer).toHaveCount(0)
  await expect.poll(() => assignments(fixture.patches).at(-1)).toEqual({ formationId: 'execution', slotId: 'controller', agentId: 'critic', harness: 'claude-code', model: '', effort: 'xhigh' })
})

test('Esc and a click away change nothing; mistakes are named before they are written', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const controller = page.getByTestId('slot-execution-controller')
  const before = 'Claude controller | Claude Code · default model · medium'

  // A press on the slot, not on one of its words, opens the whole sentence.
  await controller.locator('.slot-ring').click()
  await page.keyboard.type('haiku')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · haiku · medium')
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('staffing-sentence')).toHaveCount(0)
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', before)

  await controller.locator('.slot-ring').click()
  await page.keyboard.type('max')
  await page.mouse.click(1000, 950)
  await expect(page.getByTestId('staffing-sentence')).toHaveCount(0)
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', before)

  // A typo is named with its nearest model, and Enter refuses it.
  await controller.locator('.slot-ring').click()
  await page.keyboard.type('opsu')
  const sentence = page.getByRole('dialog', { name: 'Staff Controller' })
  await expect(sentence.getByTestId('staffing-read')).toContainText('No model “opsu”. Did you mean opus?')
  await page.keyboard.press('Enter')
  await expect(sentence).toBeVisible()
  await sentence.getByRole('button', { name: 'instead: opus' }).click()
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · opus · medium')

  // A word several roles answer shows them and commits nothing until one is picked.
  await controller.locator('.slot-ring').click()
  await page.keyboard.type('review')
  await page.keyboard.press('Enter')
  const choices = sentence.getByRole('listbox', { name: 'Roles that match' })
  await expect(choices.getByRole('option')).toHaveText([/^Codex Reviewer/, /^Final Reviewer/])
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · opus · medium')
  await page.keyboard.press('Escape')

  // A model outside the catalog is staffed, with a warning on the slot.
  await controller.locator('.slot-ring').click()
  await page.keyboard.type('gpt-7-nova')
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · gpt-7-nova · medium')
  await expect(controller.locator('.slot-warn')).toHaveText('model not in catalog')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('gpt-7-nova: not in the catalog; the harness decides.')
  expect(assignments(fixture.patches)).toEqual([
    { formationId: 'execution', slotId: 'controller', agentId: 'claude', harness: 'claude-code', model: 'opus', effort: 'medium' },
    { formationId: 'execution', slotId: 'controller', agentId: 'claude', harness: 'claude-code', model: 'gpt-7-nova', effort: 'medium' },
  ])
})

test('a wrong slot is fixed in one drag that swaps, one Ctrl+Z puts both back, and a drop that misses changes nothing (archon-n2w)', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const controller = page.getByTestId('slot-execution-controller')
  const worker = page.getByTestId('slot-execution-worker')
  const drag = async (from: { x: number; y: number }, to: { x: number; y: number }) => {
    await page.mouse.move(from.x, from.y)
    await page.mouse.down()
    await page.mouse.move(to.x, to.y, { steps: 12 })
    await page.mouse.up()
  }
  const center = async (target: typeof worker) => { const box = (await target.boundingBox())!; return { x: box.x + box.width / 2, y: box.y + box.height / 2 } }

  await drag(await center(controller), { x: 1000, y: 950 })
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Not on a slot, so nothing changed.')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · medium')
  expect(assignments(fixture.patches)).toEqual([])

  await drag(await center(controller), await center(worker))
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · medium')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · medium')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Swapped: Worker 1 takes Claude controller, Controller takes Codex builder.')

  await page.keyboard.press('Control+z')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · medium')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · medium')
})

test('a role dragged from the rail lands by the same rule, previews on the slot, and a missed drop says so', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, roles })
  await page.goto('/')
  const scout = page.getByTestId('roster-agent-scout')
  const worker = page.getByTestId('slot-execution-worker')
  const from = (await scout.boundingBox())!
  const to = (await worker.boundingBox())!
  await page.mouse.move(from.x + 40, from.y + 10)
  await page.mouse.down()
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 15 })
  await expect(worker).toHaveClass(/snaptarget/)
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Repo Scout | Claude Code · opus · low')
  await page.mouse.up()
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Effort low: Repo Scout runs errands, so low.')
  await expect.poll(() => assignments(fixture.patches)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: 'scout', harness: 'claude-code', model: 'opus', effort: 'low' }])

  await page.mouse.move(from.x + 40, from.y + 10)
  await page.mouse.down()
  await page.mouse.move(1000, 950, { steps: 10 })
  await page.mouse.up()
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Not on a slot, so nothing changed.')
  expect(assignments(fixture.patches)).toHaveLength(1)
})

test('the slot menu staffs in place and empties a slot on purpose with undo', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const worker = page.getByTestId('slot-execution-worker')
  await worker.click({ button: 'right' })
  const menu = page.getByRole('menu', { name: 'Slot · Worker 1' })
  await expect(menu.getByRole('menuitem')).toHaveText(['Staff Worker 1…', 'Empty Worker 1', 'Make controller'])
  await menu.getByRole('menuitem', { name: 'Empty Worker 1' }).click()
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', '')
  await page.keyboard.press('Control+z')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · medium')
  await worker.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Staff Worker 1…' }).click()
  await expect(page.getByRole('dialog', { name: 'Staff Worker 1' })).toBeVisible()
  expect(assignments(fixture.patches)).toEqual([
    { formationId: 'execution', slotId: 'worker', agentId: '', harness: '', model: '', effort: '' },
    { formationId: 'execution', slotId: 'worker', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'medium' },
  ])
})

test('changing what a slot runs moves no other slot or card', async ({ page }) => {
  await cockpitFixture(page, { roles })
  await page.goto('/')
  const rects = () => page.evaluate(() => [...document.querySelectorAll('.world .slot, .world [data-node]')].map(element => {
    const box = element.getBoundingClientRect()
    return `${(element as HTMLElement).dataset.testid || (element as HTMLElement).dataset.node}:${Math.round(box.x)},${Math.round(box.y)},${Math.round(box.width)},${Math.round(box.height)}`
  }))
  const controller = page.getByTestId('slot-execution-controller')
  await expect(controller).toBeVisible()
  await page.waitForTimeout(500)
  const before = await rects()
  expect(before).toHaveLength(10)
  await controller.locator('[data-part=role]').click()
  await page.getByRole('dialog', { name: 'Staff Controller' }).locator('[data-row="final"]').click()
  // A final review is consequential: the policy puts it at max.
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Final Reviewer | Claude Code · default model · max')
  await page.waitForTimeout(700)
  expect(await rects()).toEqual(before)
})

test('the node window\'s staffing words open the same sentence beside it', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  await page.getByTestId('formation-node-execution').locator('.fhead .tt').click()
  const execution = page.getByRole('dialog', { name: 'Formation · Execution' })
  const staffing = execution.getByRole('region', { name: 'Staffing' })
  await staffing.getByRole('button', { name: 'Change the effort of Worker 1: medium' }).click()
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  await expect(sentence.getByRole('listbox', { name: 'Choose effort' })).toBeVisible()
  const [frame, box] = [(await execution.boundingBox())!, (await sentence.boundingBox())!]
  expect(box.x >= frame.x + frame.width || box.x + box.width <= frame.x).toBe(true)
  await page.keyboard.press('1')
  await expect(staffing).toContainText('Worker 1 is Codex builder on Codex · default model · low.')
  await expect.poll(() => assignments(fixture.patches)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'low' }])
})

test('the Agents view slot inspector staffs through the same sentence', async ({ page }) => {
  const fixture = await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await agents.getByTestId('agents-slot-recheck-recheck_seat').click()
  const inspector = agents.getByRole('complementary', { name: 'Inspector' })
  await inspector.getByRole('button', { name: 'Staff Second opinion' }).click()
  const sentence = page.getByRole('dialog', { name: 'Staff Second opinion' })
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Claude Code', 'opus', 'low'])
  await page.keyboard.type('critic')
  await page.keyboard.press('Enter')
  await expect(agents.getByTestId('agents-slot-recheck-recheck_seat').getByTestId('slot-caption')).toHaveAttribute('data-staffing', 'Brief critic | Claude Code · opus · xhigh')
  await expect(inspector.getByTestId('slot-staffing-words')).toHaveText('Second opinion (controller) is Brief critic on Claude Code · opus · xhigh.')
  expect(fixture.boardPatches).toEqual([{ assignSlot: { formationId: 'recheck', slotId: 'recheck_seat', agentId: 'critic', harness: 'claude-code', model: 'opus', effort: 'xhigh' } }])
})
