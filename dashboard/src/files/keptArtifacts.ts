/** A kept output supersedes its working copy only within the same attempt
 * and at the same complete relative path. Basenames alone are not identity. */
export function keptArtifacts<T extends { name: string }>(files: readonly T[]): T[] {
  const workingCopies = new Set(files.filter(file => /^[^/]+\/\d+\/outputs\/[^/]+\//.test(file.name))
    .map(file => file.name.replace(/^([^/]+\/\d+\/)outputs\/[^/]+\//, '$1')))
  return files.filter(file => !workingCopies.has(file.name))
}
