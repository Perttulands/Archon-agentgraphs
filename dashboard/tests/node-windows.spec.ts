import { expect, test } from '@playwright/test'
import { authoredBoard as wayfindingBoard, authoredText, nodeWindowsFixture } from './node-windows-fixture'

test('every Wayfinding node reads in full in its window, with no edit dialog and no board write', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  const fixture = await nodeWindowsFixture(page)
  await page.goto('/?board=wayfinding')
  await expect(page.getByTestId(`mission-node-${wayfindingBoard.missions[0].id}`)).toBeVisible()
  await page.getByRole('button', { name: 'FIT' }).click()

  const nodes = [
    ...wayfindingBoard.missions.map(node => ({ label: `Mission · ${node.title}`, card: page.getByTestId(`mission-node-${node.id}`), text: node.goal, field: 'mission goal', title: node.title })),
    ...wayfindingBoard.formations.map(node => ({ label: `Formation · ${node.title}`, card: page.getByTestId(`formation-node-${node.id}`).locator('.fhead .tt'), text: node.brief.goal, field: 'brief', title: node.title })),
    ...wayfindingBoard.gates.map(node => ({ label: `Gate · ${node.title}`, card: page.getByTestId(`gate-node-${node.id}`).locator('.gt'), text: node.criterion, field: 'criterion', title: node.title })),
  ]
  expect(nodes).toHaveLength(9)
  for (const node of nodes) {
    expect(node.text).toBe(authoredText(node.title, node.field))
    await node.card.click()
    const window = page.getByRole('dialog', { name: node.label })
    await expect(window).toBeVisible()
    const markdown = window.locator('.nfield-markdown').first()
    for (const line of [`${node.title}: read the operator's sketch`, 'Point 1 of the', 'Point 6 of the', `The ${node.field} for ${node.title} ends here.`]) {
      const words = markdown.getByText(line)
      await words.scrollIntoViewIfNeeded()
      await expect(words).toBeInViewport()
    }
    await expect(markdown.locator('li')).toHaveCount(6)
    expect(await markdown.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    await expect(page.locator('.pop')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(window).toHaveCount(0)
  }

  // Staffing and routes read as words, and a route opens the other node's window.
  await page.getByTestId(`gate-node-${wayfindingBoard.gates[2].id}`).locator('.gt').click()
  const review = page.getByRole('dialog', { name: 'Gate · Adversarial review' })
  await expect(review.getByRole('button', { name: 'Judged by 8 Brief critic' })).toBeVisible()
  await expect(review.getByRole('button', { name: 'Fail ↺ back to 6 Draft the brief' })).toBeVisible()
  await review.getByRole('button', { name: 'Judged by 8 Brief critic' }).click()
  const critic = page.getByRole('dialog', { name: 'Formation · Brief critic' })
  await expect(critic.getByText('Agent is Codex Judge (codex-judge) on openai-codex, model gpt-5.5, medium effort.')).toBeVisible()
  await critic.getByRole('button', { name: 'Judges 7 Adversarial review' }).click()
  await expect(review).toHaveClass(/focused/)
  await review.getByRole('button', { name: 'Pass → 9 Brief sign-off' }).click()
  await expect(page.getByRole('dialog', { name: 'Gate · Brief sign-off' }).getByText('Pass → run ends here')).toBeVisible()

  expect(fixture.writes).toEqual([])
})
