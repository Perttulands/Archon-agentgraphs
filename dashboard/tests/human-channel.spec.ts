import { expect, test } from '@playwright/test'
import { answerGate, evidenceShot, humanChannelFixture, missionId, peers, talkRunFixture } from './human-channel-fixture'

test('a mission human channel is chosen in its window and Start mission, saved with undo, and shown on the canvas and in Flow', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await humanChannelFixture(page)
  await page.goto('/?board=wayfinding')
  const card = page.getByTestId(`mission-node-${missionId}`)
  await expect(card.locator('.mchannel')).toHaveText('Human gates · Notify me')

  await card.click()
  const win = page.getByRole('dialog', { name: 'Mission · Wayfinding' })
  const channel = win.getByRole('radiogroup', { name: 'Human gates' })
  await expect(channel.getByRole('radio', { name: /Notify me/ })).toBeChecked()
  await channel.getByText('Talk with the agents').click()
  await expect.poll(() => fixture.patches.at(-1)?.updateMission).toEqual({ id: missionId, humanChannel: 'session' })
  await expect(card.locator('.mchannel')).toHaveText('Human gates · Talk with the agents')
  await expect(channel.getByRole('radio', { name: /Talk with the agents/ })).toBeChecked()
  await expect(win).toContainText('A change applies to runs started afterwards')
  await channel.scrollIntoViewIfNeeded()
  await evidenceShot(page, 'human-channel-mission-window')

  // Undo works with the chosen radio still focused.
  await page.keyboard.press('Control+z')
  await expect.poll(() => fixture.patches.at(-1)?.updateMission).toEqual({ id: missionId, humanChannel: '' })
  await expect(card.locator('.mchannel')).toHaveText('Human gates · Notify me')
  await expect(channel.getByRole('radio', { name: /Notify me/ })).toBeChecked()
  await page.keyboard.press('Escape')
  await expect(win).toHaveCount(0)

  // Start mission shows the channel and saves a change only when the run starts.
  await page.getByTestId(`run-mission-${missionId}`).click()
  const start = page.getByRole('dialog', { name: 'Start mission' })
  const startChannel = start.getByRole('radiogroup', { name: 'Human gates' })
  await expect(startChannel.getByRole('radio', { name: /Notify me/ })).toBeChecked()
  await startChannel.getByText('Talk with the agents').click()
  await expect(start).toContainText('Saved on the mission when you start. A change applies to runs started afterwards; runs already going keep their channel.')
  await evidenceShot(page, 'human-channel-start-mission')
  await start.getByRole('button', { name: 'Cancel' }).click()
  expect(fixture.patches).toHaveLength(2)

  // Flow names the channel under the mission's goal.
  await card.click()
  await page.getByRole('dialog', { name: 'Mission · Wayfinding' }).getByRole('radiogroup', { name: 'Human gates' }).getByText('Talk with the agents').click()
  await expect.poll(() => fixture.patches.length).toBe(3)
  await page.keyboard.press('Escape')
  await page.getByRole('radio', { name: 'Flow' }).click()
  await expect(page.getByTestId('flow-view').locator('.flow-channel')).toHaveText('Human gatesTalk with the agents')
  expect(fixture.writes).toEqual([])
})

test('Talk with the asked formation opens each peer seat beside the answer panel, where typing and resizing reach that seat', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const fixture = await talkRunFixture(page)
  await page.goto('/?board=wayfinding')
  const panel = page.getByRole('dialog', { name: 'Answer gate Answer questions' })
  await expect(panel).toContainText('The 2 agents that did the work are waiting in their terminals.')
  await panel.getByRole('button', { name: 'Talk with Question peers' }).click()

  const planner = page.getByRole('dialog', { name: 'Talk with Delivery Planner · Claude Code' })
  const codex = page.getByRole('dialog', { name: 'Talk with Codex Planner · Codex' })
  await expect(planner).toContainText('Question peers / Peer')
  await expect(planner).toContainText('On call · waiting for you')
  await expect(planner.locator('.xterm-rows')).toContainText('seat 21: which questions should we settle first?')
  await expect(codex.locator('.xterm-rows')).toContainText('seat 22: which questions should we settle first?')
  await expect(planner).toContainText('Live · type to talk to the agent')

  // Each window opens beside the panel or the window before it, covering neither.
  const [panelBox, plannerBox, codexBox] = await Promise.all([panel.boundingBox(), planner.boundingBox(), codex.boundingBox()])
  const overlaps = (a: typeof panelBox, b: typeof panelBox) => a!.x < b!.x + b!.width && b!.x < a!.x + a!.width && a!.y < b!.y + b!.height && b!.y < a!.y + a!.height
  expect(overlaps(plannerBox, panelBox)).toBe(false)
  expect(overlaps(codexBox, panelBox)).toBe(false)
  expect(overlaps(codexBox, plannerBox)).toBe(false)
  expect(plannerBox!.x + plannerBox!.width).toBeLessThanOrEqual(panelBox!.x)

  // The first seat takes the keyboard as it opens; Escape goes to the agent, not the window.
  await expect.poll(() => fixture.resizes(21).length).toBeGreaterThan(0)
  await page.keyboard.type('Settle 1 and 3 first')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Escape')
  await expect.poll(() => fixture.typed(21)).toBe('Settle 1 and 3 first\r\x1b')
  await expect(planner).toBeVisible()
  await evidenceShot(page, 'talk-with-question-peers')

  // The other peer is typed into after a click, and its window's size reaches only that seat.
  await codex.locator('.terminal-surface-host').click()
  await page.keyboard.type('agreed')
  await expect.poll(() => fixture.typed(22)).toBe('agreed')
  expect(fixture.typed(21)).toBe('Settle 1 and 3 first\r\x1b')
  const before = fixture.resizes(22).at(-1)!
  const plannerResizes = fixture.resizes(21).length
  const corner = (await codex.locator('.floating-frame-handle[data-handle="se"]').boundingBox())!
  await page.mouse.move(corner.x + 6, corner.y + 6)
  await page.mouse.down()
  await page.mouse.move(corner.x + 6, corner.y + 186, { steps: 8 })
  await page.mouse.up()
  await expect.poll(() => fixture.resizes(22).at(-1)!.rows).toBeGreaterThan(before.rows)
  expect(fixture.resizes(21)).toHaveLength(plannerResizes)

  // Talk again raises the open windows instead of opening more; the Flow row offers the same.
  await panel.getByRole('button', { name: 'Talk with Question peers' }).click()
  await expect(page.locator('[data-window-id^="talk:"]')).toHaveCount(2)
  await page.getByRole('radio', { name: 'Flow' }).click()
  const row = page.getByTestId(`flow-step-${answerGate.id}`)
  await expect(row.getByRole('button', { name: 'Talk with Question peers' })).toBeVisible()
  await expect(page.locator('[data-window-id^="talk:"]')).toHaveCount(2)

  // Peek marks the kept seats as on call and waiting.
  await page.getByRole('button', { name: 'Open terminal' }).click()
  const peek = page.getByRole('dialog', { name: 'Formation terminal Peek' })
  await expect(peek.getByRole('navigation', { name: 'Run seats' }).getByText('on call')).toHaveCount(peers.slots!.length)
  await expect(peek.locator('.peek-on-call')).toHaveText('On call · waiting for you')
  await peek.getByRole('button', { name: 'Close terminal Peek' }).click()

  // Once a seat records the decision, the open windows say so and stay, still taking typing.
  await expect(planner.getByRole('status').filter({ hasText: 'Decision recorded' })).toHaveCount(0)
  fixture.decide()
  await expect(planner.getByText('Decision recorded; this agent closes when idle.')).toBeVisible()
  await expect(codex.getByText('Decision recorded; this agent closes when idle.')).toBeVisible()
  await expect(planner.getByText('On call · waiting for you')).toHaveCount(0)
  await expect(planner.locator('.peek-foot')).toContainText('Type to talk · select to copy')
  await expect(page.getByRole('button', { name: 'Talk with Question peers' })).toHaveCount(0)
  await page.getByRole('radio', { name: 'Canvas' }).click()
  await planner.locator('.terminal-surface-host').click()
  await page.keyboard.type('thanks')
  await expect.poll(() => fixture.typed(21)).toContain('thanks')
  await evidenceShot(page, 'talk-decision-recorded')
  await fixture.endSeat(22)
  await expect(codex.getByText('Decision recorded; this agent has closed.')).toBeVisible()
  await expect(planner.getByText('Decision recorded; this agent closes when idle.')).toBeVisible()
  expect(fixture.writes).toEqual([])
})

test('a gate whose ask fell back says why, and a relayed decision names the seat that recorded it', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await talkRunFixture(page, { fallbackReason: 'every seat that received the ask is gone' })
  await page.goto('/?board=wayfinding')
  const panel = page.getByRole('dialog', { name: 'Answer gate Answer questions' })
  await expect(panel.getByRole('note')).toHaveText('The agents are not available for this gate: every seat that received the ask is gone. Answer here.')
  await expect(panel.getByRole('button', { name: /Talk with/ })).toHaveCount(0)
  await expect(panel.getByRole('button', { name: 'Approve' })).toBeEnabled()
  await evidenceShot(page, 'talk-fallback-reason')

  await page.getByRole('button', { name: 'Inspect gate evidence for Framing review' }).click()
  await expect(page.getByTestId('gate-evaluation-4')).toContainText('pass · human:operator · via codex-scout')
  await evidenceShot(page, 'relayed-decision-via')
})

test('a narrow Talk window exposes the end of a native-width line without resizing another viewer', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const marker = 'END_OF_LINE'
  const fixture = await talkRunFixture(page, { columns: 160, terminalText: `\x1b[?1049h${'Question '.repeat(16)}${marker}\r\n> ` })
  await page.goto('/?board=wayfinding')
  await page.getByRole('button', { name: 'Talk with Question peers' }).click()
  const win = page.getByRole('dialog', { name: 'Talk with Delivery Planner · Claude Code' })
  await expect(win).toContainText('Live · type to talk to the agent')
  await expect.poll(() => fixture.resizes(21).at(-1)?.columns).toBe(160)
  const host = win.getByTestId('terminal-surface')
  await expect.poll(() => host.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
  await win.getByRole('button', { name: 'End of line' }).click()
  // Check the actual glyph range is inside the scroll viewport, not merely in the DOM.
  await expect.poll(() => host.evaluate((el, text) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
    let node: Node | null
    while ((node = walker.nextNode())) {
      const offset = node.textContent?.indexOf(text) ?? -1
      if (offset < 0) continue
      const range = document.createRange()
      range.setStart(node, offset)
      range.setEnd(node, offset + text.length)
      const glyphs = range.getBoundingClientRect(), viewport = el.getBoundingClientRect()
      return glyphs.left >= viewport.left && glyphs.right <= viewport.right
    }
    return false
  }, marker)).toBe(true)
  const otherSizes = fixture.resizes(22).length
  await win.getByRole('button', { name: 'Start of line' }).click()
  await expect.poll(() => host.evaluate(el => el.scrollLeft)).toBe(0)
  await host.click()
  await page.keyboard.type('read the complete question')
  await expect.poll(() => fixture.typed(21)).toBe('read the complete question')
  expect(fixture.resizes(22)).toHaveLength(otherSizes)
  await evidenceShot(page, 'talk-native-width-line')
})
