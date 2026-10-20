/** Known generated caches stay available in collapsed file groups, outside the
 * primary reading list. This is presentation only, never artifact deletion. */
export function isCacheArtifact(name: string): boolean {
  return name.split('/').some(part => ['.uv-cache', '.mpl', '.cache', '__pycache__'].includes(part))
}
