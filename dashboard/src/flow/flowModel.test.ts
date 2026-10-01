import { describe, expect, it } from 'vitest'
import delivery from '../../tests/fixtures/delivery.json'
import scouting from '../../tests/fixtures/scouting.json'
import type { BoardDocument } from '../components/formationsTypes'
import { buildFlow, type FlowStep, type FlowTarget } from './flowModel'

const words = (targets: FlowTarget[]) => targets.map(target => target.kind === 'step'
  ? `${target.back ? '↺ ' : ''}${target.number ?? '-'} ${target.title}`
  : target.kind === 'end' ? `end ${target.title} (${target.outcome})` : target.kind)
const outline = (step: FlowStep) => step.kind === 'gate'
  ? `${step.number} ${step.node.title} [${step.deciders.join('+')}]${step.judges.length ? ` judged by ${step.judges.map(judge => judge.title).join(', ')}` : ''} pass ${words(step.pass).join(', ')} fail ${words(step.fail).join(', ')}`
  : `${step.number} ${step.node.title} → ${words(step.next).join(', ')}`

describe('the flow model', () => {
  it('orders Scouting along its main path with gates inline, the judge nested and loops as back-references', () => {
    const flow = buildFlow(scouting.mission as unknown as BoardDocument)
    expect(flow.sections).toHaveLength(1)
    const [section] = flow.sections
    expect(section.mission?.title).toBe('Scouting')
    expect(words(section.start)).toEqual(['1 Map the territory'])
    expect(section.steps.map(outline)).toEqual([
      '1 Map the territory → 2 Framing review',
      '2 Framing review [you] pass 3 Question peers fail ↺ 1 Map the territory',
      '3 Question peers → 4 Answer questions',
      '4 Answer questions [you] pass 5 Draft the brief fail ↺ 3 Question peers',
      '5 Draft the brief → 6 Adversarial review',
      '6 Adversarial review [judge] judged by Brief critic pass 7 Brief sign-off fail ↺ 5 Draft the brief',
      '7 Brief sign-off [you] pass end Done (done) fail ↺ 5 Draft the brief',
    ])
    // End nodes are where paths end, never numbered steps.
    expect(flow.numbers.has('end_scouting_done')).toBe(false)
    // The judge is nested under its gate, not a step of its own.
    const critic = scouting.mission.formations.find(node => node.title === 'Brief critic')!
    expect(flow.numbers.has(critic.id)).toBe(false)
    expect(flow.judgeOf.get(critic.id)).toBe(section.steps[5].id)
    // Three loops: the run returns to map the territory, to question peers and to draft the brief.
    const loops = new Set(section.steps.flatMap(step => step.kind === 'gate' ? step.fail : []).flatMap(target => target.kind === 'step' && target.back ? [target.title] : []))
    expect([...loops]).toEqual(['Map the territory', 'Question peers', 'Draft the brief'])
  })

  it('orders Delivery with the Beads reviewer nested and the final review ending at its End node', () => {
    const flow = buildFlow(delivery.mission as unknown as BoardDocument)
    expect(flow.sections.map(section => section.mission?.title ?? null)).toEqual(['Deliver the supplied brief'])
    expect(flow.sections[0].steps.map(outline)).toEqual([
      '1 Plan → 2 Beads',
      '2 Beads → 3 Beads review',
      '3 Beads review [judge] judged by Beads reviewer pass 4 Execution fail ↺ 2 Beads',
      '4 Execution → 5 Final review',
      '5 Final review → end Delivered (done)',
    ])
  })

  it('lists steps no mission reaches after the missions, and a route not wired yet as leading nowhere', () => {
    const board = {
      inputCards: [{ id: 'mis', title: 'Mission', goal: '' }],
      formations: [
        { id: 'work', type: 'solo', title: 'Work', inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Output' }], slots: [] },
        { id: 'loose', type: 'solo', title: 'Loose', inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Output' }], slots: [] },
      ],
      gates: [{ id: 'check', title: 'Check', kinds: ['code', 'human'], criterion: '' }],
      connections: [
        { id: 'a', from: 'mis:out', to: 'work:in' },
        { id: 'b', from: 'work:out', to: 'check:in' },
      ],
    } as unknown as BoardDocument
    const flow = buildFlow(board)
    expect(flow.sections.map(section => [section.mission?.title ?? null, section.steps.map(outline)])).toEqual([
      ['Mission', ['1 Work → 2 Check', '2 Check [you+code] pass nowhere fail nowhere']],
      [null, ['3 Loose → nowhere']],
    ])
  })

  it('ends a path at an End node, which any number of routes may share', () => {
    const board = {
      inputCards: [{ id: 'mis', title: 'Mission', goal: '' }],
      formations: [{ id: 'work', type: 'solo', title: 'Work', inputs: [{ id: 'in', label: 'Input' }], outputs: [{ id: 'out', label: 'Output' }], slots: [] }],
      gates: [{ id: 'check', title: 'Check', kinds: ['human'], criterion: '' }],
      ends: [{ id: 'shipped', title: 'Shipped', outcome: 'done' }, { id: 'no', title: 'Rejected', outcome: 'rejected' }],
      connections: [
        { id: 'a', from: 'mis:out', to: 'work:in' },
        { id: 'b', from: 'work:out', to: 'check:in' },
        { id: 'c', from: 'check:pass', to: 'shipped:in' },
        { id: 'd', from: 'check:fail', to: 'no:in' },
      ],
    } as unknown as BoardDocument
    const flow = buildFlow(board)
    expect(flow.sections.map(section => section.steps.map(outline))).toEqual([['1 Work → 2 Check', '2 Check [you] pass end Shipped (done) fail end Rejected (rejected)']])
    const gate = flow.sections[0].steps[1]
    expect(gate.kind === 'gate' && gate.pass[0]).toEqual({ kind: 'end', nodeId: 'shipped', title: 'Shipped', outcome: 'done' })
    expect([...flow.numbers.keys()]).toEqual(['work', 'check'])
  })
})
