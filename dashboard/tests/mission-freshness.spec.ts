import { expect, test } from '@playwright/test'
import { cockpitFixture } from './cockpit-fixture'
import type { LayoutDocument } from '../src/components/formationsTypes'

for (const width of [1536, 1680, 1920, 2560]) {
  test(`external source and layout changes reconcile at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1080 })
    const fixture = await cockpitFixture(page)
    let changed = false
    let snapshot: LayoutDocument
    const reason = '/workspace/missing.mission.toml: source does not exist'
    await page.route('**/api/missions', route => route.fulfill({ json: { success: true, data: { missions: [
      { ...fixture.board(), title: changed ? 'Renamed externally' : fixture.board().title },
      changed ? { ...fixture.board(), id: 'new', slug: 'new', title: 'Created externally' } : { slug: 'broken', broken: reason },
    ] } } }))
    await page.route('**/api/missions/browser/layout', async route => {
      if (!changed) return route.fallback()
      await route.fulfill({ json: { success: true, data: { layout: { ...snapshot, etag: 'external-layout',
        nodes: snapshot.nodes.map((node, i) => i === 0 ? { ...node, x: 280, y: 280 } : node),
      } } } })
    })
    await page.goto('/?mission=browser')
    const problem = page.getByTestId('source-problem-broken')
    await expect(problem).toBeVisible()
    await problem.locator('summary').focus()
    await page.keyboard.press('Enter')
    await expect(problem).toHaveAttribute('open', '')
    await expect(problem).toContainText(reason)
    await expect(page.getByTestId('broken-missions')).toHaveCount(0)
    await page.getByRole('radio', { name: 'Flow', exact: true }).click()
    await expect(problem).toBeVisible()
    await page.getByRole('radio', { name: 'Canvas', exact: true }).click()
    const layout = await page.evaluate(async () => (await (await fetch('/api/missions/browser/layout')).json()).data.layout)
    snapshot = layout
    const card = page.locator(`[data-node="${layout.nodes[0].id}"]`).first()
    changed = true
    await expect(page.getByTestId('nav-group-browser')).toContainText('Renamed externally')
    await expect(page.getByTestId('nav-group-new')).toContainText('Created externally')
    await expect(problem).toHaveCount(0)
    await expect(card).toHaveCSS('left', '280px')
    await expect(card).toHaveCSS('top', '280px')
    expect(fixture.writes).toEqual([])
  })
}
