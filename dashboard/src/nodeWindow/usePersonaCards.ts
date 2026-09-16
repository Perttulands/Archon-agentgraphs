import { useEffect, useState } from 'react'
import { fetchAgentCard } from '../components/formationsApi'
import type { PersonaCard } from '../components/formationsTypes'

/** The persona cards for the given agents, fetched once each while the caller is open. */
export function usePersonaCards(agentIds: readonly string[]): ReadonlyMap<string, PersonaCard> {
  const [cards, setCards] = useState<ReadonlyMap<string, PersonaCard>>(new Map())
  const wanted = [...new Set(agentIds.filter(Boolean))].sort().join('\n')
  useEffect(() => {
    let current = true
    for (const id of wanted ? wanted.split('\n') : []) {
      fetchAgentCard(id).then(card => {
        if (current) setCards(previous => new Map(previous).set(id, card))
      }, () => undefined)
    }
    return () => { current = false }
  }, [wanted])
  return cards
}
