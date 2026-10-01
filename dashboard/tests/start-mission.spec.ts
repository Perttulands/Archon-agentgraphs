import { expect, test } from '@playwright/test'
import { scoutingFixture } from './scouting-fixture'

test('pending launch keeps focus on enabled dialog controls', async ({ page }) => {
  const fixture = await scoutingFixture(page)
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  let submissions = 0
  await page.route('**/api/runs', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    submissions++
    await pending
    await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only pending launch ended' } } })
  })
  await page.goto('/?mission=scouting')
  await page.getByTitle('Start mission', { exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  await dialog.getByLabel('Workspace', { exact: true }).selectOption('existing')
  await dialog.getByLabel('Working directory').fill('/fixture')
  await dialog.getByLabel('Brief', { exact: true }).fill('A fixture-only sketch')
  await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
  try {
    await expect(dialog.getByRole('button', { name: 'Starting…', exact: true })).toBeDisabled()
    await expect(dialog.getByLabel('Brief', { exact: true })).toBeFocused()
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
  const fixture = await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  const opener = page.getByTitle('Start mission', { exact: true }).first()
  await opener.click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  await expect(dialog.getByLabel('Brief', { exact: true })).toBeFocused()
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

test('the dialog asks for no launch limits', async ({ page }) => {
  const fixture = await scoutingFixture(page)
  await page.goto('/?mission=scouting')
  await page.getByTitle('Start mission', { exact: true }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
  await expect(dialog.getByLabel('Brief', { exact: true })).toBeVisible()
  // Runs have no limits unless the mission sets them (form-o7p.7).
  await expect(dialog.getByRole('spinbutton')).toHaveCount(0)
  await expect(dialog.getByText(/dispatch|attempt|time limit/i)).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})

for (const switchFromExisting of [false, true]) {
  test(`automatic workspace submits no path${switchFromExisting ? ' after changing modes' : ' by default'}`, async ({ page }) => {
    const fixture = await scoutingFixture(page)
    const submissions: Record<string, unknown>[] = []
    await page.route('**/api/runs', async route => {
      if (route.request().method() !== 'POST') return route.fallback()
      submissions.push(route.request().postDataJSON())
      await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only payload captured' } } })
    })
    await page.goto('/?mission=scouting')
    await page.getByTitle('Start mission', { exact: true }).first().click()
    const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
    const workspace = dialog.getByLabel('Workspace', { exact: true })
    await expect(workspace).toHaveValue('automatic')
    await expect(dialog.getByLabel('Working directory')).toHaveCount(0)
    await dialog.getByLabel('Brief', { exact: true }).fill('A fixture-only sketch')
    if (switchFromExisting) {
      await workspace.selectOption('existing')
      const cwd = dialog.getByLabel('Working directory')
      for (const invalidPath of ['', 'relative/project']) {
        await cwd.fill(invalidPath)
        await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
        await expect(cwd).toBeFocused()
        expect(submissions).toEqual([])
      }
      await cwd.fill('/fixture/project')
      await workspace.selectOption('automatic')
      await expect(cwd).toHaveCount(0)
    }
    await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('Fixture-only payload captured')
    expect(submissions).toHaveLength(1)
    expect(submissions[0]).toMatchObject({ cwd: '', contextPaths: [], brief: 'A fixture-only sketch' })
    expect(submissions[0]).not.toHaveProperty('limits')
    expect(fixture.writes).toEqual([])
  })
}

for (const mode of ['automatic', 'existing']) {
  test(`context paths survive workspace switches and submit in ${mode} mode`, async ({ page }) => {
    const fixture = await scoutingFixture(page)
    const payloads: Record<string, unknown>[] = []
    await page.route('**/api/runs', async route => {
      if (route.request().method() !== 'POST') return route.fallback()
      payloads.push(route.request().postDataJSON())
      await route.fulfill({ status: 409, json: { success: false, error: { message: 'Fixture-only payload captured' } } })
    })
    await page.goto('/?mission=scouting')
    await page.getByTitle('Start mission', { exact: true }).first().click()
    const dialog = page.getByRole('dialog', { name: 'Start mission', exact: true })
    await dialog.getByLabel('Brief', { exact: true }).fill('Inspect the supplied context')
    const context = dialog.getByLabel('Context paths', { exact: true })
    const paths = '/work/prior art\n\n/work/notes.md\n'
    await context.fill(paths)
    const workspace = dialog.getByLabel('Workspace', { exact: true })
    await workspace.selectOption('existing')
    await dialog.getByLabel('Working directory').fill('/work/project')
    await workspace.selectOption('automatic')
    await workspace.selectOption(mode)
    await expect(context).toHaveValue(paths)
    await dialog.getByRole('button', { name: 'Start mission', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('Fixture-only payload captured')
    expect(payloads).toHaveLength(1)
    expect(payloads[0]).toMatchObject({ cwd: mode === 'existing' ? '/work/project' : '', contextPaths: ['/work/prior art', '/work/notes.md'] })
    expect(fixture.writes).toEqual([])
  })
}
