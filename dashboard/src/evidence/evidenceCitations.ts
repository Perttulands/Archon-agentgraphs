/** Only run artifacts become file citations. Context can disambiguate attempts,
 * but cannot establish that a file exists. URLs are never file citations.
 */
const scheme = /[a-zA-Z][a-zA-Z0-9+.-]*:/

export function citedPaths(text: string): string[] {
  // Remove whole scheme-bearing tokens before looking for paths, so a URL's
  // slash-prefixed tail cannot be mistaken for a local file.
  const local = text.split(/(\s+|[`"'<>()[\]{}])/).map(token => scheme.test(token) ? '' : token).join(' ')
  return [...new Set((local.match(/(?:\/|(?:\.?\.?\/)?[\w.-]+\/)[^\s`"'<>()[\]{},;|*]+\.[a-zA-Z0-9]{1,10}\b/g) || []).map(path => path.replace(/[.:]+$/, '')))]
}

export function resolveCitation(path: string, artifacts: readonly string[], contextPaths: readonly string[] = [], directories: readonly string[] = []): string | null {
  if (scheme.test(path)) return null
  const relative = path.replace(/^\.\//, '')
  const known = [...new Set(artifacts.filter(candidate => !scheme.test(candidate)))]
  if (known.includes(relative)) return relative
  const matches = known.filter(candidate => candidate === relative || (path.startsWith('/')
    ? path.endsWith(`/${candidate}`)
    : candidate.endsWith(`/${relative}`)))
  // Scope before suffix matching: repeated names belong to the current attempt,
  // including files nested under an output folder. Never select another attempt
  // merely because it happens to be the only suffix in a capped run listing.
  const scopedNames = directories.flatMap(directory => {
    const parts = `${directory}/${relative}`.split('/')
    const normalized: string[] = []
    for (const part of parts) {
      if (part === '..') normalized.pop()
      else if (part && part !== '.') normalized.push(part)
    }
    return [normalized.join('/')]
  })
  const scopedMatches = known.filter(candidate => scopedNames.includes(candidate))
  if (scopedMatches.length === 1) return scopedMatches[0]
  if (scopedMatches.length > 1) return null
  if (!path.startsWith('/') && directories.length) {
    // Output snapshots add outputs/<port>/ before the produced folder. A
    // citation may still include that folder's original name (replay/...),
    // so match its suffix only within this attempt and its output folders.
    const inDirectory = matches.filter(candidate => directories.some(directory => candidate.startsWith(`${directory}/`)))
    if (inDirectory.length === 1) return inDirectory[0]
    if (inDirectory.length > 1) return null
  }
  if (matches.length === 1 && (!directories.length || path.startsWith('/'))) return matches[0]
  const context = contextPaths.filter(candidate => !scheme.test(candidate))
  const exact = matches.filter(candidate => context.some(cited => cited === candidate || cited.endsWith(`/${candidate}`)))
  if (exact.length === 1) return exact[0]
  const bases = context.filter(candidate => candidate.startsWith('/')).map(candidate => candidate.slice(0, candidate.lastIndexOf('/') + 1))
  const scoped = matches.filter(candidate => bases.some(base => base.endsWith(`/${candidate.slice(0, candidate.length - relative.length)}`)))
  return scoped.length === 1 ? scoped[0] : null
}
