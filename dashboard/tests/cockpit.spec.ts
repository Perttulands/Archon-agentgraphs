import { expect, test } from '@playwright/test'
import { settledBox } from './settled'
import { cockpitFixture, judgeBlockReason, runCwd, seats } from './cockpit-fixture'

test('theme fallback, local font, notes and harness icons survive', async ({ page }) => {
  const fixture = await cockpitFixture(page, { themeFailure: true })
  await page.goto('/')
  await expect(page.getByRole('status').filter({ hasText: 'Host theme unavailable' })).toBeVisible()
  await expect(page.locator('.formation')).toHaveCount(3)
  expect(await page.locator('.formation').first().evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(26, 26, 26)')
  expect(await page.locator('.world').evaluate(el => getComputedStyle(el).backgroundImage)).not.toBe('none')
  expect(await page.evaluate(() => document.fonts.check('13px "JetBrains Mono"'))).toBe(true)
  await expect(page.locator('.slot svg')).toHaveCount(4)
  const note = page.getByRole('note', { name: 'Notes for Execution' })
  await expect(note.locator('.note-author')).toHaveText('archon')
  await expect(note.locator('.note-count')).toHaveText('+1 earlier')
  await expect(note.locator('.note-sticky-text')).toHaveText('Controller directs the assigned worker.')
  expect(fixture.themeFetches()).toBe(1)
})

for (const width of [1440, 390]) test(`a seated step's window shows the seat's whole grid, sends typing to a working seat, never resizes it, and keeps the graph stable at ${width}px`, async ({ page, context }) => {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const fixture = await cockpitFixture(page, { run: true })
  const frames: string[] = []
  await page.routeWebSocket('**/seats/*/terminal', socket => {
    socket.onMessage(data => {
      const value = Buffer.isBuffer(data) ? data.toString() : data
      frames.push(value)
      if (value.startsWith('{')) socket.send(Buffer.from('0\x1b[32mSCRATCH OUTPUT\x1b[0m\r\n' + Array.from({ length: 45 }, (_, i) => `line ${i}`).join('\r\n')))
    })
  })
  const typed = () => frames.filter(frame => frame.startsWith('0')).map(frame => frame.slice(1)).join('')
  const resizes = () => frames.filter(frame => frame.startsWith('1')).map(frame => JSON.parse(frame.slice(1)) as { columns: number; rows: number })
  await page.goto('/')
  const world = page.locator('.world')
  const card = page.getByTestId('formation-node-execution')
  await expect(card).toBeVisible()
  const transform = await world.getAttribute('style')
  // The card's first click is the step itself: its staffing and its terminal, no button between.
  await card.locator('.tt').click()
  const step = page.getByRole('dialog', { name: 'Formation · Execution' })
  // A phone keeps the room for the terminal; staffing shows with the brief line's fields.
  await expect(step.getByRole('region', { name: 'Staffing' })).toBeVisible({ visible: width !== 390 })
  await expect(step.locator('.xterm-screen')).toBeVisible()
  // A seat working a dispatch takes typing too; it is not on call.
  await expect(step.locator('.peek-seat-on-call')).toHaveCount(0)
  await expect(step.locator('.xterm-rows')).toContainText('line 44')
  const room = step.getByTestId('seat-terminal-room')
  // A narrow viewport scrolls the grid rather than cutting off the seat.
  if (width === 390) await expect(step.getByRole('button', { name: 'End of line' })).toBeVisible()
  await expect.poll(() => room.evaluate(el => el.scrollTop + el.clientHeight >= el.scrollHeight - 1)).toBe(true)
  // Painting a selection copies it, and the window says so.
  const grid = (await step.locator('.xterm-screen').boundingBox())!
  await page.mouse.move(grid.x + 4, grid.y + grid.height - 8)
  await page.mouse.down()
  await page.mouse.move(grid.x + 60, grid.y + grid.height - 8, { steps: 8 })
  await page.mouse.up()
  await expect(step.locator('.xterm-selection div').first()).toBeVisible()
  await expect(step.locator('.peek-foot')).toContainText('Copied selection')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('ine 4')
  expect(await world.getAttribute('style')).toBe(transform)

  // Typing reaches the seat; Escape goes to the agent and leaves the window open.
  await step.locator('.terminal-surface-host').click()
  await page.keyboard.type('hello seat')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect.poll(typed).toBe('hello seat\r\x1b')
  await expect(step).toBeVisible()

  // The step's other seat is one tab away, on its own connection.
  await step.getByRole('button', { name: 'Worker 1', exact: true }).click()
  await expect(step.getByRole('button', { name: 'Worker 1', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(step.locator('.xterm-screen')).toBeVisible()
  // The brief line opens the rest of the step's fields above the terminal.
  await step.locator('.nwin-briefline').click()
  await expect(step.getByText('Brief', { exact: true })).toBeVisible()
  await step.locator('.nwin-briefline').click()

  if (width !== 390) {
    // The window drags by its title bar and stays inside the page.
    const head = step.locator('.fwin-head')
    const rect = (await head.boundingBox())!
    await page.mouse.move(rect.x + rect.width / 3, rect.y + 15)
    await page.mouse.down(); await page.mouse.move(width + 100, 950); await page.mouse.up()
  }
  const close = page.getByRole('button', { name: 'Close Formation · Execution' })
  const closeRect = await settledBox(page, close)
  expect(closeRect.x + closeRect.width).toBeLessThanOrEqual(width)
  await close.click()
  expect(await world.getAttribute('style')).toBe(transform)
  expect(resizes()).toEqual([])
  expect(fixture.writes).toEqual([])
  // Each connection opens with the seat's native grid, then speaks only input and flow control.
  const handshakes = frames.filter(frame => frame.startsWith('{'))
  expect(handshakes.length).toBeGreaterThan(1)
  for (const frame of handshakes) expect(JSON.parse(frame)).toEqual({ AuthToken: '', columns: 96, rows: 30 })
  for (const frame of frames) expect(frame[0]).toMatch(/[{0123]/)
})

test('a step window from the run bar replaces the open one, resizes and stays off the zoom column', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page, { run: true })
  await page.routeWebSocket('**/seats/*/terminal', () => {})
  await page.goto('/')
  await page.getByTestId('formation-node-peer').locator('.tt').click()
  const peer = page.locator('[data-window-id="node:peer"]')
  const run = page.locator('[data-window-id="node:execution"]')
  await expect(peer).toBeVisible()
  // One card window at a time (archon-kiwt.2): a step shown from the run bar replaces it.
  await page.getByTestId('run-banner').getByRole('button', { name: 'running Execution' }).click()
  await expect(run.getByTestId('seat-pane')).toBeVisible()
  await expect(peer).toHaveCount(0)
  const before = await settledBox(page, run)

  // Shrink it from a corner: its opposite corner stays put.
  const corner = (await run.locator('[data-handle="nw"]').boundingBox())!
  await page.mouse.move(corner.x + corner.width / 2, corner.y + corner.height / 2)
  await page.mouse.down()
  await page.mouse.move(before.x + 120, before.y + 100, { steps: 8 })
  await page.mouse.up()
  const after = await settledBox(page, run)
  expect(after.x + after.width).toBeCloseTo(before.x + before.width, 0)
  expect(after.y + after.height).toBeCloseTo(before.y + before.height, 0)
  expect(after.width * after.height).toBeLessThan(before.width * before.height)

  // A title is a keyboard handle as well as a pointer handle.
  await run.locator('.fwin-title').focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(async () => (await settledBox(page, run)).x).toBeGreaterThan(after.x)

  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  for (const zone of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await zone.boundingBox())!
    const rect = await settledBox(page, run)
    const overlaps = rect.x < box.x + box.width && rect.x + rect.width > box.x && rect.y < box.y + box.height && rect.y + rect.height > box.y
    expect(overlaps).toBe(false)
    expect(rect.x).toBeGreaterThanOrEqual(canvas.x)
    expect(rect.y).toBeGreaterThanOrEqual(canvas.y)
    expect(rect.x + rect.width).toBeLessThanOrEqual(canvas.x + canvas.width)
    expect(rect.y + rect.height).toBeLessThanOrEqual(canvas.y + canvas.height)
  }
  // The zoom column stays usable beside the window.
  await page.getByRole('button', { name: 'FIT' }).click()

  await run.locator('.fwin-title').focus()
  await page.keyboard.press('Escape')
  await expect(run).toHaveCount(0)
})

test('a ?mission=&run= link opens that mission and run and keeps the link', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { succeeded: true })
  const requests: string[] = []
  page.on('request', request => requests.push(new URL(request.url()).pathname + new URL(request.url()).search))
  await page.goto('/?mission=browser&run=run_browser&theme=dark')
  await expect(page.getByTestId('mission-title')).toHaveAttribute('data-mission', 'browser')
  await expect(page.getByTestId('run-produced').getByRole('button')).toHaveText(['▤review.md', '+3'])
  await expect(page).toHaveURL(/\/\?mission=browser&run=run_browser&theme=dark$/)
  expect(requests.some(path => path.startsWith('/api/missions/browser'))).toBe(true)
  expect(requests.filter(path => path.startsWith('/api/formations'))).toEqual([])
})

test('reports and other files still open from cards and the produced menu in file windows side by side', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser&run=run_browser')

  const produced = page.getByTestId('run-produced')
  await expect(produced.getByRole('button')).toHaveText(['▤review.md', '+3'])
  // A step's chips sit on its card; the run's final file is in the run bar.
  await page.getByTestId('produced-execution').getByRole('button', { name: 'report' }).click()
  const result = page.getByRole('dialog', { name: 'file report' })
  await expect(result).toContainText('Implemented the reviewed plan.')

  await produced.getByRole('button', { name: '3 more produced files' }).click()
  await page.getByRole('menuitem', { name: 'worker.log' }).click()
  const review = page.getByRole('dialog', { name: 'file worker.log' })
  await expect(review).toContainText('worker finished')
  await expect(review.getByRole('link', { name: 'Open raw' })).toHaveAttribute('href', '/api/runs/run_browser/artifacts/logs/worker.log')
  // Each window opens near where it was opened, clear of it and of the other: the card's chip by the card, the run bar's below the bar.
  const card = (await page.locator('[data-node="execution"]').boundingBox())!
  const opened = (await result.boundingBox())!
  const apart = (a: typeof card, b: typeof card) => a.x >= b.x + b.width || a.x + a.width <= b.x || a.y >= b.y + b.height || a.y + a.height <= b.y
  // The gap between two boxes along the axis that separates them.
  const gap = (a: typeof card, b: typeof card) => Math.hypot(Math.max(0, b.x - (a.x + a.width), a.x - (b.x + b.width)), Math.max(0, b.y - (a.y + a.height), a.y - (b.y + b.height)))
  expect(apart(opened, card)).toBe(true)
  // File chips on Canvas anchor to their whole source card (fileAnchor), so
  // the card stays readable. A wide card's far-side chip is not that anchor.
  expect(gap(opened, card)).toBeLessThanOrEqual(240)
  const bar = (await produced.boundingBox())!
  const below = (await review.boundingBox())!
  expect(below.y).toBeGreaterThanOrEqual(bar.y + bar.height)
  expect(apart(below, opened)).toBe(true)
  // A run bar file opens where it lives: right below the bar, by its chip, over cards if need be but clear of the other window.
  const reviewChip = (await produced.getByRole('button', { name: '3 more produced files' }).boundingBox())!
  expect(gap(below, reviewChip)).toBeLessThanOrEqual(240)
  const banner = (await page.getByTestId('run-banner').boundingBox())!
  expect(apart(below, banner)).toBe(true)

  // Drag the review to the right of the canvas and the result to the left, so the two sit side by side.
  // The review, on top, moves right by its title; the result then moves left by the start of its own title.
  const drag = async (win: typeof review, grip: 'start' | 'end', x: number) => {
    // Raise the intended window before grabbing a title another window may cover.
    await win.focus()
    const head = (await win.locator('.fwin-title').boundingBox())!
    await page.mouse.move(grip === 'start' ? head.x + 6 : head.x + head.width - 6, head.y + head.height / 2)
    await page.mouse.down()
    await page.mouse.move(x, head.y + head.height / 2, { steps: 8 })
    await page.mouse.up()
  }
  await drag(review, 'end', 1900)
  await drag(result, 'start', 0)
  const left = (await result.boundingBox())!
  const right = (await review.boundingBox())!
  expect(left.x + left.width).toBeLessThanOrEqual(right.x)
  await expect(review).toContainText('worker finished')
  await expect(result).toContainText('Implemented the reviewed plan.')
  for (const zone of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await zone.boundingBox())!
    for (const rect of [left, right]) {
      expect(rect.x < box.x + box.width && rect.x + rect.width > box.x && rect.y < box.y + box.height && rect.y + rect.height > box.y).toBe(false)
    }
  }
  await page.screenshot({ path: test.info().outputPath('produced-file-windows.png') })

  // The declared output now opens the producing step's attempts.
  await produced.getByRole('button', { name: 'review.md' }).click()
  await expect(page.getByRole('dialog', { name: 'Attempts of Peer review' })).toContainText('Verdict: revise')
  await expect(page.getByRole('dialog')).toHaveCount(3)
})

test('formation titles stay readable beside the type chip and run controls', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 850 })
  await cockpitFixture(page, { run: true })
  await page.goto('/')
  const titles = page.locator('.formation .fhead .tt')
  await expect(titles).toHaveCount(3)
  for (const title of await titles.all()) {
    expect(await title.evaluate(el => el.scrollWidth <= el.clientWidth), await title.innerText()).toBe(true)
  }
  const card = page.getByTestId('formation-node-peer')
  for (const control of [card.locator('.ftype'), card.getByRole('button', { name: 'Run formation' })]) {
    await expect(control).toBeVisible()
  }
})

test('the run banner docks above the canvas and never covers a card', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 850 })
  await cockpitFixture(page, { run: true })
  await page.goto('/')
  const banner = page.getByTestId('run-banner')
  await expect(banner).toBeVisible()
  await expect(page.locator('.formation .fhead').first()).toBeVisible()
  const bannerBox = (await banner.boundingBox())!
  const canvasBox = (await page.getByTestId('formations-canvas').boundingBox())!
  expect(bannerBox.y + bannerBox.height).toBeLessThanOrEqual(canvasBox.y + 1)
  for (const head of await page.locator('.formation .fhead, .missioncard .mhd, .gatecard').all()) {
    const box = (await head.boundingBox())!
    expect(box.y).toBeGreaterThanOrEqual(bannerBox.y + bannerBox.height)
  }
  // The working directory is said on hover, so a long path never widens the strip.
  await expect(banner.locator('.run-meta')).toHaveAttribute('title', `Runs in ${runCwd}`)
})

test('a run blocked at its judge rings the gate and names it with the reason in the run bar', async ({ page }) => {
  await cockpitFixture(page, { run: true, blockedAtJudge: true })
  await page.goto('/')
  const point = page.getByTestId('run-point')
  await expect(point).toHaveText(`blocked at Review gate: ${judgeBlockReason}`)
  await expect(page.getByTestId('run-banner').locator('.badge')).toHaveText('Blocked')

  const gate = page.getByTestId('gate-node-gate')
  await expect(gate).toHaveClass(/\bblocked\b/)
  await expect(gate.getByTestId('run-chip-gate')).toHaveText('blocked')
  await expect(gate.getByTestId('run-chip-gate')).toBeVisible()
  const error = await gate.evaluate(el => getComputedStyle(el.querySelector('.run-chip')!).color)
  // The card's box-shadow transitions in; read the settled ring.
  await expect.poll(() => gate.evaluate(el => getComputedStyle(el).boxShadow)).toContain(`${error} 0px 0px 0px 1.5px`)
  expect(await page.getByTestId('gate-node-loose').evaluate(el => getComputedStyle(el).boxShadow)).not.toContain(error)
  // Finished steps keep no ring or chip.
  await expect(page.getByTestId('formation-node-execution')).not.toHaveClass(/\b(blocked|failed|running)\b/)
  await expect(page.locator('.run-chip')).toHaveCount(1)
  await page.screenshot({ path: test.info().outputPath('blocked-at-judge.png') })

  // The phrase centres the gate in the canvas.
  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  const offCentre = async () => {
    const box = (await gate.boundingBox())!
    return Math.max(Math.abs(box.x + box.width / 2 - (canvas.x + canvas.width / 2)), Math.abs(box.y + box.height / 2 - (canvas.y + canvas.height / 2)))
  }
  expect(await offCentre()).toBeGreaterThan(40)
  await point.click()
  await expect(gate).toHaveClass(/\blocated\b/)
  await expect.poll(offCentre).toBeLessThan(3)
})

test('a formation without seats shows its fields and never opens a different formation\'s terminal', async ({ page }) => {
  await cockpitFixture(page, { run: true })
  const sockets: string[] = []
  await page.routeWebSocket('**/seats/*/terminal', socket => { sockets.push(socket.url()) })
  await page.goto('/')
  await page.getByTestId('formation-node-peer').locator('.tt').click()
  const step = page.getByRole('dialog', { name: 'Formation · Peer review' })
  await expect(step.getByText('Brief', { exact: true })).toBeVisible()
  await expect(step.getByTestId('seat-pane')).toHaveCount(0)
  expect(sockets).toEqual([])
})

test('a step window follows its worker across attempts, reconnects only when asked, and says when the seat ended', async ({ page }) => {
  const fixture = await cockpitFixture(page, { run: true })
  let current = seats.map(s => ({ ...s }))
  await page.route('**/runs/run_browser/seats', route => route.fulfill({ json: {
    success: true, data: { runId: 'run_browser', available: true, seats: current },
  } }))
  const sockets: string[] = []
  await page.routeWebSocket('**/seats/*/terminal', socket => {
    sockets.push(socket.url())
    socket.onMessage(() => {
      socket.send(Buffer.from('0Scratch worker output\r\n'))
      socket.close({ code: 1001, reason: 'Scratch service stopped' })
    })
  })
  await page.goto('/')
  await page.getByTestId('formation-node-execution').locator('.tt').click()
  const step = page.getByRole('dialog', { name: 'Formation · Execution' })
  await expect(step.getByText('Disconnected')).toBeVisible()
  await step.getByRole('button', { name: 'Worker 1', exact: true }).click()
  await expect(step.getByText('Disconnected')).toBeVisible()
  await expect.poll(() => sockets.length).toBe(2)
  // A dropped terminal never dials again in the background; Reconnect makes exactly one new connection.
  const count = sockets.length
  await page.waitForTimeout(300)
  expect(sockets).toHaveLength(count)
  await step.getByRole('button', { name: 'Reconnect' }).click()
  await expect.poll(() => sockets.length).toBe(count + 1)
  // A new attempt's seat gets its own terminal once the next read of the seats arrives.
  current = current.map(s => s.slotId === 'worker' ? { ...s, createdSeq: 28, terminalUrl: '/api/runs/run_browser/seats/28/terminal' } : s)
  await expect.poll(() => sockets.at(-1), { timeout: 15000 }).toContain('/seats/28/terminal')
  await expect(step.getByRole('button', { name: 'Worker 1', exact: true })).toHaveAttribute('aria-pressed', 'true')
  // Once every seat has ended the window is the step's fields again.
  current = current.map(s => ({ ...s, state: 'ended', terminalUrl: '' }))
  await expect(step.getByTestId('seat-pane')).toHaveCount(0, { timeout: 15000 })
  await expect(step.getByText('Brief', { exact: true })).toBeVisible()
  expect(fixture.writes).toEqual([])
})
