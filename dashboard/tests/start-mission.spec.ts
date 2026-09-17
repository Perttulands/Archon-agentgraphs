import { expect, test } from '@playwright/test'
import { wayfindingFixture } from './wayfinding-fixture'

test('pending launch keeps focus on enabled dialog controls', async ({ page }) => {
  const fixture = await wayfindingFixture(page)
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  let submissions = 0
  await page.route('**/api/formations/runs', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    submissions++
    await pending
    await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only pending launch ended' } } })
  })
  await page.goto('/?board=wayfinding')
  await page.getByTitle('Start mission', { exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  await dialog.getByLabel('Working directory').fill('/fixture')
  await dialog.getByLabel('Brief', { exact: true }).fill('A fixture-only sketch')
  await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
  try {
    await expect(dialog.getByRole('button', { name: 'Starting…', exact: true })).toBeDisabled()
    await expect(dialog.getByLabel('Working directory')).toBeFocused()
    for (const key of ['Tab', 'Shift+Tab']) {
      for (let step = 0; step < 16; step++) {
        await page.keyboard.press(key)
        await expect(dialog.locator(':focus')).toHaveCount(1)
        await expect(dialog.locator(':focus')).toBeEnabled()
      }
    }
    expect(submissions).toBe(1)
    expect(fixture.writes).toEqual([])
  } finally {
    release()
  }
  await expect(dialog.getByRole('alert')).toContainText('Fixture-only pending launch ended')
})

test('launch keeps keyboard focus inside and restores its opener after dismissal', async ({ page }) => {
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  const opener = page.getByTitle('Start mission', { exact: true }).first()
  await opener.click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  await expect(dialog.getByLabel('Working directory')).toBeFocused()
  for (const key of ['Tab', 'Shift+Tab']) {
    for (let step = 0; step < 24; step++) {
      await page.keyboard.press(key)
      await expect(dialog.locator(':focus')).toHaveCount(1)
    }
  }
  await dialog.getByRole('button', { name: 'Start mission', exact: true }).focus()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Close start mission' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(dialog.getByRole('button', { name: 'Start mission', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(opener).toBeFocused()
  await page.keyboard.press('Enter')
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(opener).toBeFocused()
  expect(fixture.writes).toEqual([])
})

test('launch limits allow replacement and explain their units without changing defaults', async ({ page }) => {
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  await page.getByTitle('Start mission', { exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  const dispatches = dialog.getByLabel('Maximum dispatches', { exact: true })
  const attempts = dialog.getByLabel('Maximum attempts', { exact: true })
  const time = dialog.getByLabel('Time limit in seconds', { exact: true })
  await expect(dispatches).toHaveValue('20')
  await expect(attempts).toHaveValue('3')
  await expect(time).toHaveValue('1800')
  await expect(dispatches).toHaveAccessibleDescription('Formation executions across this run, including judge steps.')
  await expect(attempts).toHaveAccessibleDescription('Maximum visits to each node.')
  await expect(time).toHaveAccessibleDescription(/30 minutes.*waits for you at a gate doesn’t count/)
  await dialog.getByLabel('Working directory').fill('/fixture')
  await dialog.getByLabel('Brief', { exact: true }).fill('A fixture-only sketch')
  for (const input of [dispatches, attempts, time]) {
    await input.fill('')
    await expect(input).toHaveValue('')
    await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
    await expect(input).toBeFocused()
    await input.fill('12')
    await expect(input).toHaveValue('12')
  }
  await expect(time).toHaveAccessibleDescription(/12 seconds/)
  expect(fixture.writes).toEqual([])
})
