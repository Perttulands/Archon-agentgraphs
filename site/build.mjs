#!/usr/bin/env node
// Builds the Archon documentation site into site/dist with no dependencies
// beyond Node 20.19+. Pages are HTML fragments in src/pages; this script wraps
// them in the shared layout, numbers their headings, writes the search index
// and refuses to finish while any internal or repo: link points nowhere.
//
//   node site/build.mjs            build into site/dist
//   node site/build.mjs --out DIR  build elsewhere
//   node site/build.mjs --archon /path/to/archon
//                                  also run `archon mission validate` on every
//                                  code block marked validate="mission"

import { readFileSync, writeFileSync, mkdirSync, rmSync, cpSync, existsSync, readdirSync, statSync } from 'node:fs'
import { basename, dirname, join, resolve, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync, spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import { chmodSync, mkdtempSync, renameSync } from 'node:fs'
import config from './site.config.mjs'

const siteDir = dirname(fileURLToPath(import.meta.url))
const repoDir = resolve(siteDir, '..')
const srcDir = join(siteDir, 'src')
const outArg = process.argv.indexOf('--out')
if (outArg > 0 && !process.argv[outArg + 1]) {
  console.error('--out needs a directory')
  process.exit(2)
}
const finalDir = outArg > 0 ? resolve(process.argv[outArg + 1] ?? '') : join(siteDir, 'dist')

// The output directory is replaced whole, so it must be one this script made.
// Refuse the checkout, anything containing it, the site's sources, and any
// non-empty directory without the marker a previous build leaves.
const BUILD_MARKER = '.archon-docs-build'
function refuseOut(reason) {
  console.error(`Refusing --out ${finalDir}: ${reason}`)
  process.exit(2)
}
const within = (child, parent) => child === parent || child.startsWith(parent.endsWith(sep) ? parent : parent + sep)
if (within(repoDir, finalDir)) refuseOut('it is or contains the repository')
if (within(finalDir, srcDir) || finalDir === siteDir) refuseOut('it is inside the site sources')
if (existsSync(finalDir)) {
  if (!statSync(finalDir).isDirectory()) refuseOut('it is not a directory')
  if (readdirSync(finalDir).length && !existsSync(join(finalDir, BUILD_MARKER))) refuseOut(`it is a non-empty directory that no earlier site build made (no ${BUILD_MARKER})`)
}

// Build beside the destination and swap it in only when everything passed, so
// a failed build leaves the previous one untouched.
mkdirSync(dirname(finalDir), { recursive: true })
const outDir = mkdtempSync(join(dirname(finalDir), `.${basename(finalDir)}.building-`))
chmodSync(outDir, 0o755) // mkdtemp makes 0700; a static server needs to read it
let swapped = false
process.on('exit', () => { if (!swapped) rmSync(outDir, { recursive: true, force: true }) })

const archonArg = process.argv.indexOf('--archon')
const archonBin = archonArg > 0 ? resolve(process.argv[archonArg + 1]) : null
const validations = []

const problems = []
const problem = (page, message) => problems.push(`${page}: ${message}`)

// ---------------------------------------------------------------- facts

const version = readFileSync(join(repoDir, 'VERSION'), 'utf8').trim()
let commit = 'unknown'
try {
  commit = execFileSync('git', ['-C', repoDir, 'rev-parse', '--short', 'HEAD'], { encoding: 'utf8' }).trim()
} catch { /* a tarball without .git still builds */ }

const tokens = {
  version,
  commit,
  'release.tag': config.release.tag,
  'release.date': config.release.date,
  'release.url': config.release.url,
  repo: config.repoUrl,
}

// ---------------------------------------------------------------- helpers

const escapeHtml = text => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const escapeAttr = text => escapeHtml(text).replace(/"/g, '&quot;')
const stripTags = html => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&amp;/g, '&')

export function slugify(text) {
  return stripTags(text).toLowerCase().trim()
    .replace(/[^a-z0-9 _-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
}

// GitHub's heading anchors: lowercase, drop punctuation but keep hyphens and
// underscores, spaces become hyphens, repeats get -1, -2.
function githubAnchors(markdown) {
  const seen = new Map()
  const anchors = new Set()
  let fenced = false
  for (const line of markdown.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) fenced = !fenced
    if (fenced) continue
    const match = /^#{1,6}\s+(.*?)\s*#*\s*$/.exec(line)
    if (!match) continue
    let slug = match[1].replace(/`/g, '').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').toLowerCase()
      .replace(/[^\p{L}\p{N} _-]/gu, '').replace(/ /g, '-')
    const count = seen.get(slug) ?? 0
    seen.set(slug, count + 1)
    if (count) slug = `${slug}-${count}`
    anchors.add(slug)
  }
  return anchors
}

const repoAnchorCache = new Map()
function checkRepoLink(page, target) {
  const [path, anchor] = target.split('#')
  const full = join(repoDir, path)
  if (!existsSync(full)) {
    problem(page, `repo:${target} names a file that does not exist`)
    return
  }
  if (!anchor) return
  if (!path.endsWith('.md')) {
    problem(page, `repo:${target} has an anchor on a file that is not Markdown`)
    return
  }
  if (!repoAnchorCache.has(path)) repoAnchorCache.set(path, githubAnchors(readFileSync(full, 'utf8')))
  if (!repoAnchorCache.get(path).has(anchor)) problem(page, `repo:${target}: ${path} has no heading #${anchor}`)
}

function repoHref(target) {
  const [path] = target.split('#')
  const full = join(repoDir, path)
  const kind = existsSync(full) && statSync(full).isDirectory() ? 'tree' : 'blob'
  return `${config.repoUrl}/${kind}/${config.repoBranch}/${target}`
}

// ---------------------------------------------------------------- code

// Brightness, not hue, sets comments apart: colour carries meaning only.
function highlight(code, lang) {
  const escaped = escapeHtml(code)
  if (lang === 'bash' || lang === 'sh' || lang === 'toml' || lang === 'text' || lang === 'yaml') {
    return escaped.split('\n').map(line => {
      if (lang === 'text') return line
      const comment = /^(\s*)(#.*)$/.exec(line)
      if (comment) return `${comment[1]}<span class="c">${comment[2]}</span>`
      const trailing = /^(.*?\s)(# [^"']*)$/.exec(line)
      if (trailing && lang !== 'text') return `${trailing[1]}<span class="c">${trailing[2]}</span>`
      return line
    }).join('\n')
  }
  return escaped
}

function dedent(text) {
  const lines = text.replace(/^\n/, '').replace(/\n\s*$/, '').split('\n')
  const indent = Math.min(...lines.filter(l => l.trim()).map(l => /^ */.exec(l)[0].length))
  return lines.map(l => l.slice(indent)).join('\n')
}

// <code-block lang="bash" title="...">raw text</code-block> becomes a figure
// with a label and a Copy button. Its body is raw: write < and & as they are.
function renderCodeBlock(attrs, body) {
  const lang = /lang="([^"]*)"/.exec(attrs)?.[1] ?? 'text'
  const title = /title="([^"]*)"/.exec(attrs)?.[1]
  const validate = /validate="([^"]*)"/.exec(attrs)?.[1]
  const code = dedent(body)
  const label = title ?? { bash: 'Shell', sh: 'Shell', toml: 'TOML', text: 'Text', json: 'JSON', yaml: 'YAML', ini: 'Unit file' }[lang] ?? lang
  const dataValidate = validate ? ` data-validate="${escapeAttr(validate)}"` : ''
  if (validate === 'mission') validations.push(code)
  return `<figure class="code"${dataValidate}><figcaption><span class="code-label">${escapeHtml(label)}</span><button type="button" class="kit-button code-copy" hidden>Copy</button></figcaption><pre tabindex="0"><code class="lang-${escapeAttr(lang)}">${highlight(code, lang)}</code></pre></figure>`
}

// ---------------------------------------------------------------- generated reference

function cliReference() {
  const path = join(srcDir, 'generated', 'cli-help.txt')
  const text = readFileSync(path, 'utf8')
  const sections = text.split(/^=== /m).filter(Boolean)
  let html = ''
  for (const section of sections) {
    const newline = section.indexOf('\n')
    const noun = section.slice(0, newline).trim()
    const body = section.slice(newline + 1)
    const [nounHelp, ...commands] = body.split(/^--- /m)
    if (noun === 'archon') {
      html += `<h2 id="cli-archon">archon</h2>\n${renderCodeBlock('lang="text" title="archon -h"', nounHelp)}\n`
      continue
    }
    const summary = (/\n\n(.*?)\n\n/s.exec(nounHelp)?.[1] ?? '').trim()
    html += `<h2 id="cli-${noun}">archon ${noun}</h2>\n<p>${escapeHtml(summary)}</p>\n`
    html += renderCodeBlock(`lang="text" title="archon ${noun} -h"`, nounHelp)
    if (commands.length) {
      html += `<details class="cli-commands"><summary>Every <code>archon ${noun}</code> command with its flags (${commands.length})</summary>\n`
      for (const command of commands) {
        const nl = command.indexOf('\n')
        const name = command.slice(0, nl).trim()
        html += `<h3 id="cli-${noun}-${slugify(name)}" data-toc="no">${escapeHtml(`archon ${noun} ${name}`)}</h3>\n${renderCodeBlock(`lang="text" title="archon ${noun} ${name} -h"`, command.slice(nl + 1))}\n`
      }
      html += '</details>\n'
    }
  }
  return html
}

function daemonFlags() {
  const text = readFileSync(join(srcDir, 'generated', 'archond-help.txt'), 'utf8')
  const rows = []
  const lines = text.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const flag = /^\s+-(\S+)(?:\s+(\S+))?$/.exec(lines[i])
    if (!flag) continue
    const help = []
    while (i + 1 < lines.length && /^ {4}\t/.test(lines[i + 1])) {
      help.push(lines[++i].trim())
    }
    rows.push(`<tr><th scope="row"><code>--${escapeHtml(flag[1])}</code></th><td>${flag[2] ? `<code>${escapeHtml(flag[2])}</code>` : ''}</td><td>${escapeHtml(help.join(' '))}</td></tr>`)
  }
  return `<div class="table-wrap"><table class="flags"><caption class="visually-hidden">archond flags</caption><thead><tr><th scope="col">Flag</th><th scope="col">Value</th><th scope="col">What it sets</th></tr></thead><tbody>${rows.join('')}</tbody></table></div>`
}

// ---------------------------------------------------------------- pages

function parsePage(slug) {
  const raw = readFileSync(join(srcDir, 'pages', `${slug}.html`), 'utf8')
  const front = /^<!--\n([\s\S]*?)\n-->\n/.exec(raw)
  if (!front) throw new Error(`${slug}.html: missing the <!-- key: value --> header`)
  const meta = {}
  for (const line of front[1].split('\n')) {
    const match = /^(\w+):\s*(.*)$/.exec(line)
    if (match) meta[match[1]] = match[2]
  }
  if (!meta.title) throw new Error(`${slug}.html: header has no title`)
  return { slug, meta, body: raw.slice(front[0].length) }
}

const order = config.nav.flatMap(group => group.pages.map(slug => ({ slug, section: group.section })))
const pages = order.map(({ slug, section }) => ({ ...parsePage(slug), section }))
for (const file of readdirSync(join(srcDir, 'pages'))) {
  const slug = file.replace(/\.html$/, '')
  if (!order.some(p => p.slug === slug) && slug !== '404') problem(`${slug}.html`, 'is not in site.config.mjs nav')
}

function transform(page) {
  let html = page.body
  // Generated blocks first, so they flow through the same heading pass.
  html = html.replace('{{cli-reference}}', () => cliReference())
  html = html.replace('{{archond-flags}}', () => daemonFlags())
  // {{version}} and friends; any other {{word}} is prose, such as the
  // {{name}} escape a brief uses, and stays as written.
  html = html.replace(/\{\{([\w.]+)\}\}/g, (whole, key) => key in tokens ? escapeHtml(tokens[key]) : whole)
  // Protect code blocks from the inline passes.
  const blocks = []
  html = html.replace(/<code-block([^>]*)>([\s\S]*?)<\/code-block>/g, (_, attrs, body) => {
    blocks.push(renderCodeBlock(attrs, body))
    return `\u0000${blocks.length - 1}\u0000`
  })
  // Pre-rendered figures (generated reference) are protected too.
  html = html.replace(/<figure class="code"[\s\S]*?<\/figure>/g, figure => {
    blocks.push(figure)
    return `\u0000${blocks.length - 1}\u0000`
  })
  // Scrollable tables and diagrams must be reachable by keyboard: each wrapper
  // is a focusable, labelled region (named by the table's column headings or
  // the diagram's title).
  html = html.replace(/<div class="table-wrap">(\s*<table[^>]*>[\s\S]*?<\/table>)/g, (_, table) => {
    const heads = [...(/<thead>([\s\S]*?)<\/thead>/.exec(table)?.[1] ?? '').matchAll(/<th[^>]*>([\s\S]*?)<\/th>/g)].map(m => stripTags(m[1]).replace(/`/g, '').trim()).filter(Boolean)
    const firstRow = stripTags(/<tbody>\s*<tr>\s*<th[^>]*>([\s\S]*?)<\/th>/.exec(table)?.[1] ?? '').replace(/`/g, '').trim()
    const label = heads.length ? `Table: ${heads.join(', ')}` : `Table starting ${firstRow}`
    return `<div class="table-wrap" tabindex="0" role="region" aria-label="${escapeAttr(label)}">${table}`
  })
  html = html.replace(/(<figure class="diagram">\s*)(<svg[\s\S]*?<\/svg>)/g, (_, open, svg) => {
    const title = stripTags(/<title[^>]*>([\s\S]*?)<\/title>/.exec(svg)?.[1] ?? 'Diagram')
    return `${open}<div class="diagram-scroll" tabindex="0" role="region" aria-label="${escapeAttr(`${title} (scrolls sideways on narrow screens)`)}">${svg}</div>`
  })
  // `inline code` with raw < and &.
  html = html.replace(/`([^`\n]+)`/g, (_, code) => `<code>${escapeHtml(code)}</code>`)
  // repo: links become GitHub links, checked against this checkout.
  html = html.replace(/href="repo:([^"]+)"/g, (_, target) => {
    checkRepoLink(page.slug, target)
    return `href="${escapeAttr(repoHref(target))}"`
  })
  // Headings: ids and a self-link.
  const toc = []
  const used = new Set()
  html = html.replace(/<h([23])((?:\s[^>]*)?)>([\s\S]*?)<\/h\1>/g, (_, level, attrs, inner) => {
    let id = /\sid="([^"]+)"/.exec(attrs)?.[1]
    if (!id) {
      id = slugify(inner) || `section-${toc.length + 1}`
      let n = 2
      while (used.has(id)) id = `${slugify(inner)}-${n++}`
      attrs = `${attrs} id="${id}"`
    }
    if (used.has(id)) problem(page.slug, `duplicate heading id #${id}`)
    used.add(id)
    if (!/data-toc="no"/.test(attrs)) toc.push({ level: Number(level), id, text: stripTags(inner) })
    return `<h${level}${attrs}><a class="anchor" href="#${id}" aria-label="Link to this section: ${escapeAttr(stripTags(inner))}">#</a>${inner}</h${level}>`
  })
  html = html.replace(/\u0000(\d+)\u0000/g, (_, i) => blocks[Number(i)])
  return { html, toc }
}

// ---------------------------------------------------------------- layout

const markSvg = `<svg class="mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false"><rect width="32" height="32" rx="7" class="mark-ground"/><path d="M8 26 16 9l8 17" fill="none" class="mark-stroke" stroke-width="3.8" stroke-linecap="round" stroke-linejoin="round"/><path d="M11.5 20h9" class="mark-stroke" stroke-width="3.2" stroke-linecap="round"/><circle cx="16" cy="8" r="4.2" class="mark-node"/></svg>`

function navHtml(current) {
  return config.nav.map(group => {
    const items = group.pages.map(slug => {
      const page = pages.find(p => p.slug === slug)
      const label = page.meta.nav ?? page.meta.title
      const here = slug === current ? ' aria-current="page"' : ''
      return `<li><a class="kit-row" href="${slug}.html"${here}>${escapeHtml(label)}</a></li>`
    }).join('')
    return `<li class="nav-group"><p class="kit-label" id="nav-${slugify(group.section)}">${escapeHtml(group.section)}</p><ul aria-labelledby="nav-${slugify(group.section)}">${items}</ul></li>`
  }).join('')
}

function tocHtml(toc) {
  if (toc.length < 2) return ''
  const items = toc.map(item => `<li class="toc-${item.level}"><a href="#${item.id}">${escapeHtml(item.text)}</a></li>`).join('')
  return `<nav class="toc" aria-labelledby="toc-label"><p class="kit-label" id="toc-label">On this page</p><ol>${items}</ol></nav>`
}

function pagerHtml(index) {
  const prev = pages[index - 1]
  const next = pages[index + 1]
  const link = (page, rel, word) => page
    ? `<a class="pager-${rel}" href="${page.slug}.html" rel="${rel}"><span class="kit-label">${word}</span><span class="pager-title">${escapeHtml(page.meta.nav ?? page.meta.title)}</span></a>`
    : '<span></span>'
  return `<nav class="pager" aria-label="Previous and next page">${link(prev, 'prev', 'Previous')}${link(next, 'next', 'Next')}</nav>`
}

function layout({ page, html, toc, index }) {
  const isHome = page.slug === 'index'
  const title = isHome ? `${config.title} documentation` : `${page.meta.title} · ${config.title} docs`
  const description = page.meta.description ?? config.tagline
  const lead = page.meta.lead ? `<p class="lead">${page.meta.lead.replace(/`([^`]+)`/g, (_, c) => `<code>${escapeHtml(c)}</code>`)}</p>` : ''
  return `<!doctype html>
<html lang="en" data-style="console">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeAttr(description)}">
<meta name="color-scheme" content="dark light">
<link rel="icon" href="assets/favicon.svg" type="image/svg+xml">
<link rel="preload" href="assets/fonts/JetBrainsMono-Regular.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="assets/site.css">
<script>try{var s=localStorage.getItem('archon-docs.style');if(s==='paper'||(!s&&matchMedia('(prefers-color-scheme: light)').matches))document.documentElement.dataset.style='paper'}catch(e){}document.documentElement.classList.add('js')</script>
<script src="assets/search-index.js" defer></script>
<script src="assets/site.js" defer></script>
</head>
<body class="page-${page.slug}">
<a class="skip" href="#main">Skip to content</a>
<header class="topbar">
  <a class="brand" href="index.html" aria-label="Archon documentation home">${markSvg}<span class="brand-name">Archon</span><span class="brand-sub">docs</span></a>
  <button type="button" class="kit-button menu-toggle" aria-expanded="false" aria-controls="sidebar">Menu</button>
  <div class="search" role="search">
    <label class="visually-hidden" for="search-input">Search the documentation</label>
    <input id="search-input" class="search-field" type="search" placeholder="Search the docs" autocomplete="off" spellcheck="false" aria-controls="search-results" aria-expanded="false" aria-autocomplete="list" role="combobox">
    <kbd class="kit-chord search-chord" aria-hidden="true">/</kbd>
    <ul id="search-results" class="search-results" role="listbox" aria-label="Search results" hidden></ul>
  </div>
  <div class="style-switch" role="group" aria-label="Style">
    <button type="button" class="kit-button" data-style-choice="console" aria-pressed="true">Console</button>
    <button type="button" class="kit-button" data-style-choice="paper" aria-pressed="false">Paper</button>
  </div>
</header>
<div class="edition" role="note">
  <p><strong>Current source, unreleased.</strong> These pages follow commit <code>${escapeHtml(commit)}</code>. The public download is still <a href="${escapeAttr(config.release.url)}">${escapeHtml(config.release.tag)}</a>, which predates missions, End nodes, Limit cards, <code>run wait</code> and Pi. <a href="versions.html">What differs</a></p>
</div>
<div class="frame">
  <nav id="sidebar" class="sidebar" aria-label="Documentation">
    <ul class="nav">${navHtml(page.slug)}</ul>
  </nav>
  <main id="main" class="main" tabindex="-1">
    <article class="doc">
      ${isHome ? '' : `<p class="crumb kit-label">${escapeHtml(page.section)}</p>`}
      <h1>${escapeHtml(page.meta.heading ?? page.meta.title)}</h1>
      ${lead}
      ${html}
      <footer class="doc-foot">
        ${pagerHtml(index)}
        <p class="doc-meta">Built from <code>${escapeHtml(commit)}</code> · VERSION <code>${escapeHtml(version)}</code> · <a href="${escapeAttr(repoHref(`site/src/pages/${page.slug}.html`))}">Page source</a> · <a href="${escapeAttr(config.repoUrl)}">Repository</a> · MIT license</p>
      </footer>
    </article>
  </main>
  <aside class="aside">${tocHtml(toc)}</aside>
</div>
</body>
</html>
`
}

// ---------------------------------------------------------------- build

mkdirSync(join(outDir, 'assets', 'fonts'), { recursive: true })
cpSync(join(srcDir, 'assets'), join(outDir, 'assets'), { recursive: true })
const fontDir = join(repoDir, 'dashboard', 'public', 'fonts')
for (const font of ['JetBrainsMono-Regular.woff2', 'JetBrainsMono-Bold.woff2']) {
  if (!existsSync(join(fontDir, font))) throw new Error(`missing ${relative(repoDir, join(fontDir, font))}: the site uses the cockpit's own font files`)
  cpSync(join(fontDir, font), join(outDir, 'assets', 'fonts', font))
}

const built = new Map()
const searchIndex = []
pages.forEach((page, index) => {
  const { html, toc } = transform(page)
  const doc = layout({ page, html, toc, index })
  built.set(page.slug, doc)
  // Search: the page, then each h2/h3 section with its own text.
  const sections = html.split(/(?=<h[23][\s>])/)
  sections.forEach((chunk, i) => {
    const heading = /<h([23])[^>]*id="([^"]+)"[^>]*>([\s\S]*?)<\/h\1>/.exec(chunk)
    const text = stripTags(chunk.replace(/<a class="anchor"[^>]*>#<\/a>/g, '').replace(/<h[23][\s\S]*?<\/h[23]>/, '')).replace(/\s+/g, ' ').trim()
    if (i === 0 && !heading) {
      searchIndex.push({ u: `${page.slug}.html`, t: page.meta.title, s: page.section, h: '', x: `${page.meta.lead ? stripTags(page.meta.lead) + ' ' : ''}${text}`.slice(0, 900) })
    } else if (heading) {
      searchIndex.push({ u: `${page.slug}.html#${heading[2]}`, t: page.meta.title, s: page.section, h: stripTags(heading[3].replace(/<a class="anchor"[^>]*>#<\/a>/, '')), x: text.slice(0, 2400) })
    }
  })
})

// 404 uses the layout but sits outside the navigation.
if (existsSync(join(srcDir, 'pages', '404.html'))) {
  const page = { ...parsePage('404'), section: '' }
  const { html, toc } = transform(page)
  built.set('404', layout({ page, html, toc, index: -1 }).replace('<nav class="pager"', '<nav hidden class="pager"'))
}

// Every internal link must reach a page, and every #anchor an id on it.
const ids = new Map()
for (const [slug, doc] of built) ids.set(slug, new Set([...doc.matchAll(/\sid="([^"]+)"/g)].map(m => m[1])))
for (const [slug, doc] of built) {
  for (const [, href] of doc.matchAll(/href="([^"]+)"/g)) {
    if (/^(https?:|mailto:)/.test(href) || href.startsWith('assets/')) continue
    const [file, anchor] = href.split('#')
    const target = file === '' ? slug : file.replace(/\.html$/, '')
    if (!ids.has(target)) { problem(slug, `link to ${href}: no such page`); continue }
    if (anchor && !ids.get(target).has(anchor)) problem(slug, `link to ${href}: ${target}.html has no #${anchor}`)
  }
  for (const [, src] of doc.matchAll(/src="([^"]+)"/g)) {
    if (!src.startsWith('http') && src !== 'assets/search-index.js' && !existsSync(join(outDir, src))) problem(slug, `missing asset ${src}`)
  }
}

// Mission examples marked validate="mission" must pass `archon mission validate`.
if (archonBin) {
  for (const code of validations) {
    const slug = /^slug\s*=\s*"([^"]+)"/m.exec(code)?.[1]
    if (!slug) { problem('validate', 'a validate="mission" block has no slug'); continue }
    const workspace = mkdtempSync(join(tmpdir(), 'archon-site-validate-'))
    mkdirSync(join(workspace, '.archon', 'missions'), { recursive: true })
    writeFileSync(join(workspace, '.archon', 'missions', `${slug}.mission.toml`), code + '\n')
    const result = spawnSync(archonBin, ['--workspace', workspace, 'mission', 'validate', slug], { encoding: 'utf8' })
    const errors = /\t(\d+) errors/.exec(result.stdout)?.[1]
    if (result.status !== 0 || errors !== '0') problem('validate', `mission ${slug} does not validate:\n${result.stdout}${result.stderr}`)
    rmSync(workspace, { recursive: true, force: true })
  }
  console.log(`Validated ${validations.length} mission example(s) with ${archonBin}`)
}

if (problems.length) {
  console.error(`Site build found ${problems.length} problem(s):\n  ${problems.join('\n  ')}`)
  process.exit(1)
}
for (const [slug, doc] of built) writeFileSync(join(outDir, `${slug}.html`), doc)
writeFileSync(join(outDir, 'assets', 'search-index.js'), `window.ARCHON_SEARCH=${JSON.stringify(searchIndex)};\n`)
writeFileSync(join(outDir, BUILD_MARKER), `Archon documentation site, built from ${commit}. This directory is replaced by node site/build.mjs.\n`)
if (existsSync(finalDir)) {
  const old = `${outDir}.old`
  renameSync(finalDir, old)
  renameSync(outDir, finalDir)
  rmSync(old, { recursive: true, force: true })
} else {
  renameSync(outDir, finalDir)
}
swapped = true
console.log(`Built ${built.size} pages into ${relative(process.cwd(), finalDir) || '.'} (source ${commit}, VERSION ${version})`)
