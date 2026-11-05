#!/usr/bin/env node
// Regenerates the CLI and daemon reference from the binaries' own help, so the
// reference cannot drift from the code. Build both from this checkout first:
//
//   (cd src && go build -o /tmp/archon-bin/archon ./cmd/archon && go build -o /tmp/archon-bin/archond ./cmd/archond)
//   node site/tools/gen-reference.mjs --bin /tmp/archon-bin
//
// With --check it writes nothing and exits 1 when the committed reference
// differs from what the binaries print.

import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const siteDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const binArg = process.argv.indexOf('--bin')
if (binArg < 0) {
  console.error('usage: gen-reference.mjs --bin <directory holding archon and archond> [--check]')
  process.exit(2)
}
const bin = resolve(process.argv[binArg + 1])
const check = process.argv.includes('--check')

// Help must not depend on the host: the Codex model list comes from
// $CODEX_HOME/models_cache.json, and ARCHON_* variables change defaults. Run
// the binaries with an empty home and only PATH from this environment, so
// every machine and CI generate the same text.
const emptyHome = mkdtempSync(join(tmpdir(), 'archon-gen-reference-'))
process.on('exit', () => rmSync(emptyHome, { recursive: true, force: true }))
const env = { PATH: process.env.PATH ?? '/usr/bin:/bin', HOME: emptyHome, CODEX_HOME: emptyHome, LANG: 'C' }

// Help goes to stderr or stdout depending on the command; take both.
function help(binary, args) {
  const result = spawnSync(join(bin, binary), args, { encoding: 'utf8', env })
  if (result.error) throw result.error
  return `${result.stdout}${result.stderr}`.replace(/\t/g, '    ')
}

const top = help('archon', ['-h'])
const nouns = [...top.matchAll(/^ {2}(\w+)\s{2,}/gm)].map(m => m[1])
let cli = `=== archon\n${top.trimEnd()}\n`
for (const noun of nouns) {
  const nounHelp = help('archon', [noun, '-h'])
  cli += `=== ${noun}\n${nounHelp.trimEnd()}\n`
  const commands = [...nounHelp.matchAll(/^ {2}([a-z][\w-]*)\b/gm)].map(m => m[1])
  for (const command of [...new Set(commands)]) {
    cli += `--- ${command}\n${help('archon', [noun, command, '-h']).trimEnd()}\n`
  }
}
const daemon = `${help('archond', ['-h']).trimEnd()}\n`

const outputs = [
  [join(siteDir, 'src', 'generated', 'cli-help.txt'), cli],
  [join(siteDir, 'src', 'generated', 'archond-help.txt'), daemon],
]
let stale = false
for (const [path, text] of outputs) {
  let current = ''
  try { current = readFileSync(path, 'utf8') } catch { /* first run */ }
  if (current === text) continue
  stale = true
  if (!check) writeFileSync(path, text)
}
if (check && stale) {
  console.error('The CLI reference is stale: run node site/tools/gen-reference.mjs --bin <dir>')
  process.exit(1)
}
console.log(check ? 'CLI reference matches the binaries.' : `Wrote the CLI reference from ${bin}.`)
