/* Staffing a slot: the rules every way of staffing in the cockpit follows.
 * A slot owns its harness, model and effort; a role is optional role text, and
 * a slot without one is a vanilla agent ("Claude Code · opus · low"). Ported
 * from variant A, "Sentence", of the archon-n7u.56 prototypes (proto/staffing
 * catalog.ts and model.ts), with the catalog and roles served by the daemon
 * instead of a client-side store. */
import type { AgentProjection, EffortPolicyEntry, FormationSlot, HarnessModel, LaunchableHarness } from '../components/formationsTypes'
import { harnessName } from '../components/harnessIcons'

/** What a slot runs. A blank role is vanilla; a blank model is the harness default. */
export interface Staffing {
  role: string
  harness: string
  model: string
  effort: string
}

export interface RoleEntry {
  id: string
  name: string
  kind: string
  summary: string
}

/** What staffing can choose from: the daemon's harnesses with their models, the roles, and the effort policy. */
export interface StaffingCatalog {
  harnesses: LaunchableHarness[]
  roles: RoleEntry[]
  policy: EffortPolicyEntry[]
}

/** Every effort in rising order; the digits 1-6 pick them. */
export const EFFORT_ORDER = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']

export const DEFAULT_MODEL_WORDS = 'default model'

export function rolesOf(agents: AgentProjection[]): RoleEntry[] {
  return agents
    .filter(agent => agent.assignable && !agent.unbound)
    .map(agent => ({ id: agent.id, name: agent.displayName || agent.id, kind: agent.kind || '', summary: agent.summary || '' }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** A role's name as the roster gives it, else its id. */
export function roleNamer(agents: AgentProjection[]): (id: string) => string {
  const names = new Map(agents.map(agent => [agent.id, agent.displayName || agent.id]))
  return id => names.get(id) || id
}

export function staffingOf(slot: FormationSlot): Staffing | null {
  if (!slot.agentId && !slot.harness && !slot.model && !slot.effort) return null
  return { role: slot.agentId || '', harness: slot.harness || '', model: slot.model || '', effort: slot.effort || '' }
}

/** The assignSlot fields that state a staffing in full; null empties the slot. */
export function slotSettings(staffing: Staffing | null): { agentId: string; harness: string; model: string; effort: string } {
  return staffing
    ? { agentId: staffing.role, harness: staffing.harness, model: staffing.model, effort: staffing.effort }
    : { agentId: '', harness: '', model: '', effort: '' }
}

export function sameStaffing(a: Staffing | null | undefined, b: Staffing | null | undefined): boolean {
  if (!a || !b) return !a && !b
  return a.role === b.role && a.harness === b.harness && a.model === b.model && a.effort === b.effort
}

export function modelWords(model: string): string {
  return model || DEFAULT_MODEL_WORDS
}

/** "Claude Code · opus · low". */
export function captionText(staffing: Staffing): string {
  return `${harnessName(staffing.harness) || staffing.harness || 'no harness'} · ${modelWords(staffing.model)} · ${staffing.effort || 'no effort'}`
}

export function roleName(catalog: Pick<StaffingCatalog, 'roles'>, role: string): string {
  if (!role) return 'vanilla'
  return catalog.roles.find(entry => entry.id === role)?.name || role
}

// ---------- the catalog ----------

export function modelsOf(catalog: StaffingCatalog, harness: string): HarnessModel[] {
  return catalog.harnesses.find(entry => entry.id === harness)?.models || []
}

/** The model a slot takes when it moves to a harness: the first it is known to run, else its default. */
export function firstModel(catalog: StaffingCatalog, harness: string): string {
  return modelsOf(catalog, harness)[0]?.id || ''
}

export function harnessForModel(catalog: StaffingCatalog, model: string): string | null {
  return catalog.harnesses.find(harness => (harness.models || []).some(entry => entry.id === model))?.id || null
}

/** A model outside its harness's catalog: accepted, with a warning. A harness with no catalog warns of nothing. */
export function offCatalog(catalog: StaffingCatalog, staffing: Pick<Staffing, 'harness' | 'model'>): boolean {
  const models = modelsOf(catalog, staffing.harness)
  return Boolean(staffing.model) && models.length > 0 && !models.some(entry => entry.id === staffing.model)
}

/** The efforts a slot may take: its model's own when the daemon knows them, else its harness's. */
export function effortsFor(catalog: StaffingCatalog, harness: string, model: string): string[] {
  const known = modelsOf(catalog, harness).find(entry => entry.id === model)
  if (known?.efforts?.length) return known.efforts
  return catalog.harnesses.find(entry => entry.id === harness)?.efforts || []
}

/** Every effort any harness takes, in rising order. */
export function allEfforts(catalog: StaffingCatalog): string[] {
  const taken = new Set(catalog.harnesses.flatMap(harness => harness.efforts))
  return [...EFFORT_ORDER.filter(effort => taken.has(effort)), ...[...taken].filter(effort => !EFFORT_ORDER.includes(effort))]
}

function rank(effort: string): number {
  const index = EFFORT_ORDER.indexOf(effort)
  return index < 0 ? EFFORT_ORDER.length : index
}

/** The nearest effort a slot takes at or below the one wanted: ultra on Claude Code is max. */
export function clampEffort(catalog: StaffingCatalog, harness: string, model: string, effort: string): string {
  const efforts = effortsFor(catalog, harness, model)
  if (!efforts.length || efforts.includes(effort)) return effort
  const below = efforts.filter(candidate => rank(candidate) <= rank(effort))
  return below.length ? below[below.length - 1] : efforts[0]
}

/** Why a slot cannot take an effort, in words; '' when it can. */
export function effortRefusal(catalog: StaffingCatalog, harness: string, model: string, effort: string): string {
  const efforts = effortsFor(catalog, harness, model)
  if (!efforts.length || efforts.includes(effort)) return ''
  const modelNarrows = modelsOf(catalog, harness).some(entry => entry.id === model && entry.efforts?.length)
  const subject = modelNarrows ? model : harnessName(harness) || harness
  const elsewhere = catalog.harnesses.filter(entry => entry.id !== harness && entry.efforts.includes(effort)).map(entry => harnessName(entry.id) || entry.id)
  const top = efforts[efforts.length - 1]
  return `${subject} goes up to ${top}${!modelNarrows && elsewhere.length ? `; ${effort} is ${elsewhere.join(' and ')} only` : ''}`
}

// ---------- the effort policy ----------

export interface Suggestion {
  effort: string
  /** Why, citing the policy line the effort comes from: "planning falls under architecture and review". */
  reason: string
}

/** What each kind the policy names does, so a reason cites the policy line rather than the kind. */
const ACTIVITY: Record<string, string> = {
  verifier: 'verifying', scout: 'scouting',
  builder: 'building', debugger: 'debugging', operator: 'operating',
  reviewer: 'reviewing', judge: 'judging', architect: 'designing', designer: 'designing', planner: 'planning', orchestrator: 'orchestrating',
}

type Rule = { test: RegExp; effort: string }

/**
 * A role's words, read only when its kind is one the policy does not name.
 * max is never among them: it is chosen by hand for consequential reviews.
 */
const RULES: Rule[] = [
  { test: /\b(review|reviews|reviewer|critic|judge|verdict|architect\w*|design|plan\w*|orchestrat\w*)\b/i, effort: 'xhigh' },
  { test: /\b(build\w*|implement\w*|make|making|draft\w*|writ\w*|worker|execut\w*|integrat\w*|debug\w*|fix\w*|triage|operat\w*)\b/i, effort: 'medium' },
  { test: /\b(scout|errand\w*|record\w*|runner|chore|fetch|lookup|verif\w*|observ\w*)\b/i, effort: 'low' },
]

/** The policy line an effort comes from, in its own words: "architecture and review". */
function policyUse(catalog: Pick<StaffingCatalog, 'policy'>, effort: string): string {
  return catalog.policy.find(entry => entry.effort === effort)?.use || `${effort} work`
}

/** The effort the policy names for a role kind; never max, which is chosen by hand. */
function kindEffort(catalog: Pick<StaffingCatalog, 'policy'>, kind: string): string | null {
  const wanted = kind.trim().toLowerCase()
  if (!wanted) return null
  return catalog.policy.find(entry => (entry.kinds || []).includes(wanted))?.effort || null
}

function byRule(catalog: Pick<StaffingCatalog, 'policy'>, subject: string, words: string): Suggestion | null {
  const rule = RULES.find(candidate => candidate.test.test(words))
  return rule ? { effort: rule.effort, reason: `${subject} reads as ${policyUse(catalog, rule.effort)}` } : null
}

/**
 * The policy's suggestion for a role, from its kind (Perttu's policy,
 * 2026-10-01), else for the step it sits in. A role's name and summary are
 * read only when its kind is one the policy does not name. Never max. The
 * reason cites the policy line: "planning falls under architecture and review".
 */
export function suggestEffort(catalog: Pick<StaffingCatalog, 'policy'>, role: RoleEntry | undefined, stepTitle: string): Suggestion | null {
  if (role) {
    const kind = role.kind.trim().toLowerCase()
    const effort = kindEffort(catalog, kind)
    if (effort) return { effort, reason: `${ACTIVITY[kind] || `${kind} work`} falls under ${policyUse(catalog, effort)}` }
    const read = byRule(catalog, role.name, `${role.name} ${role.kind}`) || (role.summary ? byRule(catalog, role.name, role.summary) : null)
    if (read) return read
  }
  return stepTitle ? byRule(catalog, `the step “${stepTitle}”`, stepTitle) : null
}

/** "low errands · medium making things · xhigh architecture and review · max consequential reviews". */
export function policyLine(catalog: StaffingCatalog): string {
  return catalog.policy.map(entry => `${entry.effort} ${entry.use}`).join(' · ')
}

export function policyWords(catalog: StaffingCatalog, effort: string): string {
  return catalog.policy.find(entry => entry.effort === effort)?.use || ''
}

// ---------- what a pick, a drop or typed words do to a slot ----------

/** Where a staffing happens: the slot and the step, for the policy and the words. */
export interface SlotContext {
  label: string
  step: string
}

export interface Outcome {
  next: Staffing
  /** Something to read: a clamp, a harness switch, the policy's reason. */
  note?: string
  /** Refused, in plain words; nothing changes. */
  refused?: string
  /** The policy's effort, offered when a role lands on a staffed slot that keeps another. */
  offer?: Suggestion
}

/** A new slot's first staffing: vanilla on the first harness and its first model, at the step's policy effort or low. */
export function freshStaffing(catalog: StaffingCatalog, context: SlotContext): Staffing {
  const harness = catalog.harnesses[0]?.id || 'claude-code'
  const model = firstModel(catalog, harness)
  const suggestion = suggestEffort(catalog, undefined, context.step)
  return { role: '', harness, model, effort: suggestion ? clampEffort(catalog, harness, model, suggestion.effort) : 'low' }
}

export function roleSuggestion(catalog: StaffingCatalog, context: SlotContext, role: RoleEntry | undefined): Suggestion | null {
  return suggestEffort(catalog, role, context.step) || suggestEffort(catalog, undefined, context.label)
}

/**
 * Whose effort a slot keeps when a role lands: `policy` for a fresh slot,
 * which takes the policy's effort; `slot` for a staffed slot, which keeps its
 * settings; `stated` for an effort chosen in the open sentence (picked, typed
 * or set by digit), which is kept as stated.
 */
export type EffortHold = 'policy' | 'slot' | 'stated'

/**
 * The one rule wherever a role lands, with no memory beyond the open
 * sentence. A fresh slot takes the policy's effort. A staffed slot, or an
 * effort stated in the sentence, keeps its effort and is offered the policy's
 * in one click when it differs. The note says why.
 */
export function withRole(catalog: StaffingCatalog, context: SlotContext, base: Staffing, role: RoleEntry | null, hold: EffortHold): Outcome {
  if (!role) return { next: { ...base, role: '' }, note: `Vanilla: no role. ${captionText(base)} stays.` }
  const suggestion = roleSuggestion(catalog, context, role)
  if (!suggestion) return { next: { ...base, role: role.id }, note: `${role.name}: the policy names no effort for this role, so ${base.effort} stays.` }
  const effort = clampEffort(catalog, base.harness, base.model, suggestion.effort)
  if (effort === base.effort) return { next: { ...base, role: role.id }, note: `Effort ${effort}: ${suggestion.reason}.` }
  if (hold === 'policy') return { next: { ...base, role: role.id, effort }, note: `Effort ${effort}: ${suggestion.reason}.` }
  const why = hold === 'stated' ? `${base.effort} stays: you chose it.` : `${captionText(base)} stays: the slot keeps its settings.`
  return {
    next: { ...base, role: role.id },
    note: `${why} Policy suggests ${effort}: ${suggestion.reason}.`,
    offer: { effort, reason: suggestion.reason },
  }
}

/** A model outside the catalog is accepted on the slot's harness, with a warning. */
export function withFreeModel(base: Staffing, model: string): Outcome {
  return { next: { ...base, model }, note: `${model}: not in the catalog; the harness decides.` }
}

export function withModel(catalog: StaffingCatalog, base: Staffing, harness: string, model: string, harnessChosen = false): Outcome {
  const home = (model && harnessForModel(catalog, model)) || harness
  const effort = clampEffort(catalog, home, model, base.effort)
  const notes: string[] = []
  if (home !== base.harness && harnessChosen) notes.push(`Model ${modelWords(model)}: the first one ${harnessName(home)} lists.`)
  else if (home !== base.harness) notes.push(`Harness is now ${harnessName(home)}: ${model} runs there, not on ${harnessName(base.harness)}.`)
  if (effort !== base.effort) notes.push(`${model && modelsOf(catalog, home).some(entry => entry.id === model && entry.efforts?.length) ? model : harnessName(home)} goes up to ${effort}, so ${base.effort} became ${effort}.`)
  return { next: { ...base, harness: home, model, effort }, note: notes.join(' ') || undefined }
}

export function withHarness(catalog: StaffingCatalog, base: Staffing, harness: string): Outcome {
  if (harness === base.harness) return { next: base }
  return withModel(catalog, base, harness, firstModel(catalog, harness), true)
}

export function withEffort(catalog: StaffingCatalog, base: Staffing, effort: string): Outcome {
  const refusal = effortRefusal(catalog, base.harness, base.model, effort)
  if (refusal) return { next: base, refused: `${refusal}.` }
  return { next: { ...base, effort } }
}

// ---------- typed words ----------

/** How free-order words ("cri ast", "sonnet high") were read against a staffing. */
export interface Parsed {
  result: Staffing
  /** Each word and what it was read as, for the echo line. */
  read: { word: string; as: string }[]
  /** Problems in plain words; a blocking one keeps Enter from staffing. */
  issues: { text: string; blocking: boolean }[]
  /** One-click readings, such as the model a typo was close to. */
  alternatives: { label: string; staffing: Staffing; effortTyped: boolean; roleTouched: boolean; harnessTyped: boolean }[]
  roleTouched: boolean
  effortTyped: boolean
  harnessTyped: boolean
  /** A word several roles answer: shown as choices, never committed silently. */
  ambiguous?: { word: string; roles: RoleEntry[] }
  /** A model the catalog does not know, accepted with a warning. */
  offCatalog?: string
}

const HARNESS_WORDS: Record<string, string> = {
  claude: 'claude-code', 'claude-code': 'claude-code', cc: 'claude-code',
  codex: 'openai-codex', openai: 'openai-codex', 'openai-codex': 'openai-codex',
}
const EFFORT_ALIASES: Record<string, string> = { med: 'medium', mid: 'medium', hi: 'high', xh: 'xhigh', 'x-high': 'xhigh', 'extra-high': 'xhigh', lo: 'low' }
const VANILLA_WORDS = new Set(['vanilla', 'none', 'norole', '-role', 'no-role'])

function editDistance(a: string, b: string): number {
  const dp = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array<number>(b.length).fill(0)])
  for (let j = 1; j <= b.length; j++) dp[0][j] = j
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      dp[i][j] = Math.min(dp[i - 1][j] + 1, dp[i][j - 1] + 1, dp[i - 1][j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1))
    }
  }
  return dp[a.length][b.length]
}

/** Each known model, and the last part of a model name when only one model ends that way: "astra" is gpt-6-astra. */
function modelWordsOf(catalog: StaffingCatalog): Record<string, string> {
  const words: Record<string, string> = {}
  const tails = new Map<string, string[]>()
  for (const harness of catalog.harnesses) {
    for (const model of harness.models || []) {
      words[model.id] = model.id
      const tail = model.id.split('-').pop() || ''
      if (tail && tail !== model.id && !/^[\d.]+$/.test(tail)) tails.set(tail, [...(tails.get(tail) || []), model.id])
    }
  }
  for (const [tail, models] of tails) if (models.length === 1 && !(tail in words)) words[tail] = models[0]
  return words
}

/** Three or more letters that start exactly one known word read as that word: "ast" is astra, "xhi" is xhigh. */
function expandPrefix(word: string, known: string[], canonical: (word: string) => string): string {
  if (word.length < 3 || known.includes(word)) return word
  const hits = [...new Set(known.filter(candidate => candidate.startsWith(word)).map(canonical))]
  return hits.length === 1 ? hits[0] : word
}

function closeModel(catalog: StaffingCatalog, word: string): string | null {
  let best: string | null = null
  let score = 3
  for (const model of catalog.harnesses.flatMap(harness => (harness.models || []).map(entry => entry.id))) {
    const tail = model.split('-').pop() || model
    const distance = Math.min(editDistance(word, model), tail !== model ? editDistance(word, tail) : 99)
    if (distance < score) { score = distance; best = model }
  }
  return score <= 2 ? best : null
}

/** How well a role's name answers the typed fragments: 0 its name starts with them, 1 a word does, 2 it contains them. */
function roleRank(role: RoleEntry, words: string[]): number | null {
  const name = role.name.toLowerCase()
  if (!words.every(word => name.includes(word) || role.id.includes(word))) return null
  if (name.startsWith(words[0])) return 0
  if (name.split(/\s+/).some(part => part.startsWith(words[0]))) return 1
  return 2
}

/** Roles whose name holds every typed fragment, best first. */
export function matchRoles(roles: RoleEntry[], query: string): RoleEntry[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return roles
  return roles
    .map(role => { const r = roleRank(role, words); return r === null ? null : { role, score: r + role.name.length / 100 } })
    .filter((entry): entry is { role: RoleEntry; score: number } => Boolean(entry))
    .sort((a, b) => a.score - b.score)
    .map(entry => entry.role)
}

/** The roles tied for the best reading; more than one means the word is ambiguous. */
function bestRoles(roles: RoleEntry[], query: string): RoleEntry[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return []
  const ranked = roles.map(role => ({ role, rank: roleRank(role, words) })).filter((entry): entry is { role: RoleEntry; rank: number } => entry.rank !== null)
  if (!ranked.length) return []
  const best = Math.min(...ranked.map(entry => entry.rank))
  return ranked.filter(entry => entry.rank === best).map(entry => entry.role)
}

/**
 * Reads free-order words such as "cri ast" or "sonnet high" against a base
 * staffing. Parts not typed keep the base; a typed role brings the policy's
 * effort through withRole when the words are applied.
 */
export function parseWords(catalog: StaffingCatalog, input: string, base: Staffing): Parsed {
  const words = input.toLowerCase().split(/\s+/).filter(Boolean)
  const models = modelWordsOf(catalog)
  const efforts = allEfforts(catalog)
  const known = [...Object.keys(HARNESS_WORDS), ...Object.keys(models), ...efforts, ...VANILLA_WORDS]
  const canonical = (word: string) => HARNESS_WORDS[word] || models[word] || word
  const read: Parsed['read'] = []
  const issues: Parsed['issues'] = []
  let harness: string | null = null
  let model: string | null = null
  let effort: string | null = null
  let role: RoleEntry | null | undefined // null is vanilla; undefined is untouched
  const roleWords: string[] = []
  for (const raw of words) {
    const word = expandPrefix(raw.replace(/[·,]/g, ''), known, canonical)
    if (!word) continue
    if (VANILLA_WORDS.has(word)) { role = null; read.push({ word: raw, as: 'no role' }); continue }
    const harnessWord = HARNESS_WORDS[word] || (catalog.harnesses.some(entry => entry.id === word) ? word : null)
    if (harnessWord && catalog.harnesses.some(entry => entry.id === harnessWord)) { harness = harnessWord; read.push({ word: raw, as: harnessName(harnessWord) }); continue }
    if (models[word]) { model = models[word]; read.push({ word: raw, as: `model ${model}` }); continue }
    const effortWord = EFFORT_ALIASES[word] || (efforts.includes(word) ? word : null)
    if (effortWord) { effort = effortWord; read.push({ word: raw, as: `effort ${effortWord}` }); continue }
    roleWords.push(word)
  }
  let ambiguous: Parsed['ambiguous']
  let offCatalogModel: string | undefined
  let guess: { word: string; model: string } | null = null
  if (roleWords.length) {
    const query = roleWords.join(' ')
    const tied = bestRoles(catalog.roles, query)
    if (tied.length > 1) {
      ambiguous = { word: query, roles: tied }
      issues.push({ text: `“${query}” could be ${tied.length} roles: pick one.`, blocking: true })
    } else if (tied[0]) {
      role = tied[0]
      read.push({ word: query, as: `role ${tied[0].name}` })
    } else if (roleWords.length === 1 && /[-.\d]/.test(roleWords[0]) && !closeModel(catalog, roleWords[0])) {
      // A model name the catalog does not know yet: accepted, with a warning.
      model = roleWords[0]
      offCatalogModel = roleWords[0]
      read.push({ word: roleWords[0], as: `model ${roleWords[0]}, not in the catalog` })
    } else {
      const close = roleWords.length === 1 ? closeModel(catalog, roleWords[0]) : null
      if (close) {
        issues.push({ text: `No model “${roleWords[0]}”. Did you mean ${close}?`, blocking: true })
        guess = { word: roleWords[0], model: close }
      } else {
        issues.push({ text: `“${query}” is not a model, effort or role.`, blocking: true })
      }
    }
  }
  const alternatives: Parsed['alternatives'] = []
  // The harness follows the model when only the model was typed.
  let nextHarness = harness || (model && !offCatalogModel ? harnessForModel(catalog, model) || base.harness : base.harness)
  let nextModel = model ?? (nextHarness === base.harness ? base.model : firstModel(catalog, nextHarness))
  if (model && !offCatalogModel && harness && harnessForModel(catalog, model) !== harness) {
    const home = harnessForModel(catalog, model) as string
    issues.push({ text: `${model} runs on ${harnessName(home)}, not ${harnessName(harness)}.`, blocking: true })
    alternatives.push({ label: `${harnessName(home)} · ${model}`, staffing: { ...base, harness: home, model, effort: clampEffort(catalog, home, model, base.effort) }, effortTyped: false, roleTouched: false, harnessTyped: false })
    alternatives.push({ label: `${harnessName(harness)} · ${modelWords(firstModel(catalog, harness))}`, staffing: { ...base, harness, model: firstModel(catalog, harness), effort: clampEffort(catalog, harness, firstModel(catalog, harness), base.effort) }, effortTyped: false, roleTouched: false, harnessTyped: false })
    nextHarness = harness
    nextModel = firstModel(catalog, harness)
  }
  const nextRole = role === undefined ? base.role : role ? role.id : ''
  let nextEffort = effort || base.effort
  if (effort) {
    const refusal = effortRefusal(catalog, nextHarness, nextModel, effort)
    if (refusal) {
      issues.push({ text: `${refusal}.`, blocking: true })
      const clamped = clampEffort(catalog, nextHarness, nextModel, effort)
      alternatives.push({ label: `${harnessName(nextHarness)} · ${modelWords(nextModel)} · ${clamped}`, staffing: { role: nextRole, harness: nextHarness, model: nextModel, effort: clamped }, effortTyped: true, roleTouched: role !== undefined, harnessTyped: false })
      const other = catalog.harnesses.find(entry => entry.id !== nextHarness && entry.efforts.includes(effort))
      if (other) alternatives.push({ label: `${harnessName(other.id)} · ${modelWords(firstModel(catalog, other.id))} · ${effort}`, staffing: { role: nextRole, harness: other.id, model: firstModel(catalog, other.id), effort }, effortTyped: true, roleTouched: role !== undefined, harnessTyped: false })
    }
  } else {
    nextEffort = clampEffort(catalog, nextHarness, nextModel, nextEffort)
  }
  if (guess) {
    const fixed = parseWords(catalog, words.map(word => (word === guess?.word ? guess.model : word)).join(' '), base)
    alternatives.unshift({ label: guess.model, staffing: fixed.result, effortTyped: fixed.effortTyped, roleTouched: fixed.roleTouched, harnessTyped: fixed.harnessTyped })
  }
  return {
    result: { role: nextRole, harness: nextHarness, model: nextModel, effort: nextEffort },
    read, issues, alternatives, ambiguous, offCatalog: offCatalogModel,
    roleTouched: role !== undefined, effortTyped: Boolean(effort), harnessTyped: Boolean(harness) && !model,
  }
}

/**
 * Typed words applied through the same rules as a pick or a drop: the model
 * and harness first, then an effort typed by hand, then the role through
 * withRole, so a role lands the same way on every path.
 */
export function applyParsed(catalog: StaffingCatalog, context: SlotContext, base: Staffing, parsed: Pick<Parsed, 'result' | 'roleTouched' | 'effortTyped' | 'harnessTyped' | 'offCatalog'>, hold: EffortHold): Outcome & { effortTyped: boolean } {
  const wanted = parsed.result
  const notes: string[] = []
  let next: Staffing = { ...base }
  if (wanted.harness !== base.harness || wanted.model !== base.model) {
    const outcome = parsed.offCatalog
      ? withFreeModel(next, wanted.model)
      : withModel(catalog, next, wanted.harness, wanted.model, parsed.harnessTyped)
    next = outcome.next
    if (outcome.note) notes.push(outcome.note)
  }
  if (parsed.effortTyped) {
    const outcome = withEffort(catalog, next, wanted.effort)
    if (outcome.refused) return { next: base, refused: outcome.refused, effortTyped: true }
    next = outcome.next
  }
  let offer: Suggestion | undefined
  if (parsed.roleTouched) {
    const role = wanted.role ? catalog.roles.find(entry => entry.id === wanted.role) || null : null
    // An effort typed with the role is stated: it stays, with the policy offered.
    const outcome = withRole(catalog, context, next, role, parsed.effortTyped ? 'stated' : hold)
    next = outcome.next
    offer = outcome.offer
    if (outcome.note) notes.push(outcome.note)
  }
  return { next, note: notes.join(' ') || undefined, offer, effortTyped: parsed.effortTyped }
}
