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

for (const width of [1440, 390]) test(`floating Peek shows the seat's whole grid, sends typing to a working seat, never resizes it, and keeps the graph stable at ${width}px`, async ({ page, context }) => {
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
  await expect(page.getByRole('button', { name: 'Open terminal' })).toBeEnabled()
  const world = page.locator('.world')
  const transform = await world.getAttribute('style')
  await page.getByRole('button', { name: 'Open terminal' }).click()
  await expect(page.getByText('Live · type to talk to the agent', { exact: true })).toBeVisible()
  // A seat working a dispatch takes typing too; it is not on call.
  await expect(page.locator('.peek-on-call, .peek-seat-on-call')).toHaveCount(0)
  await expect(page.locator('.xterm-screen')).toBeVisible()
  const rows = page.locator('.xterm-rows')
  await expect(rows).toContainText('line 44')
  const peek = page.getByRole('dialog', { name: 'Formation terminal Peek' })
  const room = peek.getByTestId('seat-terminal-room')
  // Peek opens where its placement leaves room, beside the graph. There the
  // seat's grid is drawn at the 11px floor and scrolls rather than being cut,
  // newest rows in view.
  await expect.poll(() => room.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
  await expect(peek.getByRole('button', { name: 'End of line' })).toBeVisible()
  await expect.poll(() => room.evaluate(el => el.scrollTop + el.clientHeight >= el.scrollHeight - 1)).toBe(true)
  // Painting a selection copies it, and Peek says so.
  const grid = (await page.locator('.xterm-screen').boundingBox())!
  await page.mouse.move(grid.x + 4, grid.y + grid.height - 8)
  await page.mouse.down()
  await page.mouse.move(grid.x + 60, grid.y + grid.height - 8, { steps: 8 })
  await page.mouse.up()
  await expect(page.locator('.xterm-selection div').first()).toBeVisible()
  await expect(peek.locator('.peek-foot')).toContainText('Copied selection')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('ine 4')
  expect(await world.getAttribute('style')).toBe(transform)

  // Typing reaches the seat; Escape goes to the agent and leaves the window open.
  await page.locator('.terminal-surface-host').click()
  await page.keyboard.type('hello seat')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect.poll(typed).toBe('hello seat\r\x1b')
  await expect(page.getByRole('dialog', { name: 'Formation terminal Peek' })).toBeVisible()

  if (width === 1440) {
    // Sizing the window larger fits a larger font until the whole grid shows;
    // the seat keeps its size.
    // Grown up and to the right by dragging the corner. The first drag is the
    // operator sizing the window, so Peek must not fit itself back mid-drag.
    const before = (await page.locator('.xterm-screen').boundingBox())!
    const corner = (await page.locator('.floating-peek [data-handle="ne"]').boundingBox())!
    const peekBox = (await peek.boundingBox())!
    const target = { x: corner.x + Math.min(500, 1370 - peekBox.x - peekBox.width), y: Math.max(160, corner.y - 500) }
    const grab = { x: corner.x + corner.width / 2, y: corner.y + corner.height / 2 }
    await page.mouse.move(grab.x, grab.y)
    await page.mouse.down()
    // Unpaced moves, faster than a frame: the window must follow the pointer
    // every step and never shrink back to wrap the grid mid-drag.
    const sizes: { width: number, height: number }[] = []
    for (let i = 1; i <= 24; i++) {
      await page.mouse.move(grab.x + (target.x - grab.x) * i / 24, grab.y + (target.y - grab.y) * i / 24)
      const box = (await peek.boundingBox())!
      sizes.push({ width: box.width, height: box.height })
    }
    await page.mouse.up()
    for (let i = 1; i < sizes.length; i++) {
      expect(sizes[i].width, `width shrank mid-drag at step ${i}`).toBeGreaterThanOrEqual(sizes[i - 1].width - 1)
      expect(sizes[i].height, `height shrank mid-drag at step ${i}`).toBeGreaterThanOrEqual(sizes[i - 1].height - 1)
    }
    const dragged = await settledBox(page, peek)
    // The drag grew the window both ways (the workspace edge may clamp it), and
    // what is remembered is the size the window really has.
    expect(dragged.width).toBeGreaterThan(peekBox.width + 100)
    expect(dragged.height).toBeGreaterThan(peekBox.height + 100)
    const remembered = await page.evaluate(() => JSON.parse(localStorage.getItem('archon.floatingWindowSize.v1') || 'null')?.sizes?.peek ?? null)
    expect(remembered, 'the dragged size is remembered').not.toBeNull()
    expect(Math.abs(remembered.width - dragged.width)).toBeLessThan(2)
    expect(Math.abs(remembered.height - dragged.height)).toBeLessThan(2)
    await expect.poll(async () => (await page.locator('.xterm-screen').boundingBox())!.width).toBeGreaterThan(before.width)
    await expect.poll(() => room.evaluate(el => el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight)).toBe(true)
    await expect(peek.getByRole('button', { name: 'End of line' })).toHaveCount(0)
    await expect(rows).toContainText('line 44')
  }
  expect(resizes()).toEqual([])

  const before = fixture.seatsFetches()
  await page.getByRole('button', { name: 'Worker 1', exact: true }).click()
  await expect(page.getByText('Live · type to talk to the agent', { exact: true })).toBeVisible()
  expect(fixture.seatsFetches()).toBeGreaterThan(before)
  const head = page.locator('.peek-head')
  const rect = (await head.boundingBox())!
  await page.mouse.move(rect.x + rect.width / 2, rect.y + 15)
  await page.mouse.down(); await page.mouse.move(width + 100, 950); await page.mouse.up()
  const close = page.getByRole('button', { name: 'Close terminal Peek' })
  const closeRect = await settledBox(page, close)
  expect(closeRect.x + closeRect.width).toBeLessThanOrEqual(width)
  await close.click()
  expect(await world.getAttribute('style')).toBe(transform)
  await page.getByRole('button', { name: 'Open terminal' }).click()
  await expect(page.getByText('Live · type to talk to the agent', { exact: true })).toBeVisible()
  expect(await world.getAttribute('style')).toBe(transform)
  expect(fixture.writes).toEqual([])
  // Each connection opens with the seat's native grid, then speaks only input and flow control.
  const handshakes = frames.filter(frame => frame.startsWith('{'))
  expect(handshakes.length).toBeGreaterThan(1)
  for (const frame of handshakes) expect(JSON.parse(frame)).toEqual({ AuthToken: '', columns: 96, rows: 30 })
  for (const frame of frames) expect(frame[0]).toMatch(/[{0123]/)
})

test('two floating windows open, resize, stack and stay off the zoom column', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await cockpitFixture(page, { run: true })
  await page.routeWebSocket('**/seats/*/terminal', () => {})
  await page.goto('/')
  await page.getByRole('button', { name: 'Peek at Peer review', exact: true }).click()
  await page.getByRole('button', { name: 'Open terminal' }).click()
  const peer = page.locator('[data-window-id="peek:peer"]')
  const run = page.locator('[data-window-id="peek:"]')
  await expect(peer).toBeVisible()
  await expect(run).toBeVisible()
  // The run window wraps its seat's grid once its font is fitted; measure it after that.
  await expect(run.locator('.seat-terminal-grid.fitted')).toBeVisible()
  const z = (win: typeof run) => win.evaluate(el => Number(getComputedStyle(el).zIndex))
  await expect.poll(async () => await z(run) > await z(peer)).toBe(true)
  const before = await settledBox(page, run)
  // The run window opens clear of the peer window's title bar, so both can still be grabbed.
  const peerHead = await settledBox(page, peer.locator('.peek-head'))
  expect(before.y >= peerHead.y + peerHead.height || before.y + before.height <= peerHead.y
    || before.x >= peerHead.x + peerHead.width || before.x + before.width <= peerHead.x).toBe(true)

  // Grow it from the corner that has room: its opposite corner stays put.
  const room = { left: before.x - 300, top: before.y - 200 }
  const corner = (await run.locator('[data-handle="nw"]').boundingBox())!
  await page.mouse.move(corner.x + corner.width / 2, corner.y + corner.height / 2)
  await page.mouse.down()
  await page.mouse.move(room.left, room.top, { steps: 8 })
  await page.mouse.up()
  const after = await settledBox(page, run)
  expect(after.x + after.width).toBeCloseTo(before.x + before.width, 0)
  expect(after.y + after.height).toBeCloseTo(before.y + before.height, 0)
  expect(after.width * after.height).toBeGreaterThan(before.width * before.height)

  // The peer's header stays uncovered even after the run window grew.
  const head = await settledBox(page, peer.locator('.peek-head'))
  await page.mouse.move(head.x + 8, head.y + 8)
  await page.mouse.down()
  await page.mouse.move(1600, 1100, { steps: 8 })
  await page.mouse.up()
  await expect.poll(async () => await z(peer) > await z(run)).toBe(true)

  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  for (const zone of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await zone.boundingBox())!
    for (const win of [run, peer]) {
      const rect = await settledBox(page, win)
      const overlaps = rect.x < box.x + box.width && rect.x + rect.width > box.x && rect.y < box.y + box.height && rect.y + rect.height > box.y
      expect(overlaps).toBe(false)
      expect(rect.x).toBeGreaterThanOrEqual(canvas.x)
      expect(rect.y).toBeGreaterThanOrEqual(canvas.y)
      expect(rect.x + rect.width).toBeLessThanOrEqual(canvas.x + canvas.width)
      expect(rect.y + rect.height).toBeLessThanOrEqual(canvas.y + canvas.height)
    }
  }
  // The zoom column stays usable with both windows pressed against it.
  await page.getByRole('button', { name: 'FIT' }).click()

  await peer.locator('.peek-title').focus()
  await page.keyboard.press('Escape')
  await expect(peer).toHaveCount(0)
  await expect(run).toBeVisible()
})

test('a ?mission=&run= link opens that mission and run and keeps the link', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { succeeded: true })
  const requests: string[] = []
  page.on('request', request => requests.push(new URL(request.url()).pathname + new URL(request.url()).search))
  await page.goto('/?mission=browser&run=run_browser&theme=dark')
  await expect(page.getByTestId('board-picker')).toHaveValue('browser')
  await expect(page.getByTestId('run-produced').getByRole('button')).toHaveText(['▤review.md', '+3'])
  await expect(page).toHaveURL(/\/\?mission=browser&run=run_browser&theme=dark$/)
  expect(requests.some(path => path.startsWith('/api/missions/browser'))).toBe(true)
  expect(requests.filter(path => path.startsWith('/api/formations'))).toEqual([])
})

test('a finished run opens what it produced from the run bar and cards in file windows side by side', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser&run=run_browser')

  const produced = page.getByTestId('run-produced')
  await expect(produced.getByRole('button')).toHaveText(['▤review.md', '+3'])
  // A step's chips sit on its card; the run's final file is in the run bar.
  await page.getByTestId('produced-execution').getByRole('button', { name: 'Result' }).click()
  const result = page.getByRole('dialog', { name: 'file Result' })
  await expect(result).toContainText('All tests pass.')

  await produced.getByRole('button', { name: 'review.md' }).click()
  const review = page.getByRole('dialog', { name: 'file review.md' })
  await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()
  await expect(review.locator('strong', { hasText: 'revise' })).toBeVisible()
  await expect(review.getByRole('link', { name: 'Open raw' })).toHaveAttribute('href', '/api/runs/run_browser/artifacts/review.md')
  // Each window opens near where it was opened, clear of it and of the other: the card's chip by the card, the run bar's below the bar.
  const card = (await page.locator('[data-node="execution"]').boundingBox())!
  const opened = (await result.boundingBox())!
  const apart = (a: typeof card, b: typeof card) => a.x >= b.x + b.width || a.x + a.width <= b.x || a.y >= b.y + b.height || a.y + a.height <= b.y
  // The gap between two boxes along the axis that separates them.
  const gap = (a: typeof card, b: typeof card) => Math.hypot(Math.max(0, b.x - (a.x + a.width), a.x - (b.x + b.width)), Math.max(0, b.y - (a.y + a.height), a.y - (b.y + b.height)))
  expect(apart(opened, card)).toBe(true)
  const chip = (await page.getByTestId('produced-execution').getByRole('button', { name: 'Result' }).boundingBox())!
  expect(gap(opened, chip)).toBeLessThanOrEqual(240)
  const bar = (await produced.boundingBox())!
  const below = (await review.boundingBox())!
  expect(below.y).toBeGreaterThanOrEqual(bar.y + bar.height)
  expect(apart(below, opened)).toBe(true)
  // A run bar file opens where it lives: right below the bar, by its chip, over cards if need be but clear of the other window.
  const reviewChip = (await produced.getByRole('button', { name: 'review.md' }).boundingBox())!
  expect(gap(below, reviewChip)).toBeLessThanOrEqual(240)
  const banner = (await page.getByTestId('run-banner').boundingBox())!
  expect(apart(below, banner)).toBe(true)

  // Drag the review to the right of the canvas and the result to the left, so the two sit side by side.
  // The review, on top, moves right by its title; the result then moves left by the start of its own title.
  const drag = async (win: typeof review, grip: 'start' | 'end', x: number) => {
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
  await expect(review.getByRole('heading', { name: 'Peer review' })).toBeVisible()
  await expect(result).toContainText('All tests pass.')
  for (const zone of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await zone.boundingBox())!
    for (const rect of [left, right]) {
      expect(rect.x < box.x + box.width && rect.x + rect.width > box.x && rect.y < box.y + box.height && rect.y + rect.height > box.y).toBe(false)
    }
  }
  await page.screenshot({ path: test.info().outputPath('produced-file-windows.png') })

  // The rest of what the run produced is one menu away.
  await produced.getByRole('button', { name: '3 more produced files' }).click()
  await page.getByRole('menuitem', { name: 'worker.log' }).click()
  await expect(page.getByRole('dialog', { name: 'file worker.log' })).toContainText('worker finished')
  await expect(page.getByRole('dialog')).toHaveCount(3)
})

test('formation titles stay readable beside the type chip and run controls', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 850 })
  await cockpitFixture(page, { run: true })
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Open terminal' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Peek at Peer review' })).toBeVisible()
  const titles = page.locator('.formation .fhead .tt')
  await expect(titles).toHaveCount(3)
  for (const title of await titles.all()) {
    expect(await title.evaluate(el => el.scrollWidth <= el.clientWidth), await title.innerText()).toBe(true)
  }
  const card = page.getByTestId('formation-node-peer')
  for (const control of [card.locator('.ftype'), card.getByRole('button', { name: 'Peek at Peer review' }), card.getByRole('button', { name: 'Run formation' })]) {
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
  const cwd = banner.locator('.run-cwd')
  await expect(cwd).toHaveAttribute('title', runCwd)
  expect(await cwd.evaluate(el => el.scrollWidth > el.clientWidth && el.clientWidth <= 260)).toBe(true)
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

test('the phone roster count stays inside its header column', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await cockpitFixture(page, { run: true })
  await page.goto('/')
  const count = page.getByTestId('roster-count')
  await expect(count).toHaveText(/roles · \d+ in use/)
  const header = (await page.locator('.roster-hd').boundingBox())!
  const pill = (await count.boundingBox())!
  const firstRole = (await page.locator('.ragent').first().boundingBox())!
  expect(pill.x + pill.width).toBeLessThanOrEqual(header.x + header.width)
  expect(pill.x + pill.width).toBeLessThanOrEqual(firstRole.x)
  expect(await count.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})

for (const width of [1440, 390]) test(`the persona editor stays inside the viewport at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 844 })
  await cockpitFixture(page)
  await page.goto('/')
  await page.getByRole('button', { name: 'Edit Codex builder' }).click()
  const editor = page.getByTestId('persona-editor')
  await expect(editor.getByLabel('Agent display name')).toHaveValue('Codex builder')
  const box = (await editor.boundingBox())!
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual(width)
})

test('a formation without seats never opens a different formation', async ({ page }) => {
  await cockpitFixture(page, { run: true })
  const sockets: string[] = []
  await page.routeWebSocket('**/seats/*/terminal', socket => { sockets.push(socket.url()) })
  await page.goto('/')
  await page.getByRole('button', { name: 'Peek at Peer review', exact: true }).click()
  await expect(page.getByText('No terminal seats have been created for this formation.')).toBeVisible()
  expect(sockets).toEqual([])
})

test('refresh follows the selected worker across attempts and never reconnects in the background', async ({ page }) => {
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
  await page.getByRole('button', { name: 'Open terminal' }).click()
  await expect(page.getByText('Disconnected. Refresh seats to reconnect.')).toBeVisible()
  await page.getByRole('button', { name: 'Worker 1', exact: true }).click()
  await expect(page.getByText('Disconnected. Refresh seats to reconnect.')).toBeVisible()
  const count = sockets.length
  await page.waitForTimeout(200)
  expect(sockets).toHaveLength(count)
  current = current.map(s => s.slotId === 'worker' ? { ...s, createdSeq: 28, terminalUrl: '/api/runs/run_browser/seats/28/terminal' } : s)
  // Refresh keeps the terminal on screen until the new projection arrives; the new attempt then gets its own.
  await page.getByRole('button', { name: 'Refresh seats' }).click()
  await expect.poll(() => sockets.at(-1)).toContain('/seats/28/terminal')
  await expect(page.getByText('Disconnected. Refresh seats to reconnect.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Worker 1', exact: true })).toHaveAttribute('aria-pressed', 'true')
  current = current.map(s => ({ ...s, state: 'ended', terminalUrl: '' }))
  await page.getByRole('button', { name: 'Refresh seats' }).click()
  await expect(page.getByText('Seat ended.')).toBeVisible()
  expect(sockets).toHaveLength(count + 1)
  expect(fixture.writes).toEqual([])
})
