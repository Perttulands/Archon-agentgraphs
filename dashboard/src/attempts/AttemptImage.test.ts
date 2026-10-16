import { describe, expect, it } from 'vitest'
import { fitImage, nativeFactor, zoomedImage } from './AttemptImage'

const frame = { width: 400, height: 300 }
const portrait = { width: 1024, height: 1536 }

describe('AttemptImage geometry', () => {
  it('fits an image whole and centred in its frame', () => {
    expect(fitImage(frame, portrait)).toEqual({ width: 200, height: 300, left: 100, top: 0 })
    expect(fitImage({ width: 0, height: 0 }, portrait)).toEqual({ width: 0, height: 0, left: 0, top: 0 })
  })

  it('zooms to the image\'s own pixels, or twice its fitted size when it is small', () => {
    expect(nativeFactor(frame, portrait)).toBeCloseTo(5.12)
    expect(nativeFactor(frame, { width: 100, height: 150 })).toBe(2)
  })

  it('puts the zoom\'s point at the frame\'s centre and shows no empty edge past the image', () => {
    const face = zoomedImage(frame, portrait, { x: .5, y: .2, factor: 0 })
    expect(face.width).toBeCloseTo(1024)
    expect(face.height).toBeCloseTo(1536)
    expect(face.left).toBeCloseTo(200 - .5 * 1024)
    expect(face.top).toBeCloseTo(150 - .2 * 1536)
    // A point at the image's corner holds the image against the frame's edge.
    const corner = zoomedImage(frame, portrait, { x: 0, y: 1, factor: 0 })
    expect(corner.left).toBe(0)
    expect(corner.top).toBeCloseTo(300 - 1536)
    // A zoom that still fits the frame across keeps the image centred that way.
    const slight = zoomedImage(frame, portrait, { x: 0, y: .5, factor: 1.5 })
    expect(slight.width).toBe(300)
    expect(slight.left).toBe(50)
  })
})
