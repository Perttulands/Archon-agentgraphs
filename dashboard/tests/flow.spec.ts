import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Node = { id: string; title: string }

test('Flow shows all 7 Wayfinding steps readable at 1440x900 without horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')
  await expect(page.locator('.formation').first()).toBeVisible()
  await page.getByRole('radio', { name: 'Flow' }).click()
  const flow = page.getByTestId('flow-view')
  await expect(flow).toBeVisible()
  expect(await flow.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)

  const steps = flow.locator('[data-testid^="flow-step-"]')
  await expect(steps).toHaveCount(7)
  const titles = ['Map the territory', 'Framing review', 'Question peers', 'Answer questions', 'Draft the brief', 'Adversarial review', 'Brief sign-off']
  for (const [index, title] of titles.entries()) {
    const step = steps.nth(index)
    await step.scrollIntoViewIfNeeded()
    const heading = step.getByRole('button', { name: `${index + 1} ${title}` })
    await expect(heading).toBeInViewport()
    expect(await heading.evaluate(element => element.scrollWidth <= element.clientWidth), `${title} title fits`).toBe(true)
    expect(await step.evaluate(element => element.scrollWidth <= element.clientWidth), `${title} row fits`).toBe(true)
    const fontSize = await step.locator('.flow-text').first().evaluate(element => parseFloat(getComputedStyle(element).fontSize))
    expect(fontSize, `${title} text size`).toBeGreaterThanOrEqual(11)
  }
  await expect(steps.nth(1)).toContainText('↺ back to 1 Map the territory')
  await expect(steps.nth(5)).toContainText('Decided by a judge')
  await expect(steps.nth(5).getByRole('list', { name: 'Judges of Adversarial review' })).toContainText('Brief critic')
  await expect(steps.nth(6)).toContainText('→ run ends')

  // A row opens its node window beside it, and the Flow view is remembered for the board.
  await steps.nth(4).getByRole('button', { name: '5 Draft the brief' }).click()
  await expect(page.getByRole('dialog', { name: 'Formation · Draft the brief' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.reload()
  await expect(page.getByTestId('flow-view')).toBeVisible()
  const critic = wayfinding.board.formations.find((node: Node) => node.title === 'Brief critic')
  await expect(page.getByTestId(`flow-step-${critic.id}`)).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})
