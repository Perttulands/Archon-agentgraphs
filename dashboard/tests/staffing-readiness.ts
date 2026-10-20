import { expect, type Locator, type Page } from '@playwright/test'

/** The zoom label starts at 100% before the graph loads. Wait for its actual
 * slot, fonts and the animation frame in which the cockpit performs initial fit
 * before taking coordinates for a manual mouse gesture. */
export async function waitForCanvasSlot(page: Page, slot: Locator) {
  await expect(page.getByTestId('formations-canvas')).toBeVisible()
  await expect(slot).toBeVisible()
  await page.locator('[data-testid="formations-world"]').evaluate(async world => {
    await document.fonts.ready
    await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
    await Promise.all(world.getAnimations().map(animation => animation.finished))
  })
}

/** Opacity zero still counts as visible to Playwright. The sentence explicitly
 * removes `placing` after measuring and, where needed, panning its canvas. */
export async function waitForStaffingPlacement(sentence: Locator) {
  await expect(sentence).toBeVisible()
  await expect(sentence).not.toHaveClass(/placing/)
  await sentence.evaluate(element => Promise.all(element.getAnimations().map(animation => animation.finished)))
}
