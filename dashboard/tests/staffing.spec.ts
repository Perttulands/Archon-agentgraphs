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
  await expect(sentence.getByTestId('staffing-policy')).toContainText('the step “Execution” reads as making things.')
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
  // An empty slot takes the policy for the role's kind.
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Harness is now Codex: gpt-6-astra runs there, not on Claude Code. Effort xhigh: reviewing falls under architecture and review.')
  await expect.poll(() => assignments(fixture.patches)).toEqual([{ formationId: 'execution', slotId: 'worker', agentId: 'critic', harness: 'openai-codex', model: 'gpt-6-astra', effort: 'xhigh' }])
  // N on a mission with no empty slot says so.
  await page.mouse.click(1000, 950)
  await page.keyboard.press('n')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('No empty slot in this mission.')
})

test('a word of a staffed slot opens only its list, on the slot\'s current value, and names what a harness refuses', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  const reviewer = page.getByTestId('slot-peer-peer_1')
  await reviewer.locator('[data-part=effort]').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Reviewer' })
  const list = sentence.getByRole('listbox', { name: 'Choose effort' })
  // The role's kind decides the suggestion: Codex builder builds, which falls under making things.
  await expect(list.getByRole('option')).toHaveText([/^low\s*errands/, /^medium\s*suggested\s*making things/, /^high/, /^xhigh\s*architecture and review/, /^max\s*consequential reviews/, /^ultra/])
  await expect(list.getByRole('option', { selected: true })).toHaveAttribute('data-row', 'medium')
  await expect(sentence.getByTestId('staffing-policy')).toHaveText('Policy suggests medium: building falls under making things.')
  await list.locator('[data-row="xhigh"]').click()
  await expect(caption(page, 'slot-peer-peer_1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · xhigh')
  await expect.poll(() => assignments(fixture.patches).at(-1)).toEqual({ formationId: 'peer', slotId: 'peer_1', agentId: 'codex', harness: 'openai-codex', model: '', effort: 'xhigh' })

  // Every quick-edit list opens on what the slot holds, never on its first row or the suggestion,
  // so a reflex Enter changes nothing.
  for (const [part, row] of [['effort', 'xhigh'], ['harness', 'openai-codex'], ['model', 'openai-codex:'], ['role', 'codex']]) {
    await reviewer.locator(`[data-part=${part}]`).click()
    const open = page.getByRole('dialog', { name: 'Staff Reviewer' }).getByRole('listbox', { name: `Choose ${part}` })
    await expect(open.getByRole('option', { selected: true })).toHaveAttribute('data-row', row)
    await expect(open.locator('[aria-current="true"]')).toHaveAttribute('data-row', row)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog', { name: 'Staff Reviewer' })).toHaveCount(0)
  }
  await expect(caption(page, 'slot-peer-peer_1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · default model · xhigh')
  expect(assignments(fixture.patches)).toHaveLength(1)

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
  // A blank model is the harness default: that row is the current one.
  await expect(models.locator('.staffing-row.current')).toHaveAttribute('data-row', 'openai-codex:')
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

  // A model the catalog does not know keeps the harness's whole list.
  await reviewer.locator('.slot-ring').click()
  await page.keyboard.type('gpt-7-nova')
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-peer-peer_1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · gpt-7-nova · medium')
  await reviewer.locator('[data-part=effort]').click()
  const unknown = page.getByRole('dialog', { name: 'Staff Reviewer' }).getByRole('listbox', { name: 'Choose effort' })
  for (const effort of ['max', 'ultra']) await expect(unknown.locator(`[data-row="${effort}"]`)).toHaveAttribute('aria-disabled', 'false')
  await page.keyboard.press('Escape')
  expect(assignments(fixture.patches)).toEqual([
    { formationId: 'peer', slotId: 'peer_1', agentId: 'codex', harness: 'openai-codex', model: 'gpt-5.5', effort: 'medium' },
    { formationId: 'peer', slotId: 'peer_1', agentId: 'codex', harness: 'openai-codex', model: 'gpt-7-nova', effort: 'medium' },
  ])
})

test('a role landing on a staffed slot keeps its harness, model and effort, and offers the policy\'s effort in one click', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles })
  await page.goto('/')
  // The Judge slot is staffed: the critic lands on its settings, and the note says why.
  const judge = page.getByTestId('slot-judge-judge_1')
  await judge.locator('[data-part=role]').click()
  const grid = page.getByRole('dialog', { name: 'Staff Judge' }).getByRole('listbox', { name: 'Choose role' })
  await expect(grid.locator('[data-row="critic"]')).toContainText('xhigh')
  await grid.locator('[data-row="critic"]').click()
  await expect(caption(page, 'slot-judge-judge_1')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · default model · medium')
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Claude Code · default model · medium stays: the slot keeps its settings. Policy suggests xhigh: reviewing falls under architecture and review.')
  const offer = judge.getByTestId('staffing-offer')
  await expect(offer).toHaveText('use xhigh?')
  await page.waitForTimeout(9500)
  await expect(offer).toBeVisible()
  await offer.click()
  await expect(caption(page, 'slot-judge-judge_1')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · default model · xhigh')
  await expect(offer).toHaveCount(0)

  // The whole sentence of a staffed slot offers it too, in the window, before anything is written.
  const controller = page.getByTestId('slot-execution-controller')
  await controller.focus()
  await page.keyboard.press('3')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Claude controller | Claude Code · default model · high')
  await controller.locator('.slot-ring').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Controller' })
  await sentence.getByRole('textbox', { name: /Role, or type/ }).click()
  await sentence.locator('[data-row="final"]').click()
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Claude Code', 'default model', 'high'])
  await sentence.getByTestId('staffing-window-offer').click()
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Claude Code', 'default model', 'xhigh'])
  await expect(sentence.getByTestId('staffing-window-offer')).toHaveCount(0)
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Final Reviewer | Claude Code · default model · xhigh')
  await expect.poll(() => assignments(fixture.patches)).toEqual([
    { formationId: 'judge', slotId: 'judge_1', agentId: 'critic', harness: 'claude-code', model: '', effort: 'medium' },
    { formationId: 'judge', slotId: 'judge_1', agentId: 'critic', harness: 'claude-code', model: '', effort: 'xhigh' },
    { formationId: 'execution', slotId: 'controller', agentId: 'claude', harness: 'claude-code', model: '', effort: 'high' },
    { formationId: 'execution', slotId: 'controller', agentId: 'final', harness: 'claude-code', model: '', effort: 'xhigh' },
  ])
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
  // The ghost waits beside the slot, off the caption it previews.
  const [ghost, preview] = [(await page.locator('.staffing-ghost').boundingBox())!, (await caption(page, 'slot-execution-worker').boundingBox())!]
  expect(ghost.x + ghost.width <= preview.x || ghost.x >= preview.x + preview.width || ghost.y + ghost.height <= preview.y || ghost.y >= preview.y + preview.height).toBe(true)
  await page.mouse.up()
  await expect(page.getByTestId('staffing-stamp')).toHaveText('Effort low: scouting falls under errands.')
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
  // A role landing on a staffed slot keeps its settings and offers the policy's effort beside the label.
  await expect(caption(page, 'slot-execution-controller')).toHaveAttribute('data-staffing', 'Final Reviewer | Claude Code · default model · medium')
  await expect(controller.getByTestId('staffing-offer')).toHaveText('use xhigh?')
  await page.waitForTimeout(700)
  expect(await rects()).toEqual(before)
})

// archon-o7p.17 revision 3: the sentence is a compact popover right beside its slot that covers no card or note.
test('the sentence opens right beside its slot, compact, and covers no card or operator note', async ({ page }) => {
  await cockpitFixture(page, { roles })
  await page.goto('/')
  for (const [slot, label] of [['slot-execution-controller', 'Controller'], ['slot-execution-worker', 'Worker 1'], ['slot-peer-peer_1', 'Reviewer'], ['slot-judge-judge_1', 'Judge']]) {
    await page.getByTestId(slot).locator('.slot-ring').click()
    const sentence = page.getByRole('dialog', { name: `Staff ${label}` })
    await sentence.getByRole('textbox', { name: /Role, or type/ }).click()
    await expect(sentence.getByRole('listbox', { name: 'Choose role' })).toBeVisible()
    const placed = await page.evaluate(([slotId]) => {
      const r = (el: Element) => el.getBoundingClientRect()
      const win = r(document.querySelector('.staffing-window')!)
      const anchor = r(document.querySelector(`[data-testid="${slotId}"]`)!)
      const gap = Math.hypot(Math.max(0, anchor.left - win.right, win.left - anchor.right), Math.max(0, anchor.top - win.bottom, win.top - anchor.bottom))
      const covered = [...document.querySelectorAll('.world [data-node], .note-sticky')].filter(el => {
        const b = r(el)
        return win.left < b.right && b.left < win.right && win.top < b.bottom && b.top < win.bottom
      }).map(el => (el as HTMLElement).dataset.node || 'note')
      return { gap: Math.round(gap), covered, height: Math.round(win.height) }
    }, [slot])
    expect(placed.covered, `${label}'s sentence covers ${placed.covered.join(', ')}`).toEqual([])
    expect(placed.gap).toBeLessThanOrEqual(160)
    expect(placed.height).toBeLessThanOrEqual(330)
    await page.keyboard.press('Escape')
    await expect(sentence).toHaveCount(0)
  }
})

test('an effort chosen in the open sentence counts as stated: a role landing on an empty slot keeps it and offers the policy, as typed words do', async ({ page }) => {
  const fixture = await cockpitFixture(page, { emptyWorker: true, roles })
  await page.goto('/')
  const worker = page.getByTestId('slot-execution-worker')
  await worker.click()
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  await page.keyboard.press('3')
  await expect(sentence.locator('[data-token=effort]')).toHaveText('high')
  await sentence.getByRole('textbox', { name: /Role, or type/ }).click()
  await sentence.locator('[data-row="critic"]').click()
  await expect(sentence.locator('[data-token]')).toHaveText(['', 'Claude Code', 'opus', 'high'])
  await expect(sentence.getByTestId('staffing-policy')).toContainText('high stays: you chose it. Policy suggests xhigh: reviewing falls under architecture and review.')
  await expect(sentence.getByTestId('staffing-window-offer')).toHaveText('use xhigh')
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · opus · high')
  await expect(worker.getByTestId('staffing-offer')).toHaveText('use xhigh?')

  // Typed words state the same intent and land the same way.
  await page.mouse.click(1000, 950)
  await page.keyboard.press('Control+z')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', '')
  await worker.click()
  await page.keyboard.type('high cri')
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-execution-worker')).toHaveAttribute('data-staffing', 'Critic Judge | Claude Code · opus · high')
  await expect(worker.getByTestId('staffing-offer')).toHaveText('use xhigh?')
  const landed = assignments(fixture.patches).filter(patch => patch.agentId === 'critic')
  expect(landed).toEqual([landed[0], landed[0]])
  expect(landed[0]).toEqual({ formationId: 'execution', slotId: 'worker', agentId: 'critic', harness: 'claude-code', model: 'opus', effort: 'high' })
})

test('a model outside the catalog opens its list on itself, so a reflex Enter changes nothing', async ({ page }) => {
  const fixture = await cockpitFixture(page, { roles, workers: [
    { id: 'w1', label: 'Worker 1', agentId: 'codex', harness: 'openai-codex', model: 'gpt-7-nova', effort: 'medium', controller: false },
  ] })
  await page.goto('/')
  const worker = page.getByTestId('slot-execution-w1')
  await worker.locator('[data-part=model]').click()
  const models = page.getByRole('dialog', { name: 'Staff Worker 1' }).getByRole('listbox', { name: 'Choose model' })
  await expect(models.getByRole('option', { selected: true })).toHaveAttribute('data-row', 'openai-codex:gpt-7-nova')
  await expect(models.locator('[aria-current="true"]')).toContainText('gpt-7-nova')
  await page.keyboard.press('Enter')
  await expect(caption(page, 'slot-execution-w1')).toHaveAttribute('data-staffing', 'Codex builder | Codex · gpt-7-nova · medium')
  expect(assignments(fixture.patches)).toEqual([])
})

test('the landing note sits right beside its slot and covers no card or operator note', async ({ page }) => {
  await cockpitFixture(page, { roles })
  await page.goto('/')
  // The Execution card has an operator note under it.
  await expect(page.locator('.note-sticky')).toHaveCount(1)
  for (const slot of ['slot-execution-controller', 'slot-execution-worker', 'slot-judge-judge_1']) {
    await page.getByTestId(slot).locator('[data-part=role]').click()
    await page.locator('.staffing-window [data-row="critic"]').click()
    const stamp = page.getByTestId('staffing-stamp')
    await expect(stamp).toBeVisible()
    const placed = await page.evaluate(([slotId]) => {
      const r = (el: Element) => el.getBoundingClientRect()
      const note = r(document.querySelector('[data-testid="staffing-stamp"]')!)
      const anchor = r(document.querySelector(`[data-testid="${slotId}"]`)!)
      const gap = Math.hypot(Math.max(0, anchor.left - note.right, note.left - anchor.right), Math.max(0, anchor.top - note.bottom, note.top - anchor.bottom))
      const covered = [...document.querySelectorAll('.world [data-node], .note-sticky')].filter(el => {
        const b = r(el)
        return note.left < b.right && b.left < note.right && note.top < b.bottom && b.top < note.bottom
      }).map(el => (el as HTMLElement).dataset.node || 'operator note')
      return { gap: Math.round(gap), covered }
    }, [slot])
    expect(placed.covered, `the note for ${slot} covers ${placed.covered.join(', ')}`).toEqual([])
    expect(placed.gap).toBeLessThanOrEqual(160)
    await page.mouse.click(1000, 1000)
  }
})

test('a long role sentence wraps what the slot runs as one group, so the effort stays with its model', async ({ page }) => {
  await cockpitFixture(page, { roles: [{ id: 'long', displayName: 'Wayfinding Opus critic and release gatekeeper', kind: 'reviewer' }], workers: [
    { id: 'w1', label: 'Worker 1', agentId: 'long', harness: 'openai-codex', model: 'gpt-5.6-terra', effort: 'xhigh', controller: false },
  ] })
  await page.goto('/')
  await page.getByTestId('slot-execution-w1').locator('.slot-ring').click()
  const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
  const top = async (selector: string) => Math.round((await sentence.locator(selector).boundingBox())!.y)
  const line = await top('[data-token=effort]')
  expect(Math.abs(await top('[data-token=model]') - line)).toBeLessThanOrEqual(3)
  expect(Math.abs(await top('[data-token=harness]') - line)).toBeLessThanOrEqual(3)
  for (const token of ['harness', 'model', 'effort']) {
    expect(await sentence.locator(`[data-token=${token}]`).evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
  }
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
  // It drops from the word clicked, over its own window if need be, never over the word.
  const [word, box] = [(await staffing.getByRole('button', { name: 'Change the effort of Worker 1: medium' }).boundingBox())!, (await sentence.boundingBox())!]
  const gap = Math.hypot(Math.max(0, word.x - (box.x + box.width), box.x - (word.x + word.width)), Math.max(0, word.y - (box.y + box.height), box.y - (word.y + word.height)))
  expect(gap).toBeLessThanOrEqual(12)
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

// archon-n7u.38: the rail says which roles are in use, labels every count in the
// Agents view's words, and opens a role in place.
test('the rail states in words which roles are in use and opens a role in a window beside it', async ({ page }) => {
  await cockpitFixture(page, { roles })
  await page.goto('/')
  const roster = page.getByTestId('agent-roster')
  await expect(page.getByTestId('roster-count')).toHaveText('6 roles · 2 in use')
  const codex = page.getByTestId('roster-agent-codex')
  await expect(codex.locator('.r')).toHaveText('in 2 slots · builder')
  await expect(page.getByTestId('roster-agent-claude').locator('.r')).toHaveText('in 2 slots · controller')
  await expect(page.getByTestId('roster-agent-scout').locator('.r')).toHaveText('scout')
  // In use is said, not shown by dimming the row.
  expect(await codex.evaluate(element => getComputedStyle(element).opacity)).toBe('1')

  await codex.click()
  const role = page.getByRole('dialog', { name: 'role Codex builder' })
  await expect(role).toContainText('builder')
  await expect(role).toContainText('Builds the change.')
  await expect(role.getByRole('region', { name: 'Slots this role staffs' }).locator('li')).toHaveText([
    'Execution · Worker 1Codex · default model · medium',
    'Peer review · ReviewerCodex · default model · medium',
  ])
  const [row, window] = [(await codex.boundingBox())!, (await role.boundingBox())!]
  expect(window.x).toBeGreaterThanOrEqual(row.x + row.width - 1)
  await role.getByRole('button', { name: 'Open Peer review' }).click()
  await expect(page.getByRole('dialog', { name: 'Formation · Peer review' })).toBeVisible()
  await role.getByRole('button', { name: 'Edit role' }).click()
  await expect(page.getByTestId('persona-editor')).toBeVisible()
  await page.keyboard.press('Escape')

  await page.getByTestId('roster-agent-scout').click()
  await expect(page.getByRole('dialog', { name: 'role Repo Scout' })).toContainText('Not in use. Drag it onto a slot, or pick it in a slot’s sentence.')
  await expect(roster).toBeVisible()
})

test('the Agents view counts roles in use in the rail\'s words', async ({ page }) => {
  await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await expect(page.getByTestId('roster-count')).toHaveText('3 roles · 2 in use')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const roster = page.getByTestId('agents-view').getByRole('complementary', { name: 'Agent roster' })
  await expect(roster.locator('.roster-hd .s')).toHaveText('3 roles · 2 in use')
  // In use leads the row's words and is never cut off.
  const words = roster.getByRole('button', { name: /Inspect Builder/ }).locator('.r')
  await expect(words.locator('span').first()).toHaveText('in 2 slots')
  expect(await words.evaluate(element => {
    const line = element.getBoundingClientRect()
    const inUse = element.querySelector('.in-use-words')!.getBoundingClientRect()
    return inUse.right <= line.right && inUse.left >= line.left
  })).toBe(true)
})
