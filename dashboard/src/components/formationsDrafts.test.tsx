import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DraftMarker, findingText, findingsByNode, unresolvedFindings } from './formationsDrafts'
import type { BoardDocument } from './formationsTypes'

const board = {
  id: 'brd_draft',
  slug: 'draft',
  title: 'Draft',
  rev: 1,
  etag: 'etag',
  formations: [],
  connections: [{ id: 'edge_plan_gate', from: 'fmn_plan:port_out', to: 'gate_lint:in' }],
} as BoardDocument

describe('draft findings', () => {
  it('groups findings by node and marks both ends of a connection finding', () => {
    const slot = { code: 'unstaffed_slot', nodeId: 'fmn_plan', message: 'needs an agent' }
    const edge = { code: 'dangling_connection', nodeId: 'edge_plan_gate', message: 'broken endpoint' }
    const boardLevel = { code: 'mission_count', nodeId: '', message: 'add a mission' }

    const byNode = findingsByNode(board, [slot, edge, boardLevel])

    expect([...byNode.keys()].sort()).toEqual(['fmn_plan', 'gate_lint'])
    expect(byNode.get('fmn_plan')).toEqual([slot, edge])
    expect(byNode.get('gate_lint')).toEqual([edge])
  })

  it('keeps run findings only while validation still reports them', () => {
    const slot = { code: 'unstaffed_slot', nodeId: 'fmn_plan', message: 'old wording' }
    const gate = { code: 'gate_not_routable', nodeId: 'gate_lint', message: 'needs a value' }

    expect(unresolvedFindings([slot, gate], [{ ...slot, message: 'new wording' }])).toEqual([slot])
  })

  it('drops the node reference the canvas already shows', () => {
    expect(findingText({ code: 'gate_not_routable', nodeId: 'gate_lint', message: 'gate "gate_lint" needs a judge chain' })).toBe('needs a judge chain')
    expect(findingText({ code: 'orchestrated_controller', nodeId: 'fmn_b', message: 'orchestrated formation "fmn_b" needs exactly one controller slot; it has 0' })).toBe('needs exactly one controller slot; it has 0')
    expect(findingText({ code: 'dangling_connection', nodeId: 'edge_x', message: 'connection "edge_x" has a broken \'to\' endpoint "gate_y:in"' })).toBe('has a broken \'to\' endpoint "gate_y:in"')
    expect(findingText({ code: 'custom', nodeId: 'fmn_a', message: 'slot "fmn_a" wording stays' })).toBe('wording stays')
    expect(findingText({ code: 'custom', nodeId: 'fmn_a', message: 'uses 3 "fmn_a" digits' })).toBe('uses 3 "fmn_a" digits')
  })

  it('renders nothing for a complete node and a quiet tag for a draft', () => {
    const { container, rerender } = render(<DraftMarker nodeId="fmn_plan" findings={[]} blocked={false} />)
    expect(container).toBeEmptyDOMElement()

    rerender(<DraftMarker nodeId="fmn_plan" findings={[{ code: 'unstaffed_slot', nodeId: 'fmn_plan', message: 'needs an agent' }]} blocked={false} />)
    expect(screen.getByTestId('draft-marker-fmn_plan')).toHaveTextContent('draft')
    expect(screen.getByRole('note', { name: 'Draft: needs an agent' })).toBeInTheDocument()
  })
})
