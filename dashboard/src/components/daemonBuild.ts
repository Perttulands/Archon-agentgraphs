import { useEffect, useState } from 'react'
import { fetchApi } from './formationsApi'

// The daemon's build as /healthz reports it, so the operator can see which
// source is running (archon-1ea).
export interface DaemonBuild {
  version: string
  commit: string
}

export async function fetchDaemonBuild(): Promise<DaemonBuild | null> {
  const result = await fetchApi<Partial<DaemonBuild>>('/healthz')
  return result.data.version ? { version: result.data.version, commit: result.data.commit || 'unknown' } : null
}

/** The running daemon's build, or null until it is known or when it cannot be read. */
export function useDaemonBuild(): DaemonBuild | null {
  const [build, setBuild] = useState<DaemonBuild | null>(null)
  useEffect(() => {
    let current = true
    fetchDaemonBuild().then(found => { if (current) setBuild(found) }, () => undefined)
    return () => { current = false }
  }, [])
  return build
}

/** A build label short enough for the top bar: the version and a short commit. */
export function buildLabel(build: DaemonBuild): string {
  return `${build.version} · ${/^[0-9a-f]{12,}$/.test(build.commit) ? build.commit.slice(0, 7) : build.commit}`
}
