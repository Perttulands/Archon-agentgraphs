import { expect, test, type Page } from '@playwright/test'
import { failReasons, iterationsFixture, passReason, pickReason, type IterationsScenario } from './iterations-fixture'
import { settledBox } from './settled'

// A step's attempts open in one click where the step lives (archon-7963): a
// strip of every attempt with what it produced and what its gate said, keys
// to step and compare, and a pending best-of-N pick answered in place.

async function open(page: Page, scenario: IterationsScenario) {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await iterationsFixture(page, scenario)
  await page.goto('/?mission=browser&run=run_browser')
  await expect(page.getByTestId('formation-node-execution')).toBeVisible()
  return fixture
}

const columns = (page: Page) => page.getByTestId('attempts-window').locator('[data-attempt]')
const shown = (page: Page) => columns(page).evaluateAll(list => list.map(column => column.getAttribute('data-attempt')))
const focused = (page: Page) => page.getByTestId('attempts-window').locator('[data-attempt][aria-current="true"]')

test('a step that looped opens its attempts as one strip that tells the loop, steps with the keys and compares two', async ({ page }) => {
  await open(page, 'loop')
  await page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution').click()
  const win = page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })
  await expect(win).toBeVisible()
  await expect(columns(page)).toHaveCount(5)
  await expect.poll(() => shown(page)).toEqual(['1', '2', '3', '4', '5'])

  // Each attempt's verdict and reason sit beside what it made; the pass stands out.
  await expect(win.getByTestId('attempt-1-verdict-gate')).toContainText('Review gate · ✕ fail')
  await expect(win.getByTestId('attempt-1-verdict-gate')).toContainText(failReasons[0])
  await expect(win.getByTestId('attempt-5-verdict-gate')).toContainText(passReason)
  await expect(win.getByTestId('attempt-5')).toHaveClass(/settled/)
  await expect(win.getByTestId('attempts-pip-5')).toHaveClass(/settled/)
  await expect(focused(page)).toHaveAttribute('data-attempt', '5')
  await expect(win.getByTestId('attempt-2-output-notes')).toContainText('Seed 4134')
  // The oldest attempt's text was cut to the evidence budget, and says so.
  await expect(win.getByTestId('attempt-1-output-notes')).toContainText('… first 107 B of 6.1 KiB')

  // The images are drawn large enough to judge, every one in view.
  const images = win.locator('[data-attempt] .attempt-image img')
  await expect(images).toHaveCount(5)
  await expect.poll(() => images.evaluateAll(list => list.every(image => (image as HTMLImageElement).naturalWidth > 0))).toBe(true)
  for (let index = 0; index < 5; index++) {
    const box = await settledBox(page, images.nth(index))
    expect(box.height).toBeGreaterThan(300)
    expect(box.x + box.width).toBeLessThan(1920)
  }

  await page.keyboard.press('ArrowLeft')
  await expect(focused(page)).toHaveAttribute('data-attempt', '4')
  await page.keyboard.press('Home')
  await expect(focused(page)).toHaveAttribute('data-attempt', '1')
  await page.keyboard.press('3')
  await expect(focused(page)).toHaveAttribute('data-attempt', '3')

  // Side by side: the focused attempt and the one before it, in frames of one size.
  await page.keyboard.press('c')
  await expect(win.getByTestId('attempts-compare')).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(() => shown(page)).toEqual(['2', '3'])
  const frames = win.locator('[data-attempt] .attempt-image')
  const [left, right] = [await settledBox(page, frames.nth(0)), await settledBox(page, frames.nth(1))]
  expect(Math.round(left.width)).toBe(Math.round(right.width))
  expect(Math.round(left.height)).toBe(Math.round(right.height))
  expect(Math.round(left.y)).toBe(Math.round(right.y))
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => shown(page)).toEqual(['2', '4'])

  // A click zooms both to the same spot; Z fits them again.
  await frames.nth(0).click({ position: { x: left.width / 2, y: left.height / 4 } })
  await expect(frames.nth(0)).toHaveAttribute('aria-pressed', 'true')
  await expect(frames.nth(1)).toHaveAttribute('aria-pressed', 'true')
  await page.keyboard.press('z')
  await expect(frames.nth(1)).toHaveAttribute('aria-pressed', 'false')

  // Back in the strip on the side last touched: the zoom's click chose attempt 2.
  await page.keyboard.press('c')
  await expect(columns(page)).toHaveCount(5)
  await expect(focused(page)).toHaveAttribute('data-attempt', '2')
  await page.keyboard.press('Escape')
  await expect(win).toHaveCount(0)
})

test('the picked attempt is the one the strip settles on, with who picked it and why', async ({ page }) => {
  await open(page, 'picked')
  await page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution').click()
  const win = page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })
  await expect(win.getByTestId('attempt-3-outcome')).toHaveText('picked')
  await expect(win.getByTestId('attempt-3')).toHaveClass(/settled/)
  await expect(win.getByTestId('attempt-3-pick')).toContainText('Picked as the best · the judge')
  await expect(win.getByTestId('attempt-3-pick')).toContainText(pickReason)
  await expect(focused(page)).toHaveAttribute('data-attempt', '3')
})

test('a pending pick opens its candidates by itself, recommends the judge\'s choice and records the operator\'s', async ({ page }) => {
  const fixture = await open(page, 'pick')
  const win = page.getByRole('dialog', { name: 'Pick the best attempt of Draw Ahti' })
  await expect(win).toBeVisible()
  await expect(page.getByRole('dialog', { name: /^Answer gate/ })).toHaveCount(0)
  await expect(focused(page)).toHaveAttribute('data-attempt', '3')
  await expect(win.getByTestId('attempt-3-recommended')).toContainText(pickReason)
  await expect(win.getByTestId('attempts-pick-go')).toHaveText('Pick attempt 3')

  // Another attempt is one key away, and the button follows it.
  await win.getByTestId('attempts-pip-2').click()
  await expect(win.getByTestId('attempts-pick-go')).toHaveText('Pick attempt 2')
  await page.keyboard.press('ArrowRight')
  await expect(win.getByTestId('attempts-pick-go')).toHaveText('Pick attempt 3')

  await win.getByRole('textbox', { name: 'Why this attempt' }).fill('Cleanest face of the four')
  await win.getByTestId('attempts-pick-go').click()
  await expect.poll(() => fixture.picks).toEqual([expect.objectContaining({ actor: 'human:ui', attempt: 3, reason: 'Cleanest face of the four', requestedSeq: expect.any(Number) })])
  await expect(win).toHaveCount(0)

  // The pick shows on the attempt afterwards.
  await page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution').click()
  const after = page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })
  await expect(after.getByTestId('attempt-3-pick')).toContainText('Picked as the best · you')
  await expect(after.getByTestId('attempt-3-pick')).toContainText('Cleanest face of the four')
})

test('a folder output previews its images and lists the rest without drawing every file', async ({ page }) => {
  await open(page, 'folder')
  await page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution').click()
  const output = page.getByRole('dialog', { name: 'Attempts of Build meshes' }).getByTestId('attempt-1-output-out')
  await expect(output.locator('.attempt-chip').first()).toContainText('60 files')
  await expect(output.locator('.attempt-thumb')).toHaveCount(12)
  await expect(output.locator('.attempt-listing-file')).toHaveCount(8)
  await expect(output).toContainText('and 40 more')
  await output.locator('.attempt-listing-file').first().click()
  await expect(page.getByRole('dialog', { name: /piece_01\.glb/ })).toBeVisible()
})

test('a live run adds its new attempt to the open window, which follows it', async ({ page }) => {
  const fixture = await open(page, 'live')
  await page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution').click()
  const win = page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })
  await expect(focused(page)).toHaveAttribute('data-attempt', '5')
  await expect(win.getByTestId('attempt-5-outcome')).toHaveText('running')
  await expect(win.getByTestId('attempt-5')).toContainText('No output yet.')
  fixture.advance()
  await expect(win.getByTestId('attempt-6')).toBeVisible()
  await expect(focused(page)).toHaveAttribute('data-attempt', '6')
  await expect(win.getByTestId('attempt-5-outcome')).toHaveText('✕ fail')
})

test('Flow and the node window open the same attempts', async ({ page }) => {
  await open(page, 'loop')
  await page.getByRole('radio', { name: 'Flow', exact: true }).click()
  await page.locator('[data-flow-node="execution"]').getByTestId('attempts-open-execution').click()
  await expect(page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })).toBeVisible()
  await expect(columns(page)).toHaveCount(5)
})

test('a step that ran once still shows its attempt from its node window', async ({ page }) => {
  await open(page, 'single')
  // One attempt is no loop: the card offers nothing more.
  await expect(page.getByTestId('formation-node-execution').getByTestId('attempts-open-execution')).toHaveCount(0)
  await page.getByTestId('formation-node-execution').locator('.fhead .tt').click()
  const node = page.getByRole('dialog', { name: /Draw Ahti/ }).filter({ has: page.getByTestId('attempts-open-execution') })
  await node.getByTestId('attempts-open-execution').click()
  const win = page.getByRole('dialog', { name: 'Attempts of Draw Ahti' })
  await expect(columns(page)).toHaveCount(1)
  await expect(win.getByTestId('attempt-1-outcome')).toHaveText('✓ pass')
  await expect(win.getByTestId('attempts-compare')).toHaveCount(0)
})
