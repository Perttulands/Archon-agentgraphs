import { expect, test } from '@playwright/test'
import { cockpitFixture, seats } from './cockpit-fixture'

test('theme fallback, local font, notes and harness icons survive', async ({ page }) => {
  const fixture = await cockpitFixture(page, { themeFailure: true })
  await page.goto('/')
  await expect(page.getByRole('status').filter({ hasText: 'Host theme unavailable' })).toBeVisible()
  await expect(page.locator('.formation')).toHaveCount(3)
  expect(await page.locator('.formation').first().evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(26, 26, 26)')
  expect(await page.locator('.world').evaluate(el => getComputedStyle(el).backgroundImage)).not.toBe('none')
  expect(await page.evaluate(() => document.fonts.check('13px "JetBrains Mono"'))).toBe(true)
  await expect(page.locator('.slot svg')).toHaveCount(4)
  await expect(page.locator('.note-preview')).toHaveText('Controller directs the assigned worker.')
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
