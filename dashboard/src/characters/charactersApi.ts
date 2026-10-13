/* Saved characters: a named, pictured staffing (role, harness, model, effort)
 * that fills a slot in one pick. A character is a stamp: the slot then owns
 * its values, so editing or deleting one changes no mission (archon-fh5c.7). */
import { useCallback, useEffect, useState } from 'react'
import { fetchApi } from '../components/formationsApi'
import type { Staffing } from '../staffing/staffingModel'

export interface Character {
  id: string
  name: string
  description?: string
  /** A small image as a data URL; absent shows the name's initials. */
  picture?: string
  /** The role; absent is a vanilla agent. */
  agentId?: string
  harness: string
  /** Absent is the harness default model. */
  model?: string
  effort: string
}

export type CharacterDraft = Omit<Character, 'id'> & { id?: string }

export async function fetchCharacters(): Promise<Character[]> {
  return (await fetchApi<{ characters: Character[] }>('/api/characters')).data.characters || []
}

/** Saves a new character, or states an existing one (one with an id) in full. */
export async function saveCharacter(draft: CharacterDraft): Promise<Character> {
  const body = JSON.stringify(draft)
  return draft.id
    ? (await fetchApi<Character>(`/api/characters/${encodeURIComponent(draft.id)}`, { method: 'PUT', body })).data
    : (await fetchApi<Character>('/api/characters', { method: 'POST', body })).data
}

export async function deleteCharacter(id: string): Promise<void> {
  await fetchApi(`/api/characters/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** What a slot runs once the character is stamped onto it. */
export function characterStaffing(character: Character): Staffing {
  return { role: character.agentId || '', harness: character.harness, model: character.model || '', effort: character.effort }
}

/** The characters the daemon holds, read once and again on reload; a failed read leaves the list as it was. */
export function useCharacters(): { characters: Character[]; reload: () => Promise<void> } {
  const [characters, setCharacters] = useState<Character[]>([])
  const reload = useCallback(async () => {
    try {
      setCharacters(await fetchCharacters())
    } catch {
      // The roster and staffing work without characters.
    }
  }, [])
  useEffect(() => { void reload() }, [reload])
  return { characters, reload }
}

const PICTURE_SIDE = 96

/** A chosen image, cropped square and scaled to the size a character's picture is kept at. */
export async function pictureFromFile(file: File): Promise<string> {
  const bitmap = await createImageBitmap(file)
  const side = Math.min(bitmap.width, bitmap.height)
  const canvas = document.createElement('canvas')
  canvas.width = PICTURE_SIDE
  canvas.height = PICTURE_SIDE
  const context = canvas.getContext('2d')
  if (!context) throw new Error('This browser cannot read the picture')
  context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, PICTURE_SIDE, PICTURE_SIDE)
  bitmap.close()
  const webp = canvas.toDataURL('image/webp', 0.86)
  return webp.startsWith('data:image/webp') ? webp : canvas.toDataURL('image/jpeg', 0.86)
}
