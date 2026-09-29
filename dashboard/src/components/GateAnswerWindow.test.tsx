import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import GateAnswerWindow, { cardRects } from './GateAnswerWindow'
import FloatingWindow from '../windows/FloatingWindow'
import { WindowManagerProvider, useWindowManager } from '../windows/WindowManager'

function Harness() {
  const stack = useWindowManager()
  const [gateWindow, setGateWindow] = useState(false)
  const [other, setOther] = useState(false)
  return (
    <WindowManagerProvider stack={stack}>
      <button type="button" onClick={() => setGateWindow(true)}>open gate window</button>
      <button type="button" onClick={() => setOther(true)}>open other window</button>
      <GateAnswerWindow gateId="gate_review" gateTitle="Operator review" anchor={() => null} keepClear={() => []} onClose={() => {}}>
        <textarea aria-label="Your response" />
      </GateAnswerWindow>
      {gateWindow ? (
        <FloatingWindow id="node:gate_review" kind="node" title="Gate" label="Gate · Operator review" defaultSize={{ width: 400, height: 300 }} onClose={() => setGateWindow(false)}>gate</FloatingWindow>
      ) : null}
      {other ? (
        <FloatingWindow id="file:notes" kind="file" title="notes" label="file notes" defaultSize={{ width: 400, height: 300 }} onClose={() => setOther(false)}>notes</FloatingWindow>
      ) : null}
    </WindowManagerProvider>
  )
}

const z = (name: string) => Number(screen.getByRole('dialog', { name }).style.zIndex)

describe('GateAnswerWindow', () => {
  afterEach(cleanup)

  it('rises above the gate\'s node window when that opens, and leaves other windows alone', () => {
    render(<Harness />)
    fireEvent.click(screen.getByRole('button', { name: 'open gate window' }))
    expect(z('Answer gate Operator review')).toBeGreaterThan(z('Gate · Operator review'))
    // The operator can still raise the gate window deliberately.
    fireEvent.pointerDown(screen.getByRole('dialog', { name: 'Gate · Operator review' }))
    expect(z('Gate · Operator review')).toBeGreaterThan(z('Answer gate Operator review'))

    fireEvent.click(screen.getByRole('button', { name: 'open other window' }))
    expect(z('file notes')).toBeGreaterThan(z('Answer gate Operator review'))
  })

  it('measures the cards it keeps clear of', () => {
    document.body.innerHTML = '<div data-node="a"></div><div data-node="b"></div>'
    const [a, b] = [...document.querySelectorAll('[data-node]')]
    a.getBoundingClientRect = () => ({ left: 100, top: 200, right: 300, bottom: 400, width: 200, height: 200 }) as DOMRect
    b.getBoundingClientRect = () => ({ left: 400, top: 150, right: 600, bottom: 350, width: 200, height: 200 }) as DOMRect
    expect(cardRects(['a', 'b', 'missing'])).toEqual([{ left: 100, top: 200, width: 200, height: 200 }, { left: 400, top: 150, width: 200, height: 200 }])
    expect(cardRects(['missing'])).toEqual([])
    document.body.innerHTML = ''
  })
})
