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

export function resolveCitation(path: string, artifacts: readonly string[], contextPaths: readonly string[] = []): string | null {
  if (scheme.test(path)) return null
  const relative = path.replace(/^\.\//, '')
  const known = [...new Set(artifacts.filter(candidate => !scheme.test(candidate)))]
  const matches = known.filter(candidate => candidate === relative || (path.startsWith('/')
    ? path.endsWith(`/${candidate}`)
    : candidate.endsWith(`/${relative}`)))
  if (matches.length === 1) return matches[0]
  const context = contextPaths.filter(candidate => !scheme.test(candidate))
  const exact = matches.filter(candidate => context.some(cited => cited === candidate || cited.endsWith(`/${candidate}`)))
  if (exact.length === 1) return exact[0]
  const bases = context.filter(candidate => candidate.startsWith('/')).map(candidate => candidate.slice(0, candidate.lastIndexOf('/') + 1))
  const scoped = matches.filter(candidate => bases.some(base => base.endsWith(`/${candidate.slice(0, candidate.length - relative.length)}`)))
  return scoped.length === 1 ? scoped[0] : null
}
