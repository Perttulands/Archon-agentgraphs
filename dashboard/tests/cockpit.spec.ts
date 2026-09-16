import { expect, test } from '@playwright/test'
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

for (const width of [1440, 390]) test(`floating Peek observes native output and keeps graph stable at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
  const fixture = await cockpitFixture(page, { run: true })
  const frames: string[] = []
  await page.routeWebSocket('**/seats/*/terminal', socket => {
    socket.onMessage(data => {
      const value = Buffer.isBuffer(data) ? data.toString() : data
      frames.push(value)
      if (value.startsWith('{')) socket.send(Buffer.from('0\x1b[32mSCRATCH OUTPUT\x1b[0m\r\n' + Array.from({ length: 45 }, (_, i) => `line ${i}`).join('\r\n')))
    })
  })
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Open terminal' })).toBeEnabled()
  const world = page.locator('.world')
  const transform = await world.getAttribute('style')
  await page.getByRole('button', { name: 'Open terminal' }).click()
  await expect(page.getByText('Live output', { exact: true })).toBeVisible()
  await expect(page.locator('.xterm-screen')).toBeVisible()
  const host = page.locator('.terminal-surface-host')
  await expect.poll(() => host.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThan(2)
  await page.getByRole('button', { name: 'Older output', exact: true }).click()
  expect(await host.evaluate(el => el.scrollTop)).toBe(0)
  const grid = (await page.locator('.xterm-screen').boundingBox())!
  await page.mouse.move(grid.x + 4, grid.y + 20)
  await page.mouse.down()
  await page.mouse.move(grid.x + 120, grid.y + 20, { steps: 8 })
  await page.mouse.up()
  await expect(page.locator('.xterm-selection div').first()).toBeVisible()
  await page.getByRole('button', { name: 'Latest output', exact: true }).click()
  await expect.poll(() => host.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThan(2)
  expect(await world.getAttribute('style')).toBe(transform)
  await page.locator('.terminal-surface-host').click()
  await page.keyboard.type('MUST NOT REACH THE SEAT')
  await page.keyboard.press('Enter')
  const before = fixture.seatsFetches()
  await page.getByRole('button', { name: 'Worker 1', exact: true }).click()
  await expect(page.getByText('Live output', { exact: true })).toBeVisible()
  expect(fixture.seatsFetches()).toBeGreaterThan(before)
  const head = page.locator('.peek-head')
  const rect = (await head.boundingBox())!
  await page.mouse.move(rect.x + rect.width / 2, rect.y + 15)
  await page.mouse.down(); await page.mouse.move(width + 100, 950); await page.mouse.up()
  const close = page.getByRole('button', { name: 'Close terminal Peek' })
  const closeRect = (await close.boundingBox())!
  expect(closeRect.x + closeRect.width).toBeLessThanOrEqual(width)
  await close.click()
  expect(await world.getAttribute('style')).toBe(transform)
  await page.getByRole('button', { name: 'Open terminal' }).click()
  await expect(page.getByText('Live output', { exact: true })).toBeVisible()
  expect(await world.getAttribute('style')).toBe(transform)
  expect(fixture.writes).toEqual([])
  expect(frames.length).toBeGreaterThan(1)
  for (const frame of frames) expect(JSON.parse(frame)).toEqual({ columns: 96, rows: 30 })
  await page.screenshot({ path: `/tmp/form-ui-peek-${width}.png` })
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
  const z = (win: typeof run) => win.evaluate(el => Number(getComputedStyle(el).zIndex))
  expect(await z(run)).toBeGreaterThan(await z(peer))
  const peerBox = (await peer.boundingBox())!
  const before = (await run.boundingBox())!
  expect(before.x).toBeGreaterThan(peerBox.x)
  expect(before.y).toBeGreaterThan(peerBox.y)

  const corner = (await run.locator('[data-handle="se"]').boundingBox())!
  await page.mouse.move(corner.x + corner.width / 2, corner.y + corner.height / 2)
  await page.mouse.down()
  await page.mouse.move(1600, 1100, { steps: 8 })
  await page.mouse.up()
  const after = (await run.boundingBox())!
  expect(after.x).toBeCloseTo(before.x, 0)
  expect(after.y).toBeCloseTo(before.y, 0)
  expect(after.width * after.height).toBeGreaterThan(before.width * before.height)

  // The run window cascaded below and right of the peer window, so the peer's top-left header corner stays uncovered.
  const head = (await peer.locator('.peek-head').boundingBox())!
  await page.mouse.move(head.x + 8, head.y + 8)
  await page.mouse.down()
  await page.mouse.move(1600, 1100, { steps: 8 })
  await page.mouse.up()
  expect(await z(peer)).toBeGreaterThan(await z(run))

  const canvas = (await page.getByTestId('formations-canvas').boundingBox())!
  for (const zone of [page.locator('.zoomctl'), page.locator('.zoomlevel')]) {
    const box = (await zone.boundingBox())!
    for (const win of [run, peer]) {
      const rect = (await win.boundingBox())!
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

test('a finished run opens what it produced from the run bar and cards in file windows side by side', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await cockpitFixture(page, { succeeded: true })
  await page.goto('/?board=browser&run=run_browser')

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
  await expect(review.getByRole('link', { name: 'Open raw' })).toHaveAttribute('href', '/api/formations/runs/run_browser/artifacts/review.md')

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
  await expect(page.getByTestId('run-banner').locator('.badge')).toHaveText('blocked')

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
  await expect(count).toHaveText(/on board/)
  const header = (await page.locator('.roster-hd').boundingBox())!
  const pill = (await count.boundingBox())!
  const label = (await page.locator('.roster-group-label').first().boundingBox())!
  expect(pill.x + pill.width).toBeLessThanOrEqual(header.x + header.width)
  expect(pill.x + pill.width).toBeLessThanOrEqual(label.x)
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
  current = current.map(s => s.slotId === 'worker' ? { ...s, createdSeq: 28, terminalUrl: '/api/formations/runs/run_browser/seats/28/terminal' } : s)
  await page.getByRole('button', { name: 'Refresh seats' }).click()
  await expect(page.getByText('Disconnected. Refresh seats to reconnect.')).toBeVisible()
  expect(sockets.at(-1)).toContain('/seats/28/terminal')
  await expect(page.getByRole('button', { name: 'Worker 1', exact: true })).toHaveAttribute('aria-pressed', 'true')
  current = current.map(s => ({ ...s, state: 'ended', terminalUrl: '' }))
  await page.getByRole('button', { name: 'Refresh seats' }).click()
  await expect(page.getByText('Seat ended.')).toBeVisible()
  expect(sockets).toHaveLength(count + 1)
  expect(fixture.writes).toEqual([])
})
