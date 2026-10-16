import { expect, test } from '@playwright/test'
import { cockpitFixture, pickedExecution, runArtifacts } from './cockpit-fixture'

// Peer review's result becomes a file, the mission declares it as its report,
// and the succeeded run delivers the snapshot's path, which opens in a file window.
test('a port kind and a mission output bound to it lead to a run output that opens', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { succeeded: true })
  await page.goto('/?mission=browser')
  const peer = page.getByTestId('formation-node-peer')
  await expect(peer).toBeVisible()
  const menuItem = (name: string) => page.locator('.ctxmenu').getByRole('menuitem', { name })

  await peer.locator('.fio.out').first().click({ button: 'right' })
  await expect(page.locator('.ctxmenu')).toContainText('Carries text')
  await menuItem('Carry a file instead').click()
  await expect(page.getByTestId('port-kind-peer:out')).toHaveText('file')
  expect(fixture.board().formations.find(formation => formation.id === 'peer')!.outputs[0]).toEqual({ id: 'out', label: 'Result', kind: 'file' })

  const card = page.getByTestId('mission-node-mission')
  await card.click()
  const win = page.getByRole('dialog', { name: 'Input card · Delivery' })
  await win.getByRole('button', { name: 'Edit outputs' }).click()
  await win.getByRole('button', { name: 'Add output' }).click()
  await win.getByLabel('Name of output 1').fill('report')
  await win.getByLabel('Port of output 1').selectOption({ label: 'Peer review · Result' })
  await expect(win.getByLabel('Kind of output 1')).toHaveValue('file')
  await win.getByRole('button', { name: 'Save outputs' }).click()
  await expect(win.getByRole('list', { name: 'Outputs' })).toHaveText('report file, required · from Peer review · Result')
  await expect(page.getByTestId('mission-outputs-mission')).toHaveText('Outputs · report')
  expect(fixture.board().inputCards[0].outputs).toEqual([{ name: 'report', kind: 'file', required: true, from: 'peer:out', description: '' }])
  await expect(page.getByTestId('formations-error')).toHaveCount(0)

  await page.goto('/?mission=browser&run=run_browser')
  const output = page.getByTestId('run-outputs').getByTestId('run-output-report')
  await expect(output).toHaveText('▤reportreview.md')
  await expect(output).toHaveAttribute('title', `report · file · ${runArtifacts}/peer/1/review.md`)
  await output.click()
  const file = page.getByRole('dialog', { name: 'file review.md' })
  await expect(file.getByRole('heading', { name: 'Peer review' })).toBeVisible()
})

// A run whose work went on through a pick is failed work, not a pass
// (ADR-0022): the run bar says so, and each output names the failed verdict.
test('a picked run reads as picked, and its outputs carry the verdict that failed them', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { succeeded: true, picked: true })
  fixture.board().inputCards[0].outputs = [{ name: 'summary', kind: 'text', from: 'execution:out' }]
  await page.goto('/?mission=browser&run=run_browser')
  await expect(page.getByTestId('run-banner').locator('.badge')).toHaveText('Picked, not passed')
  const output = page.getByTestId('run-output-summary')
  await expect(output).toHaveClass(/picked/)
  await expect(output).toContainText('picked')
  await expect(output).toHaveAttribute('title', `summary · text · Implemented the reviewed plan.\n\nAll tests pass.\npicked: Execution attempt 1, which Review gate judged fail: ${pickedExecution.verdict.reason}\n  - ${pickedExecution.verdict.findings[0]}`)
})

test('a folder output opens as its listing, whose files open in their own windows', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await cockpitFixture(page, { succeeded: true })
  const card = fixture.board().inputCards[0]
  card.outputs = [{ name: 'site', kind: 'folder', from: 'execution:out' }]
  await page.goto('/?mission=browser&run=run_browser')
  await page.getByTestId('run-output-site').click()
  const listing = page.getByRole('dialog', { name: 'file site/' })
  await expect(listing).toContainText('assets/')
  await listing.getByRole('link', { name: 'index.html' }).click()
  await expect(page.getByRole('dialog', { name: 'file index.html' })).toContainText('<h1>Site</h1>')
})
