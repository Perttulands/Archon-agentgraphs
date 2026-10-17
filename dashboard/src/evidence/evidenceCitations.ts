/** File citations use their authored absolute path, or an unambiguous run artifact.
 * Relative names may also be rooted beside an absolute report cited with them.
 * Never guess between attempts that happen to contain the same file name.
 */
export function citedPaths(text: string): string[] {
  return [...new Set((text.match(/(?:\/|(?:\.?\.?\/)?[\w.-]+\/)[^\s`"'<>()[\]{},;|*]+\.[a-zA-Z0-9]{1,10}\b/g) || []).map(path => path.replace(/[.:]+$/, '')))]
}

export function resolveCitation(path: string, artifacts: readonly string[], contextPaths: readonly string[] = []): string | null {
  if (path.startsWith('/')) return path
  const relative = path.replace(/^\.\//, '')
  const exact = contextPaths.filter(candidate => candidate === relative || candidate.endsWith(`/${relative}`))
  if (new Set(exact).size === 1) return exact[0]
  const matches = artifacts.filter(candidate => candidate === relative || candidate.endsWith(`/${relative}`))
  const bases = contextPaths.filter(candidate => candidate.startsWith('/')).map(candidate => candidate.slice(0, candidate.lastIndexOf('/') + 1))
  const scoped = matches.filter(candidate => bases.some(base => base.endsWith(`/${candidate.slice(0, candidate.length - relative.length)}`)))
  if (new Set(scoped).size === 1) return scoped[0]
  if (new Set(matches).size === 1) return matches[0]
  // A report's sibling citation remains readable even when the bounded artifact
  // listing omitted it. Several different report directories cannot establish a base.
  const reportBases = [...new Set(contextPaths.filter(candidate => /\.(?:md|txt|json)$/i.test(candidate)).map(candidate => candidate.slice(0, candidate.lastIndexOf('/') + 1)))]
  if (!matches.length && reportBases.length === 1) return `${reportBases[0]}${relative}`
  return null
}
