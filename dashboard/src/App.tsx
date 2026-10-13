import { useState } from 'react'
import AgentsView from './components/AgentsView'
import FormationsCockpit from './components/FormationsCockpit'
import { buildLabel, useDaemonBuild } from './components/daemonBuild'
import { useTheme } from './theme/ThemeContext'

type View = 'formations' | 'agents'

export default function App() {
  const [view, setView] = useState<View>('formations')
  const { error } = useTheme()
  const build = useDaemonBuild()
  const [topbarSlot, setTopbarSlot] = useState<HTMLElement | null>(null)

  return (
    <main className="formations-app">
      <nav className="formations-app-nav" aria-label="Archon views">
        <span className="archon-brand">ARCHON</span>
        <button type="button" onClick={() => setView('formations')} aria-pressed={view === 'formations'}>
          Missions
        </button>
        <button type="button" onClick={() => setView('agents')} aria-pressed={view === 'agents'}>
          Agents
        </button>
        <div className="app-topbar-slot" ref={setTopbarSlot} />
        {build ? <span className="archon-build" title={`The daemon runs Archon ${build.version} (${build.commit})`}>{buildLabel(build)}</span> : null}
      </nav>
      {error ? <div className="theme-error" role="status">{error}</div> : null}
      <section className="formations-app-content">
        {view === 'formations' ? <FormationsCockpit topbarSlot={topbarSlot} /> : <AgentsView />}
      </section>
    </main>
  )
}
