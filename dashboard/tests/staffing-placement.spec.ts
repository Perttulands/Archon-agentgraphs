import { expect, test, type Locator, type Page } from '@playwright/test'
import { agentsFixture } from './agents-fixture'
import { cockpitFixture } from './cockpit-fixture'

/* Where the staffing sentence opens (archon-o7p.17, the placement rethink after
 * rv-slots5): like a dropdown, directly below the clicked slot's row or word
 * and left-aligned with it, or directly above where the view cannot pan to make
 * room. On the canvas, a short room below is made by panning by exactly what
 * the list needs, within 150 ms, before the sentence fades in. It keeps one
 * height while lists change, its lists show whole rows, and the landing note
 * is a line in the slot. One rule on the canvas, in node windows (from the
 * canvas and from Flow) and in the Agents inspector. */

const roles = [
  { id: 'critic', displayName: 'Critic Judge', kind: 'reviewer', summary: 'Reviews against acceptance.' },
  { id: 'scout', displayName: 'Repo Scout', kind: 'scout', summary: 'Explores a codebase.' },
]
const efforts = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']
const GAP = 4

type Box = { left: number; top: number; right: number; bottom: number; width: number; height: number }

/** Records when the canvas moves and when the sentence first shows, from the next press on. */
async function watchOpening(page: Page) {
  // The view first settles from its fit on load.
  await expect.poll(() => page.evaluate(async () => {
    const world = document.querySelector<HTMLElement>('.world')
    const before = world?.style.transform
    await new Promise(resolve => setTimeout(resolve, 250))
    return world?.style.transform === before
  })).toBe(true)
  await page.evaluate(() => {
    const w = window as unknown as { __opening: Opening }
    w.__opening = { moves: 0, started: 0, ended: 0, shown: 0 }
    const world = document.querySelector('.world')
    if (world) {
      new MutationObserver(() => { w.__opening.moves++ }).observe(world, { attributes: true, attributeFilter: ['style'] })
      world.addEventListener('transitionrun', event => { if (event.target === world) w.__opening.started ||= performance.now() })
      world.addEventListener('transitionend', event => { if (event.target === world) w.__opening.ended ||= performance.now() })
    }
    const seen = () => {
      const win = document.querySelector('.staffing-window')
      if (win && !win.classList.contains('placing') && !w.__opening.shown) w.__opening.shown = performance.now()
    }
    new MutationObserver(seen).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] })
  })
}

type Opening = { moves: number; started: number; ended: number; shown: number }

async function opening(page: Page) {
  return page.evaluate(() => (window as unknown as { __opening: Opening }).__opening)
}

/** The open sentence, the row or word it belongs to, and the bounds it must keep to. */
async function measure(page: Page, anchor: Locator, boundsSelector: string | null) {
  const sentence = page.locator('.staffing-window')
  await expect(sentence).not.toHaveClass(/placing/)
  await sentence.evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished)))
  const box = async (locator: Locator): Promise<Box> => locator.evaluate(element => {
    const { left, top, right, bottom, width, height } = element.getBoundingClientRect()
    return { left, top, right, bottom, width, height }
  })
  const bounds: Box = boundsSelector
    ? await box(page.locator(boundsSelector))
    : await page.evaluate(() => ({ left: 0, top: 0, right: innerWidth, bottom: innerHeight, width: innerWidth, height: innerHeight }))
  return { win: await box(sentence), anchor: await box(anchor), bounds }
}

/** The rule: directly below (or above) the anchor, left-aligned unless held in, never over it, inside the bounds. */
function expectDrop({ win, anchor, bounds }: { win: Box; anchor: Box; bounds: Box }, where: string) {
  const below = Math.abs(win.top - (anchor.bottom + GAP)) <= 1.5
  const above = Math.abs(win.bottom - (anchor.top - GAP)) <= 1.5
  expect(below || above, `${where}: the sentence sits right below or above its row (win ${Math.round(win.top)}-${Math.round(win.bottom)}, row ${Math.round(anchor.top)}-${Math.round(anchor.bottom)})`).toBe(true)
  const aligned = Math.abs(win.left - anchor.left) <= 1.5
  const heldIn = Math.abs(win.right - bounds.right) <= 10 && anchor.left > win.left
  expect(aligned || heldIn, `${where}: left-aligned with its row (win ${Math.round(win.left)}, row ${Math.round(anchor.left)})`).toBe(true)
  const covers = win.left < anchor.right && anchor.left < win.right && win.top < anchor.bottom && anchor.top < win.bottom
  expect(covers, `${where}: never over its own row`).toBe(false)
  expect(win.top, `${where}: inside the view`).toBeGreaterThanOrEqual(bounds.top - 1)
  expect(win.bottom, `${where}: inside the view`).toBeLessThanOrEqual(bounds.bottom + 1)
  expect(win.left).toBeGreaterThanOrEqual(bounds.left - 1)
  expect(win.right).toBeLessThanOrEqual(bounds.right + 1)
}

/** Rows cut by the list's edges, and the lines it shows whole. */
async function rows(page: Page) {
  return page.locator('.staffing-window .staffing-list').evaluate(list => {
    const l = list.getBoundingClientRect()
    const all = [...list.querySelectorAll<HTMLElement>('.staffing-row')]
    const cut = all.filter(row => {
      const b = row.getBoundingClientRect()
      return b.bottom > l.top + 0.5 && b.top < l.bottom - 0.5 && (b.top < l.top - 0.5 || b.bottom > l.bottom + 0.5)
    }).map(row => row.dataset.row)
    const whole = new Set(all.filter(row => {
      const b = row.getBoundingClientRect()
      return b.top >= l.top - 0.5 && b.bottom <= l.bottom + 0.5
    }).map(row => Math.round(row.getBoundingClientRect().top))).size
    const total = new Set(all.map(row => row.offsetTop)).size
    const shown = all.filter(row => {
      const b = row.getBoundingClientRect()
      return b.top >= l.top - 0.5 && b.bottom <= l.bottom + 0.5
    }).map(row => row.dataset.row || '')
    return { cut, whole, total, shown }
  })
}

/** Drags empty canvas so the slot's edge meets the canvas's own edge, at the zoom it has. */
async function panSlotTo(page: Page, slot: Locator, edge: 'bottom' | 'right' | 'top') {
  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  const box = (await slot.boundingBox())!
  const dx = edge === 'right' ? canvas.x + canvas.width - 8 - (box.x + box.width) : 0
  const dy = edge === 'bottom' ? canvas.y + canvas.height - 8 - (box.y + box.height) : edge === 'top' ? canvas.y + 8 - box.y : 0
  // A point of empty canvas to drag from, whose destination is still on the canvas.
  const start = await page.evaluate(([cx, cy, cw, ch, mx, my]) => {
    for (let y = cy + 40; y < cy + ch - 40; y += 24) {
      for (let x = cx + 40; x < cx + cw - 40; x += 24) {
        const hit = document.elementFromPoint(x, y)
        const tx = x + mx, ty = y + my
        if (hit?.closest('[data-testid="formations-world"]') !== null && hit?.closest('[data-node], .note-sticky, .zoomctl, .staffing-keyhint, .run-banner, .wire, svg')) continue
        if (!hit?.closest('[data-testid="formations-canvas"]')) continue
        if (tx < cx + 4 || tx > cx + cw - 4 || ty < cy + 4 || ty > cy + ch - 4) continue
        return { x, y }
      }
    }
    return null
  }, [canvas.x, canvas.y, canvas.width, canvas.height, dx, dy])
  expect(start, 'a free point of canvas to pan from').not.toBeNull()
  await page.mouse.move(start!.x, start!.y)
  await page.mouse.down()
  await page.mouse.move(start!.x + dx / 2, start!.y + dy / 2, { steps: 4 })
  await page.mouse.move(start!.x + dx, start!.y + dy, { steps: 4 })
  await page.mouse.up()
  const after = (await slot.boundingBox())!
  if (edge === 'bottom') expect(Math.abs(after.y + after.height - (canvas.y + canvas.height - 8))).toBeLessThanOrEqual(2)
  if (edge === 'top') expect(Math.abs(after.y - (canvas.y + 8))).toBeLessThanOrEqual(2)
  if (edge === 'right') expect(Math.abs(after.x + after.width - (canvas.x + canvas.width - 8))).toBeLessThanOrEqual(2)
}

/**
 * Opens a slot's whole sentence on the canvas and checks the rule with the role
 * list open, the effort list after it, at one height; then the effort word's
 * quick list. The pan, where there is one, is quick and comes before the fade.
 */
async function checkCanvasSlot(page: Page, testId: string, label: string, where: string) {
  const slot = page.getByTestId(testId)
  await watchOpening(page)
  await slot.locator('.slot-ring').click()
  const sentence = page.getByRole('dialog', { name: `Staff ${label}` })
  await expect(sentence).toBeVisible()
  let placed = await measure(page, slot, '[data-testid="formations-canvas"]')
  expectDrop(placed, `${where}, whole sentence`)
  const timing = await opening(page)
  if (timing.moves) {
    // The canvas panned to make room: one quick glide, and only then the sentence.
    expect(timing.moves, `${where}: one pan`).toBe(1)
    expect(timing.ended, `${where}: the pan finished`).toBeGreaterThan(0)
    const panned = timing.ended - timing.started
    expect(panned, `${where}: the pan takes ${Math.round(panned)} ms`).toBeLessThanOrEqual(150)
    expect(timing.shown, `${where}: the sentence fades in after the pan`).toBeGreaterThanOrEqual(timing.ended - 1)
  }
  const height = placed.win.height
  // The role list: about eight lines of it, whole.
  await sentence.getByRole('textbox', { name: /Role, or type/ }).click()
  await expect(sentence.getByRole('listbox', { name: 'Choose role' })).toBeVisible()
  const roleRows = await rows(page)
  expect(roleRows.cut, `${where}: role rows cut`).toEqual([])
  expect(roleRows.whole, `${where}: role lines shown`).toBe(Math.min(8, roleRows.total))
  // Switching lists keeps one height; the six efforts show whole.
  await sentence.locator('[data-token=effort]').click()
  await expect(sentence.getByRole('listbox', { name: 'Choose effort' })).toBeVisible()
  expect((await rows(page)).shown, `${where}: efforts shown whole`).toEqual(efforts)
  placed = await measure(page, slot, '[data-testid="formations-canvas"]')
  expect(Math.abs(placed.win.height - height), `${where}: one height while lists change`).toBeLessThanOrEqual(0.5)
  expectDrop(placed, `${where}, effort list`)
  await page.keyboard.press('Escape')
  await expect(sentence).toHaveCount(0)

  // A word of a staffed slot opens only its list, by the same rule.
  if (await slot.locator('[data-part=effort]').count()) {
    await slot.locator('[data-part=effort]').click()
    await expect(sentence.getByRole('listbox', { name: 'Choose effort' })).toBeVisible()
    expectDrop(await measure(page, slot, '[data-testid="formations-canvas"]'), `${where}, effort word`)
    expect((await rows(page)).shown, `${where}: efforts from the word`).toEqual(efforts)
    await page.keyboard.press('Escape')
    await expect(sentence).toHaveCount(0)
  }
}

for (const size of [{ width: 1920, height: 1080 }, { width: 2560, height: 1440 }]) {
  for (const edge of ['bottom', 'right', 'top'] as const) {
    test(`at exact 100% at ${size.width}x${size.height}, a slot at the ${edge} edge opens its sentence by the dropdown rule`, async ({ page }) => {
      await page.setViewportSize(size)
      await cockpitFixture(page, { roles, extraAgents: 30 })
      await page.goto('/')
      await expect(page.locator('.zoomlevel')).toHaveText('100%')
      const worker = page.getByTestId('slot-execution-worker')
      await panSlotTo(page, worker, edge)
      await checkCanvasSlot(page, 'slot-execution-worker', 'Worker 1', `${size.width} ${edge}`)
      if (edge === 'bottom') {
        // There was no room below: the canvas moved up by exactly the room, and the sentence still drops from the row.
        const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
        expect((await worker.boundingBox())!.y + 100).toBeLessThan(canvas.y + canvas.height)
      }
    })
  }
}

test('at the default fit, established slots open their sentence by the dropdown rule', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles, extraAgents: 30, newRow: true })
  await page.goto('/')
  await expect(page.locator('.zoomlevel')).not.toHaveText('100%')
  for (const [testId, label] of [['slot-execution-controller', 'Controller'], ['slot-execution-worker', 'Worker 1'], ['slot-peer-peer_1', 'Reviewer'], ['slot-judge-judge_1', 'Judge']]) {
    await checkCanvasSlot(page, testId, label, `fit ${label}`)
  }
})

for (const size of [{ width: 1920, height: 1080 }, { width: 2560, height: 1440 }]) {
  test(`a row of three new formations at ${size.width}x${size.height}: each empty slot drops its sentence from its own row`, async ({ page }) => {
    await page.setViewportSize(size)
    await cockpitFixture(page, { roles, extraAgents: 30, newRow: true })
    await page.goto('/')
    for (const id of ['new_1', 'new_2', 'new_3']) {
      await checkCanvasSlot(page, `slot-${id}-agent`, 'Agent', `new row ${id}`)
    }
  })
}

test('a node window\'s staffing words drop the sentence from the word clicked, over the window, never over the word', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles, extraAgents: 30 })
  await page.goto('/')
  await page.getByTestId('formation-node-execution').locator('.fhead .tt').click()
  const staffing = page.getByRole('dialog', { name: 'Formation · Execution' }).getByRole('region', { name: 'Staffing' })
  for (const part of ['effort', 'role'] as const) {
    const word = staffing.getByRole('button', { name: new RegExp(`^Change the ${part} of Worker 1`) })
    await word.click()
    const sentence = page.getByRole('dialog', { name: 'Staff Worker 1' })
    await expect(sentence.getByRole('listbox', { name: `Choose ${part}` })).toBeVisible()
    expectDrop(await measure(page, word, null), `node window ${part}`)
    const listed = await rows(page)
    expect(listed.cut).toEqual([])
    if (part === 'effort') expect(listed.shown).toEqual(efforts)
    else expect(listed.whole).toBe(Math.min(8, listed.total))
    await page.keyboard.press('Escape')
    await expect(sentence).toHaveCount(0)
  }
})

test('a node window opened from Flow drops the sentence from its word by the same rule', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles, extraAgents: 30 })
  await page.goto('/')
  await page.getByRole('radio', { name: 'Flow' }).click()
  await page.getByTestId('flow-view').getByRole('button', { name: /Execution$/ }).first().click()
  const staffing = page.getByRole('dialog', { name: 'Formation · Execution' }).getByRole('region', { name: 'Staffing' })
  for (const [part, slotLabel] of [['effort', 'Worker 1'], ['role', 'Controller']] as const) {
    const word = staffing.getByRole('button', { name: new RegExp(`^Change the ${part} of ${slotLabel}`) })
    await word.click()
    const sentence = page.getByRole('dialog', { name: `Staff ${slotLabel}` })
    await expect(sentence.getByRole('listbox', { name: `Choose ${part}` })).toBeVisible()
    expectDrop(await measure(page, word, null), `Flow ${part}`)
    expect((await rows(page)).cut).toEqual([])
    await page.keyboard.press('Escape')
    await expect(sentence).toHaveCount(0)
  }
})

test('the Agents inspector drops the sentence from its word by the same rule', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await agentsFixture(page)
  await page.goto('/?mission=delivery')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  const agents = page.getByTestId('agents-view')
  await agents.getByTestId('agents-slot-recheck-recheck_seat').click()
  const button = agents.getByRole('dialog', { name: 'Inspector' }).getByRole('button', { name: 'Staff Second opinion' })
  await button.click()
  await expect(page.getByRole('dialog', { name: 'Staff Second opinion' })).toBeVisible()
  expectDrop(await measure(page, button, null), 'Agents inspector')
})

test('the landing note is a line in the slot itself: it moves with the slot and covers nothing', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles })
  await page.goto('/')
  const judge = page.getByTestId('slot-judge-judge_1')
  await judge.locator('[data-part=role]').click()
  await page.locator('.staffing-window [data-row="critic"]').click()
  const note = judge.getByTestId('staffing-stamp')
  await expect(note).toHaveText('Claude Code · default model · medium stays: the slot keeps its settings. Policy suggests xhigh: reviewing falls under architecture and review.')
  // One line in the slot's height; its whole text is the tooltip.
  await expect(note).toHaveAttribute('title', /Policy suggests xhigh/)
  await expect(page.locator('.staffing-window')).toHaveCount(0)
  // Inside the slot's own box, on its label line.
  const inside = async () => {
    const [n, s] = [(await note.boundingBox())!, (await judge.boundingBox())!]
    return n.x >= s.x && n.y >= s.y && n.x + n.width <= s.x + s.width && n.y + n.height <= s.y + s.height
  }
  expect(await inside()).toBe(true)
  // A zoom moves the slot; the note goes with it.
  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  await page.mouse.move(canvas.x + 200, canvas.y + canvas.height - 60)
  await page.mouse.wheel(0, 200)
  await expect(note).toBeVisible()
  expect(await inside()).toBe(true)
  await expect(judge).toHaveClass(/staffing-noted/)
})

test('under reduced motion a landing leaves no highlight behind', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles, emptyWorker: true })
  await page.goto('/')
  const worker = page.getByTestId('slot-execution-worker')
  await worker.click()
  await page.keyboard.press('Enter')
  await expect(worker.locator('.slot-caption.landed')).toHaveCount(1)
  const layers = await worker.evaluate(slot => [slot.querySelector('.slot-caption.landed')!, slot.querySelector('.face.landed')!]
    .map(element => getComputedStyle(element, '::after').content))
  expect(layers).toEqual(['none', 'none'])
})

test('a press dragged inside a list leaves it on whole rows', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { roles, extraAgents: 30 })
  await page.goto('/')
  await page.getByTestId('slot-execution-controller').locator('[data-part=role]').click()
  const list = page.locator('.staffing-window .staffing-list')
  await expect(list).toBeVisible()
  const box = (await list.boundingBox())!
  await page.mouse.move(box.x + 40, box.y + 10)
  await page.mouse.down()
  await page.mouse.move(box.x + 40, box.y + box.height + 60, { steps: 10 })
  await page.waitForTimeout(400)
  await page.mouse.up()
  await page.waitForTimeout(250)
  expect((await rows(page)).cut).toEqual([])
  const offset = await list.evaluate(element => {
    const rowsAt = [...new Set([...element.querySelectorAll<HTMLElement>('.staffing-row')].map(row => row.offsetTop))].sort((a, b) => a - b)
    const pitch = rowsAt[1] - rowsAt[0]
    return element.scrollTop % pitch
  })
  expect(offset).toBe(0)
  // Nothing was selected by the drag.
  expect(await page.evaluate(() => String(getSelection()))).toBe('')
})
