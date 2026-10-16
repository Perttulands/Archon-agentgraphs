import { type Page } from '@playwright/test'
import { crc32, deflateSync } from 'node:zlib'
import { board, cockpitFixture, runArtifacts, runCwd } from './cockpit-fixture'

/* A run whose Execution step ran several times (archon-7963), as the attempts
 * route reports it: each attempt drew a portrait, wrote notes and was judged
 * by the Review gate's judge. Scenarios:
 *
 * - loop: four failed attempts, the fifth passed; the run succeeded.
 * - live: the fifth attempt is still running; advance() records its output
 *   and starts a sixth.
 * - pick: all four failed and the Review gate waits for the operator to pick
 *   the best (archon-gihe); its judge recommends attempt 3. A pick is recorded
 *   and the run goes on.
 * - picked: the judge picked attempt 3 of four; the run succeeded.
 * - folder: each attempt produced a folder of meshes and textures.
 * - single: Execution passed on its first attempt.
 */

export type IterationsScenario = 'loop' | 'live' | 'pick' | 'picked' | 'folder' | 'single'

/** A PNG of the given size: a field of one colour with a block of another, placed per attempt, so attempts look apart. */
export function png(width: number, height: number, field: [number, number, number], block: [number, number, number], at: number): Buffer {
  const rows: Buffer[] = []
  const left = Math.floor(width * (.15 + .1 * (at % 5)))
  for (let y = 0; y < height; y++) {
    const row = Buffer.alloc(1 + width * 3)
    for (let x = 0; x < width; x++) {
      const inBlock = x >= left && x < left + width / 3 && y >= height / 3 && y < height / 2
      const [r, g, b] = inBlock ? block : field
      row[1 + x * 3] = r; row[2 + x * 3] = g; row[3 + x * 3] = b
    }
    rows.push(row)
  }
  const chunk = (type: string, data: Buffer) => {
    const head = Buffer.alloc(4)
    head.writeUInt32BE(data.length)
    const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
    const sum = Buffer.alloc(4)
    sum.writeUInt32BE(crc32(body))
    return Buffer.concat([head, body, sum])
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header[8] = 8; header[9] = 2; header[10] = 0; header[11] = 0; header[12] = 0
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(Buffer.concat(rows))),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

const text = (value: string) => ({ text: value, bytes: Buffer.byteLength(value) })
const at = (minute: number, second = 0) => `2026-10-16T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`
const judged = '/srv/state/judge-work/review'

export const failReasons = [
  'Hair removal fails (grey moustache and beard stubble) and identity fails (furrowed scowl, not neutral).',
  'Identity fails: broad-faced heavy bodybuilder, not the lean long-faced build; the other six checks pass.',
  'Facial stubble on lip, chin and jaw, and a stern scowl instead of a neutral face, fail hair_removal and identity.',
  'Stubble is gone but the jaw is heavier than the reference and the ears are cropped; identity fails.',
]
export const passReason = 'All eight checks pass: clean shaven, neutral expression, lean long face, full figure in frame.'
export const pickReason = 'Attempt 3 is closest: the build and face match; only light stubble remains, which retouching removes.'

/** What one attempt left behind. */
interface AttemptSpec {
  attempt: number
  verdict?: 'pass' | 'fail'
  reason?: string
  running?: boolean
  picked?: { by: string; reason: string }
}

function outputsOf(scenario: IterationsScenario, attempt: number) {
  const dir = `execution/${attempt}`
  if (scenario === 'folder') {
    const folder = `${dir}/outputs/out/meshes`
    const textures = Array.from({ length: 14 }, (_, index) => `${folder}/textures/part_${String(index + 1).padStart(2, '0')}.png`)
    const meshes = Array.from({ length: 46 }, (_, index) => `${folder}/lod${index % 3}/piece_${String(index + 1).padStart(2, '0')}.glb`)
    const files = [
      ...meshes.map(name => ({ name, path: `${runArtifacts}/${name}`, size: 182_000 + attempt * 1000, modifiedAt: at(attempt * 6), kind: 'binary' })),
      ...textures.map(name => ({ name, path: `${runArtifacts}/${name}`, size: 24_000, modifiedAt: at(attempt * 6), kind: 'image' })),
    ].sort((a, b) => a.name.localeCompare(b.name))
    return {
      outputs: [{ portId: 'out', label: 'Meshes', kind: 'folder', text: text(`${runArtifacts}/${folder}`), path: `${runArtifacts}/${folder}`, artifact: folder, fileKind: 'folder' }],
      files: [...files, { name: `${dir}/build.log`, path: `${runArtifacts}/${dir}/build.log`, size: 2100, modifiedAt: at(attempt * 6), kind: 'text' }],
    }
  }
  const image = `${dir}/outputs/out/ahti-base.png`
  const notes = `## Attempt ${attempt}\n\n- Seed ${4100 + attempt * 17}\n- Prompt weight on *clean shaven* raised to ${(1 + attempt * .2).toFixed(1)}\n- Reference: \`ahti-reference.png\``
  return {
    outputs: [
      { portId: 'out', label: 'Portrait', kind: 'file', text: text(`${runArtifacts}/${image}`), path: `${runArtifacts}/${image}`, artifact: image, size: 1_843_210 + attempt * 311, fileKind: 'image' },
      // The text budget favours the newest attempts, so a long loop cuts its oldest texts.
      { portId: 'notes', label: 'Notes', kind: 'text', text: scenario === 'loop' && attempt === 1 ? { ...text(notes), bytes: 6200, truncated: true } : text(notes) },
    ],
    files: [
      { name: image, path: `${runArtifacts}/${image}`, size: 1_843_210 + attempt * 311, modifiedAt: at(attempt * 6), kind: 'image' },
      { name: `${dir}/prompt.txt`, path: `${runArtifacts}/${dir}/prompt.txt`, size: 412, modifiedAt: at(attempt * 6), kind: 'text' },
      { name: `${dir}/seed.json`, path: `${runArtifacts}/${dir}/seed.json`, size: 38, modifiedAt: at(attempt * 6), kind: 'json' },
    ],
  }
}

function specsOf(scenario: IterationsScenario): AttemptSpec[] {
  const failed = failReasons.map((reason, index) => ({ attempt: index + 1, verdict: 'fail' as const, reason }))
  switch (scenario) {
    case 'single': return [{ attempt: 1, verdict: 'pass', reason: passReason }]
    case 'folder': return [{ attempt: 1, verdict: 'fail', reason: 'Six of the meshes have open seams at the shoulder.' }, { attempt: 2, verdict: 'pass', reason: 'Seams closed; every LOD imports cleanly.' }]
    case 'pick': return failed
    case 'picked': return failed.map(spec => (spec.attempt === 3 ? { ...spec, picked: { by: 'judge', reason: pickReason } } : spec))
    case 'live': return [...failed, { attempt: 5, running: true }]
    default: return [...failed, { attempt: 5, verdict: 'pass', reason: passReason }]
  }
}

export async function iterationsFixture(page: Page, scenario: IterationsScenario) {
  const fixture = await cockpitFixture(page, { run: true })
  // Execution draws a portrait and writes notes; a failed review sends it back.
  const mission = structuredClone(board)
  mission.formations[0].outputs = scenario === 'folder' ? [{ id: 'out', label: 'Meshes', kind: 'folder' }]
    : [{ id: 'out', label: 'Portrait', kind: 'file' }, { id: 'notes', label: 'Notes', kind: 'text' }]
  mission.formations[0].title = scenario === 'folder' ? 'Build meshes' : 'Draw Ahti'
  mission.gates[0] = { ...mission.gates[0], kinds: scenario === 'pick' ? ['formation', 'human'] : ['formation'] }
  mission.connections = [...mission.connections, { id: 'gate-fail', from: 'gate:fail', to: 'execution:in' }]

  let specs = specsOf(scenario)
  let picked: { attempt: number; reason: string } | null = null
  const picks: Array<Record<string, unknown>> = []

  const events = () => {
    const ledger: Array<Record<string, unknown>> = [{ type: 'run_started', timestamp: at(0) }]
    for (const spec of specs) {
      ledger.push({ type: 'node_started', nodeId: 'execution', attempt: spec.attempt, timestamp: at(spec.attempt * 6 - 5) })
      if (spec.running) continue
      ledger.push({ type: 'node_output', nodeId: 'execution', status: 'done', timestamp: at(spec.attempt * 6 - 1) })
      ledger.push({ type: 'gate_evaluating', nodeId: 'gate', gateId: 'gate', timestamp: at(spec.attempt * 6 - 1, 5) })
      ledger.push({ type: 'node_started', nodeId: 'judge', attempt: spec.attempt, timestamp: at(spec.attempt * 6 - 1, 10) })
      ledger.push({ type: 'node_output', nodeId: 'judge', status: 'done', timestamp: at(spec.attempt * 6, 30) })
      ledger.push({ type: 'gate_verdict', nodeId: 'gate', gateId: 'gate', verdict: spec.verdict, timestamp: at(spec.attempt * 6, 31) })
    }
    if (scenario === 'pick') {
      ledger.push({ type: 'human_input_requested', nodeId: 'gate', gateId: 'gate', timestamp: at(26) })
      if (picked) ledger.push({ type: 'human_verdict_recorded', nodeId: 'gate', gateId: 'gate', timestamp: at(28), data: { requestedSeq: ledger.length } })
    }
    const settled = specs.some(spec => spec.verdict === 'pass') || specs.some(spec => spec.picked) || picked
    if (settled) {
      ledger.push({ type: 'node_started', nodeId: 'peer', attempt: 1, timestamp: at(40) })
      if (scenario !== 'pick') {
        ledger.push({ type: 'node_output', nodeId: 'peer', status: 'done', timestamp: at(44) })
        ledger.push({ type: 'run_succeeded', timestamp: at(45) })
      }
    }
    return ledger.map((event, index) => ({ seq: index + 1, ...event }))
  }
  const pendingPick = { nodeId: 'execution', candidates: [1, 2, 3, 4], recommendation: { attempt: 3, reason: pickReason, decidedBy: 'judge' } }
  const requestedSeq = () => events().find(event => event.type === 'human_input_requested')?.seq as number

  const attempts = () => {
    const ledger = events()
    const startedSeq = (attempt: number) => ledger.find(event => event.type === 'node_started' && event.nodeId === 'execution' && event.attempt === attempt)?.seq as number
    return specs.map(spec => {
      const { outputs, files } = outputsOf(scenario, spec.attempt)
      const pick = spec.picked || (picked?.attempt === spec.attempt ? { by: 'human:ui', reason: picked.reason } : null)
      return {
        attempt: spec.attempt,
        status: spec.running ? 'running' : 'done',
        startedSeq: startedSeq(spec.attempt),
        startedAt: at(spec.attempt * 6 - 5),
        ...(spec.running ? {} : { endedAt: at(spec.attempt * 6 - 1, 12), outputSeq: startedSeq(spec.attempt) + 1 }),
        directory: { name: `execution/${spec.attempt}`, path: `${runArtifacts}/execution/${spec.attempt}` },
        outputs: spec.running ? [] : outputs,
        verdicts: spec.running ? [] : [{
          gateId: 'gate', gateTitle: 'Review gate', evaluationSeq: startedSeq(spec.attempt) + 2, seq: startedSeq(spec.attempt) + 5,
          verdict: spec.verdict, reason: text(spec.reason || ''),
          evidence: scenario === 'folder' ? [] : [
            { text: text(`${judged}/attempt-${spec.attempt}/verdict.md`) },
            { text: text(`${judged}/attempt-${spec.attempt}/crop-face.png`) },
            { text: text(`${judged}/attempt-${spec.attempt}/crop-jaw.png`) },
          ],
        }],
        files: spec.running ? [files[1]] : files,
        ...(pick ? { picked: {
          gateId: 'gate', gateTitle: 'Review gate', seq: startedSeq(spec.attempt) + 6, reason: text(pick.reason), decidedBy: pick.by,
          candidates: specs.filter(other => !other.running).map(other => other.attempt), trigger: 'whenSpent',
        } } : {}),
      }
    })
  }

  // The Review gate's judge ran once per judged attempt and returned its verdict.
  const judgeAttempts = () => {
    const ledger = events()
    return specs.filter(spec => !spec.running).map((spec, index) => {
      const startedSeq = ledger.find(event => event.type === 'node_started' && event.nodeId === 'judge' && event.attempt === spec.attempt)?.seq as number
      return {
        attempt: index + 1, status: 'done', startedSeq, outputSeq: startedSeq + 1,
        startedAt: at(spec.attempt * 6 - 1, 10), endedAt: at(spec.attempt * 6, 30),
        directory: { name: `judge/${index + 1}`, path: `${runArtifacts}/judge/${index + 1}` },
        outputs: [{ portId: 'out', label: 'Result', kind: 'text', text: text(`${spec.verdict === 'pass' ? 'PASS' : 'FAIL'}: ${spec.reason}`) }],
        verdicts: [], files: [],
      }
    })
  }

  const status = () => {
    const ledger = events()
    const final = ledger.some(event => event.type === 'run_succeeded')
    const waiting = scenario === 'pick' && !picked
    return {
      runId: 'run_browser', status: final ? 'succeeded' : waiting ? 'waiting_human' : 'running', final, missionSlug: 'browser', inputCardId: 'mission',
      eventCount: ledger.length, cwd: runCwd, ...(waiting ? { waitingGates: [{ gateId: 'gate', requestedSeq: requestedSeq(), pick: pendingPick }] } : {}),
    }
  }

  const colours: Array<[number, number, number]> = [[156, 120, 96], [120, 140, 160], [170, 150, 110], [110, 150, 120], [150, 110, 140], [130, 130, 130]]
  const respond = (data: unknown) => ({ json: { success: true, data } })
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()
    if (path === '/api/missions') return route.fulfill(respond({ missions: [mission] }))
    if (path === '/api/missions/browser' && method === 'GET') return route.fulfill({ ...respond({ mission }), headers: { ETag: 'fixture-etag' } })
    if (path === '/api/runs/run_browser') return route.fulfill(respond(status()))
    if (path === '/api/runs/run_browser/events') return route.fulfill(respond({ events: events() }))
    if (path === '/api/runs/run_browser/evidence/attempts') {
      const node = url.searchParams.get('node')
      const steps = [
        { nodeId: 'execution', title: mission.formations[0].title, outputs: mission.formations[0].outputs, attempts: attempts() },
        { nodeId: 'judge', title: 'Judge', judgeOf: ['gate'], outputs: [{ id: 'out', label: 'Result', kind: 'text' }], attempts: judgeAttempts() },
      ]
      return route.fulfill(respond({ attempts: { runId: 'run_browser', steps: steps.filter(step => !node || node === step.nodeId) } }))
    }
    // Raw bytes: each attempt's portrait and textures, and the judge's crops.
    const raw = path.match(/^\/api\/runs\/run_browser\/artifacts\/execution\/(\d+)\/(.+)$/)
    if (raw) {
      const attempt = Number(raw[1])
      if (raw[2].endsWith('.png')) {
        const texture = raw[2].includes('textures/')
        return route.fulfill({ contentType: 'image/png', body: png(texture ? 32 : 96, texture ? 32 : 144, colours[(attempt - 1) % colours.length], [60, 40, 30], texture ? Number(raw[2].match(/(\d+)\.png$/)?.[1] || 0) : attempt) })
      }
      return route.fulfill({ contentType: 'text/plain', body: `attempt ${attempt} ${raw[2]}` })
    }
    if (path === '/api/files/raw' && (url.searchParams.get('path') || '').startsWith(judged)) {
      const attempt = Number((url.searchParams.get('path') || '').match(/attempt-(\d+)/)?.[1] || 1)
      return route.fulfill({ contentType: 'image/png', body: png(48, 48, colours[(attempt - 1) % colours.length], [200, 60, 60], attempt + 2) })
    }
    const preview = path.match(/^\/api\/runs\/run_browser\/evidence\/artifacts\/(execution\/\d+\/.+)$/)
    if (preview) {
      const name = decodeURIComponent(preview[1])
      const body = `Prompt for ${name.split('/')[1]}: a lean, long-faced man, clean shaven, neutral expression.`
      return route.fulfill(respond({ artifact: { name, size: body.length, modifiedAt: at(1), kind: name.endsWith('.json') ? 'json' : 'text', text: text(body), path: `${runArtifacts}/${name}` } }))
    }
    if (path === '/api/runs/run_browser/gates/gate/request' && scenario === 'pick' && !picked) {
      return route.fulfill(respond({ request: {
        gateId: 'gate', requestedSeq: requestedSeq(), criterion: mission.gates[0].criterion,
        input: { fromNodeId: 'execution', fromPortId: 'out', truncated: false, text: 'Four attempts, none passed.' },
        pick: pendingPick,
      } }))
    }
    if (path === '/api/runs/run_browser/gates/gate/pick' && method === 'POST') {
      const body = route.request().postDataJSON() as { attempt: number; reason: string; requestedSeq: number }
      picks.push(body)
      if (body.requestedSeq !== requestedSeq()) return route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT', message: 'this pick request changed' } } })
      picked = { attempt: body.attempt, reason: body.reason || 'Picked by the operator.' }
      return route.fulfill(respond({ runId: 'run_browser' }))
    }
    return route.fallback()
  })

  return {
    ...fixture,
    mission,
    picks,
    /** The running attempt records its output and fails; the next one starts. */
    advance: () => {
      const running = specs.find(spec => spec.running)
      if (!running) return
      specs = [...specs.filter(spec => spec !== running), { attempt: running.attempt, verdict: 'fail', reason: 'Closer, but the scowl is back.' }, { attempt: running.attempt + 1, running: true }]
    },
  }
}
