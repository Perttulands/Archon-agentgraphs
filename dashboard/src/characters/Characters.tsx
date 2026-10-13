/* A character's face, the list of characters in a roster, and the dialog that
 * makes and edits one. The dialog follows the role editor (PersonaEditorDialog). */
import { useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { initials } from '../components/formationsCockpitVisuals'
import { harnessIcon, harnessName } from '../components/harnessIcons'
import { DEFAULT_MODEL_WORDS, type StaffingCatalog } from '../staffing/staffingModel'
import { deleteCharacter, pictureFromFile, saveCharacter, type Character, type CharacterDraft } from './charactersApi'
import './characters.css'

/** The picture, or the name's initials, with the harness marked on its corner. */
export function CharacterFace({ character }: { character: Pick<Character, 'name' | 'picture' | 'harness'> }) {
  return (
    <span className="chface">
      {character.picture ? <img src={character.picture} alt="" /> : <span className="chface-initials">{initials(character.name)}</span>}
      <span className="chface-harness" title={harnessName(character.harness)}>{harnessIcon(character.harness)}</span>
    </span>
  )
}

/** What a character runs, in the words a slot uses. */
export function characterWords(character: Character, roleName: (id: string) => string): string {
  return [character.agentId ? roleName(character.agentId) : 'vanilla', character.model || DEFAULT_MODEL_WORDS, character.effort].join(' · ')
}

export function CharactersGroup({ characters, roleName, selectedId, title, onNew, onPick, onPointerDown }: {
  characters: Character[]
  roleName: (id: string) => string
  selectedId?: string
  /** What a row's tooltip says picking it does. */
  title: (character: Character) => string
  onNew?: (trigger: HTMLElement) => void
  onPick?: (character: Character, trigger: HTMLElement) => void
  /** A row that can be dragged takes the press itself; onPick is then its click. */
  onPointerDown?: (event: ReactPointerEvent<HTMLElement>, character: Character) => void
}) {
  if (!characters.length && !onNew) return null
  return (
    <section className="characters-group" data-provider="characters" data-testid="characters-group">
      <div className="characters-label">
        Characters
        {onNew ? <button type="button" className="characters-new" aria-label="New character" title="New character" onClick={event => onNew(event.currentTarget)}>+</button> : null}
      </div>
      {characters.map(character => (
        <button key={character.id} type="button" className={`ragent character${selectedId === character.id ? ' selected' : ''}`}
          data-testid={`character-${character.id}`} title={title(character)}
          onPointerDown={onPointerDown ? event => onPointerDown(event, character) : undefined}
          onClick={onPointerDown ? undefined : event => onPick?.(character, event.currentTarget)}>
          <CharacterFace character={character} />
          <span className="ri">
            <span className="n">{character.name}</span>
            <span className="r">{characterWords(character, roleName)}</span>
          </span>
        </button>
      ))}
    </section>
  )
}

const blankDraft = (catalog: StaffingCatalog): CharacterDraft => {
  const harness = catalog.harnesses[0]
  return { name: '', description: '', picture: '', agentId: '', harness: harness?.id || '', model: '', effort: harness?.defaultEffort || '' }
}

/** Makes a character, or edits the one given. Saving changes no mission. */
export function CharacterEditorDialog({ character, staffing, catalog, returnFocus, onClose, onSaved }: {
  character: Character | null
  /** What a new character starts from, such as a slot's staffing; the name is still to give. */
  staffing?: Pick<Character, 'agentId' | 'harness' | 'model' | 'effort'>
  catalog: StaffingCatalog
  returnFocus?: HTMLElement | null
  onClose: () => void
  onSaved: () => Promise<void> | void
}) {
  const [draft, setDraft] = useState<CharacterDraft>(() => character ? { ...character } : { ...blankDraft(catalog), ...staffing })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement | null>(null)
  const harness = catalog.harnesses.find(entry => entry.id === draft.harness)
  const efforts = harness?.efforts || []
  const close = () => { onClose(); returnFocus?.focus() }
  const set = (patch: Partial<CharacterDraft>) => { setDraft(current => ({ ...current, ...patch })); setError('') }

  const run = async (work: () => Promise<unknown>, failure: string) => {
    setBusy(true)
    try {
      await work()
      await onSaved()
      close()
    } catch (err) {
      setBusy(false)
      setError(err instanceof Error ? err.message : failure)
    }
  }
  const choosePicture = async (file: File | undefined) => {
    if (!file) return
    try {
      set({ picture: await pictureFromFile(file) })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'The picture could not be read')
    }
  }

  return (
    <div className="pop agent-dialog character-dialog" role="dialog" aria-modal="true" aria-label={character ? `Edit character ${character.name}` : 'New character'}
      data-testid="character-editor" onPointerDown={event => event.stopPropagation()}
      onKeyDown={event => { if (event.key === 'Escape' && !busy) { event.stopPropagation(); close() } }}>
      <form onSubmit={event => { event.preventDefault(); void run(() => saveCharacter(draft), 'The character was not saved') }}>
        <div className="phd">
          <span>{character ? 'Edit character' : 'New character'}</span>
          <button type="button" className="x" aria-label="Close character editor" disabled={busy} onClick={close}>×</button>
        </div>
        <div className="character-dialog-head">
          <CharacterFace character={{ name: draft.name || '?', picture: draft.picture, harness: draft.harness }} />
          <input ref={fileInput} type="file" accept="image/*" hidden aria-label="Character picture file"
            onChange={event => { void choosePicture(event.target.files?.[0]); event.target.value = '' }} />
          <button type="button" onClick={() => fileInput.current?.click()}>{draft.picture ? 'Change picture' : 'Choose picture'}</button>
          {draft.picture ? <button type="button" onClick={() => set({ picture: '' })}>Remove picture</button> : null}
        </div>
        <div className="agent-dialog-fields">
          <label className="wide">
            <span>Name</span>
            <input autoFocus aria-label="Character name" value={draft.name} onChange={event => set({ name: event.target.value })} />
          </label>
          <label className="wide">
            <span>Description</span>
            <input aria-label="Character description" value={draft.description || ''} onChange={event => set({ description: event.target.value })} />
          </label>
          <label>
            <span>Role</span>
            <select aria-label="Character role" value={draft.agentId || ''} onChange={event => set({ agentId: event.target.value })}>
              <option value="">vanilla</option>
              {draft.agentId && !catalog.roles.some(role => role.id === draft.agentId) ? <option value={draft.agentId}>{draft.agentId}</option> : null}
              {catalog.roles.map(role => <option key={role.id} value={role.id}>{role.name}</option>)}
            </select>
          </label>
          <label>
            <span>Harness</span>
            <select aria-label="Character harness" value={draft.harness} onChange={event => {
              const next = catalog.harnesses.find(entry => entry.id === event.target.value)
              set({ harness: event.target.value, model: '', effort: next?.efforts.includes(draft.effort) ? draft.effort : next?.defaultEffort || '' })
            }}>
              {catalog.harnesses.map(entry => <option key={entry.id} value={entry.id}>{harnessName(entry.id)}</option>)}
            </select>
          </label>
          <label>
            <span>Model</span>
            <input aria-label="Character model" list="character-models" placeholder={DEFAULT_MODEL_WORDS} value={draft.model || ''} onChange={event => set({ model: event.target.value.trim() })} />
            <datalist id="character-models">{(harness?.models || []).map(model => <option key={model.id} value={model.id} />)}</datalist>
          </label>
          <label>
            <span>Effort</span>
            <select aria-label="Character effort" value={draft.effort} onChange={event => set({ effort: event.target.value })}>
              {efforts.map(effort => <option key={effort} value={effort}>{effort}</option>)}
            </select>
          </label>
        </div>
        {error ? <div className="dialog-error" role="alert">{error}</div> : null}
        <div className="board-dialog-actions">
          {character ? <button type="button" className="danger" disabled={busy} onClick={() => void run(() => deleteCharacter(character.id), 'The character was not deleted')}>Delete character</button> : null}
          <button type="button" disabled={busy} onClick={close}>Cancel</button>
          <button className="primary" type="submit" aria-label="Save character" disabled={busy || !draft.name.trim() || !draft.harness || !draft.effort}>{busy ? 'Saving…' : 'Save character'}</button>
        </div>
      </form>
    </div>
  )
}
