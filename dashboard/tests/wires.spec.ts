import { expect, test } from '@playwright/test'
import { wayfinding, wayfindingFixture } from './wayfinding-fixture'

type Point = { x: number; y: number }
type Segment = [Point, Point]

/** The orthogonal corners of a rounded wire path: its start, each Q control point, its end. */
function corners(d: string): Point[] {
  const numbers = (text: string) => text.trim().split(/[\s,]+/).map(Number)
  const points: Point[] = []
  for (const [, command, args] of d.matchAll(/([MLQ])([^MLQ]*)/g)) {
    const values = numbers(args)
    if (command === 'M' || command === 'Q') points.push({ x: values[0], y: values[1] })
  }
  const last = numbers(d.slice(d.lastIndexOf('L') + 1))
  points.push({ x: last[0], y: last[1] })
  return points
}

/** Everything between a wire's two port stubs. */
const channel = (points: Point[]): Segment[] => points.slice(1, -2).map((point, index) => [point, points[index + 2]])

function shared([a1, a2]: Segment, [b1, b2]: Segment): boolean {
  const overlap = (p1: number, p2: number, q1: number, q2: number) => Math.min(Math.max(p1, p2), Math.max(q1, q2)) - Math.max(Math.min(p1, p2), Math.min(q1, q2)) > 1
  if (Math.abs(a1.x - a2.x) < 1 && Math.abs(b1.x - b2.x) < 1 && Math.abs(a1.x - b1.x) < 1) return overlap(a1.y, a2.y, b1.y, b2.y)
  if (Math.abs(a1.y - a2.y) < 1 && Math.abs(b1.y - b2.y) < 1 && Math.abs(a1.y - b1.y) < 1) return overlap(a1.x, a2.x, b1.x, b2.x)
  return false
}

test('Wayfinding loops are labelled back-references and no two share a channel segment', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.addInitScript(() => localStorage.clear())
  const fixture = await wayfindingFixture(page)
  await page.goto('/?board=wayfinding')

  const titleOf = (nodeId: string) => [...wayfinding.board.formations, ...wayfinding.board.gates].find((node: { id: string }) => node.id === nodeId)?.title
  const loops = (wayfinding.board.connections as Array<{ id: string; from: string; to: string }>).filter(connection => connection.from.endsWith(':fail'))
  expect(loops).toHaveLength(4)
  await expect(page.locator('path.wire.loop')).toHaveCount(4)
  for (const loop of loops) {
    await expect(page.getByTestId(`formation-wire-${loop.id}`)).toHaveClass(/\bloop\b/)
    await expect(page.getByTestId(`wire-label-${loop.id}`)).toHaveText(`↺ ${titleOf(loop.to.split(':')[0])}`)
  }
  const judge = (wayfinding.board.connections as Array<{ id: string; from: string }>).find(connection => connection.from.endsWith(':judge'))!
  await expect(page.getByTestId(`wire-label-${judge.id}`)).toHaveText('judges Adversarial review')

  await page.getByTitle('Fit', { exact: true }).click()
  await page.waitForTimeout(500)
  const routes = await Promise.all(loops.map(async loop => ({ id: loop.id, points: corners((await page.getByTestId(`formation-wire-${loop.id}`).getAttribute('d'))!) })))
  for (const route of routes) expect(route.points.length, `${route.id} corners`).toBe(6)
  for (let i = 0; i < routes.length; i += 1) {
    for (let j = i + 1; j < routes.length; j += 1) {
      for (const a of channel(routes[i].points)) {
        for (const b of channel(routes[j].points)) {
          expect(shared(a, b), `${routes[i].id} and ${routes[j].id} share ${JSON.stringify(a)}`).toBe(false)
        }
      }
    }
  }
  // Legend and slot words are one look away.
  await page.getByRole('button', { name: 'Legend' }).click()
  await expect(page.getByRole('dialog', { name: 'Canvas legend' })).toContainText('A gate sends work back to an earlier step')
  await page.screenshot({ path: test.info().outputPath('wayfinding-loops.png') })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: 'Canvas legend' })).toHaveCount(0)
  expect(fixture.writes).toEqual([])
})
