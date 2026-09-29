import { afterEach, describe, expect, it } from 'vitest'
import { fileAnchor } from '../files/FileWindows'
import { nodeBoxes } from './cockpitScene'

// jsdom lays nothing out, so each element is given its box.
function place(element: Element, left: number, top: number, width: number, height: number) {
  element.getBoundingClientRect = () => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top, toJSON: () => ({}) })
}

afterEach(() => {
  document.body.innerHTML = ''
})

function flowRow() {
  document.body.innerHTML = `
    <div data-testid="flow-view">
      <li class="flow-step" data-flow-node="map">
        <div class="flow-body">
          <div class="flow-step-head"><span class="flow-number">1</span><button class="flow-title">Map the territory</button></div>
          <p class="flow-text">Map the territory before anyone proposes a solution.</p>
          <p class="flow-line flow-routes"><span class="flow-label">Next</span><button class="flow-link">→ 2 Framing review</button></p>
          <p class="flow-line"><span class="flow-label">Files</span><button class="flow-file">CONTRACT.md</button></p>
        </div>
      </li>
    </div>`
  const row = document.querySelector('.flow-step')!
  place(row, 518, 340, 1120, 200)
  place(row.querySelector('.flow-number')!, 537, 357, 24, 24)
  place(row.querySelector('.flow-title')!, 569, 357, 136, 18)
  place(row.querySelector('.flow-text')!, 535, 385, 1080, 36)
  place(row.querySelectorAll('.flow-label')[0], 535, 430, 60, 16)
  place(row.querySelector('.flow-link')!, 615, 430, 150, 16)
  place(row.querySelectorAll('.flow-label')[1], 535, 455, 60, 16)
  place(row.querySelector('.flow-file')!, 615, 455, 90, 16)
  return row
}

describe('cockpit scene', () => {
  it('keeps a Flow row\'s number, title, labels and links, not its text, as what must stay visible', () => {
    flowRow()
    expect(nodeBoxes(['map'])).toEqual([
      { left: 537, top: 357, width: 24, height: 24 },
      { left: 569, top: 357, width: 136, height: 18 },
      { left: 535, top: 430, width: 60, height: 16 },
      { left: 615, top: 430, width: 150, height: 16 },
      { left: 535, top: 455, width: 60, height: 16 },
    ])
  })

  it('opens a file from a Flow row beside its chip, keeping the row\'s title and links in view', () => {
    const row = flowRow()
    const chipPlace = fileAnchor(row.querySelector('.flow-file')!)
    // The row spans the column, so the chip, not the row, is what the window opens beside.
    expect(chipPlace.anchor).toEqual({ left: 615, top: 455, width: 90, height: 16 })
    expect(chipPlace.keepClear).toEqual(nodeBoxes(['map']))
  })

  it('opens a file from a card beside the card, keeping the card and its note in view', () => {
    document.body.innerHTML = `
      <div class="formation" data-node="map"><button class="chip">CONTRACT.md</button></div>
      <div class="note-sticky" data-note-node="map"></div>`
    place(document.querySelector('.formation')!, 450, 482, 162, 180)
    place(document.querySelector('.chip')!, 460, 640, 80, 14)
    place(document.querySelector('.note-sticky')!, 482, 670, 130, 50)
    const cardPlace = fileAnchor(document.querySelector('.chip')!)
    expect(cardPlace.anchor).toEqual({ left: 450, top: 482, width: 162, height: 180 })
    expect(cardPlace.keepClear).toEqual([{ left: 450, top: 482, width: 162, height: 180 }, { left: 482, top: 670, width: 130, height: 50 }])
  })
})
