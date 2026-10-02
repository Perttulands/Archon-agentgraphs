import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

test('Ctrl+Z restores a deleted gate with its wires, then the older edit, and never repeats a failure', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const peer = page.getByTestId('formation-node-peer')
  await expect(peer).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  // An older edit: a port added from the card menu.
  await peer.locator('.fhead').click({ button: 'right' })
  await menuItem('Add output port').click()
  await expect(peer.locator('.fio.out')).toHaveCount(2)

  const gate = page.getByTestId('gate-node-gate')
  await gate.click({ button: 'right' })
  await menuItem('Delete gate').click()
  await expect(gate).toHaveCount(0)
  expect(fixture.board().connections.map(edge => edge.id)).toEqual(['start'])

  await page.keyboard.press('Control+z')
  await expect(page.getByTestId('gate-node-gate')).toBeVisible()
  expect(fixture.board().connections.map(edge => edge.id).sort()).toEqual(['judge-return', 'judge-send', 'pass', 'review', 'start'])
  expect(fixture.board().gates.find(item => item.id === 'gate')).toEqual({ id: 'gate', title: 'Review gate', kinds: ['formation'], criterion: 'Evidence supports acceptance' })
  await expect(peer.locator('.fio.out')).toHaveCount(2)
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('gate-restored.png') })

  await page.keyboard.press('Control+z')
  await expect(peer.locator('.fio.out')).toHaveCount(1)
  await page.keyboard.press('Control+z')
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
})

test('a failed undo is reported once and Ctrl+Z moves on to older history', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?mission=browser')
  const judge = page.getByTestId('formation-node-judge')
  await expect(judge).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  await judge.locator('.fhead').click({ button: 'right' })
  await menuItem('Add input port').click()
  await expect(judge.locator('.fio.in')).toHaveCount(2)
  await page.getByTestId('mission-node-mission').click({ button: 'right' })
  await menuItem('Delete Input card').click()
  await expect(page.getByTestId('mission-node-mission')).toHaveCount(0)

  // Someone else puts a node with the same ID back first, so this undo can no longer apply.
  fixture.board().inputCards.push({ id: 'mission', title: 'Delivery', goal: '' })
  await page.keyboard.press('Control+z')
  await expect(page.getByTestId('formations-error')).toHaveText(
    'Could not undo the delete of Input card “Delivery”: node "mission" is already in the mission. It was removed from the undo history.')
  await page.screenshot({ path: test.info().outputPath('undo-failed-once.png') })

  await page.keyboard.press('Control+z')
  await expect(judge.locator('.fio.in')).toHaveCount(1)
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
})

test('Ctrl+Z during an edit in flight undoes that edit, and a stale revision is reloaded and retried', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  let releaseAdd: () => void = () => undefined
  const addHeld = new Promise<void>(resolve => { releaseAdd = resolve })
  let staleOnce = true
  // Registered after the fixture, so it sees board writes first.
  await page.route('**/api/missions/browser', async route => {
    const body = route.request().method() === 'PATCH' ? route.request().postDataJSON() : null
    if (body?.addPort) await addHeld
    if (body?.restoreNode && staleOnce) {
      staleOnce = false
      return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'The mission changed since it was read; reload it and retry' } } })
    }
    return route.fallback()
  })
  await page.goto('/?mission=browser')
  const judge = page.getByTestId('formation-node-judge')
  await expect(judge).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  await page.getByTestId('mission-node-mission').click({ button: 'right' })
  await menuItem('Delete Input card').click()
  await expect(page.getByTestId('mission-node-mission')).toHaveCount(0)
  await judge.locator('.fhead').click({ button: 'right' })
  await menuItem('Add output port').click()
  // The add is still being saved: Ctrl+Z must wait for it, not undo the delete.
  await page.keyboard.press('Control+z')
  await page.waitForTimeout(300)
  await expect(page.getByTestId('mission-node-mission')).toHaveCount(0)
  releaseAdd()
  // delete (rev 2), the add (rev 3), then its undo (rev 4)
  await expect.poll(() => fixture.board().rev).toBe(4)
  await expect(judge.locator('.fio.out')).toHaveCount(1)
  await expect(page.getByTestId('mission-node-mission')).toHaveCount(0)
  expect(fixture.board().formations.find(item => item.id === 'judge')!.outputs).toHaveLength(1)

  // The next undo meets a stale revision once; it reloads, retries and restores the mission.
  await page.keyboard.press('Control+z')
  await expect(page.getByTestId('mission-node-mission')).toBeVisible()
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
})
