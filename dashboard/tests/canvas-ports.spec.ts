import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'

// The fixture rejects any addPort direction the store rejects, so these journeys
// fail if the cockpit sends anything but the server's input/output vocabulary.
test('every add-port menu adds a port, and each add is one undo entry', async ({ page }) => {
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page)
  await page.goto('/?board=browser')
  const peer = page.getByTestId('formation-node-peer')
  await expect(peer).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  await peer.locator('.fhead').click({ button: 'right' })
  await menuItem('Add input port').click()
  await expect(peer.locator('.fio.in')).toHaveCount(2)

  await peer.locator('.fhead').click({ button: 'right' })
  await menuItem('Add output port').click()
  await expect(peer.locator('.fio.out')).toHaveCount(2)

  await peer.locator('.fio.in').first().click({ button: 'right' })
  await menuItem('Add input port').click()
  await expect(peer.locator('.fio.in')).toHaveCount(3)

  await peer.locator('.fio.out').first().click({ button: 'right' })
  await menuItem('Add output port').click()
  await expect(peer.locator('.fio.out')).toHaveCount(3)
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
  expect(fixture.board().rev).toBe(5)

  // Undo removes the ports newest first, one per Ctrl+Z.
  for (const [inputs, outputs] of [[3, 2], [2, 2], [2, 1], [1, 1]]) {
    await page.keyboard.press('Control+z')
    await expect(peer.locator('.fio.in')).toHaveCount(inputs)
    await expect(peer.locator('.fio.out')).toHaveCount(outputs)
  }
  await expect(page.getByTestId('formations-error')).toHaveCount(0)
  expect(fixture.board().rev).toBe(9)
})
