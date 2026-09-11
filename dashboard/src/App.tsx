import { useState } from 'react'
import AgentsView from './components/AgentsView'
import FormationsCockpit from './components/FormationsCockpit'
import { useTheme } from './theme/ThemeContext'

type View = 'formations' | 'agents'

export default function App() {
  const [view, setView] = useState<View>('formations')
  const { error } = useTheme()

  return (
    <main className="formations-app">
      <nav className="formations-app-nav" aria-label="Formations views">
        <button type="button" onClick={() => setView('formations')} aria-pressed={view === 'formations'}>
          Formations
        </button>
        <button type="button" onClick={() => setView('agents')} aria-pressed={view === 'agents'}>
          Agents
        </button>
      </nav>
      {error ? <div className="theme-error" role="status">{error}</div> : null}
      <section className="formations-app-content">
        {view === 'formations' ? <FormationsCockpit /> : <AgentsView />}
      </section>
    </main>
  )
}
