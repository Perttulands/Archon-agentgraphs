import { expect, test } from '@playwright/test'
import { evidenceShot, humanChannelFixture, missionId } from './human-channel-fixture'

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
