// Archon documentation: style choice, the narrow-screen menu, search, Copy
// on code, and the "On this page" marker. Every page reads fully without it.
(() => {
  const root = document.documentElement

  // ---------------------------------------------------------- style
  const STYLE_KEY = 'archon-docs.style'
  const choices = [...document.querySelectorAll('[data-style-choice]')]
  const showStyle = () => {
    for (const button of choices) button.setAttribute('aria-pressed', String(button.dataset.styleChoice === root.dataset.style))
  }
  for (const button of choices) {
    button.addEventListener('click', () => {
      root.dataset.style = button.dataset.styleChoice
      try { localStorage.setItem(STYLE_KEY, root.dataset.style) } catch { /* private mode keeps it for this page */ }
      showStyle()
    })
  }
  showStyle()

  // ---------------------------------------------------------- menu
  const menu = document.querySelector('.menu-toggle')
  const sidebar = document.getElementById('sidebar')
  const setMenu = open => {
    document.body.classList.toggle('nav-open', open)
    menu.setAttribute('aria-expanded', String(open))
    menu.textContent = open ? 'Close' : 'Menu'
  }
  menu?.addEventListener('click', () => {
    const open = !document.body.classList.contains('nav-open')
    setMenu(open)
    if (open) sidebar.querySelector('[aria-current="page"]')?.focus()
  })
  document.addEventListener('click', event => {
    if (document.body.classList.contains('nav-open') && !sidebar.contains(event.target) && event.target !== menu) setMenu(false)
  })

  // ---------------------------------------------------------- copy
  for (const figure of document.querySelectorAll('figure.code')) {
    const button = figure.querySelector('.code-copy')
    if (!button) continue
    button.hidden = false
    button.addEventListener('click', async () => {
      const text = figure.querySelector('code').innerText
      try {
        await navigator.clipboard.writeText(text)
        button.textContent = 'Copied'
      } catch {
        button.textContent = 'Select and copy'
      }
      setTimeout(() => { button.textContent = 'Copy' }, 1800)
    })
  }

  // ---------------------------------------------------------- on this page
  const tocLinks = [...document.querySelectorAll('.toc a')]
  if (tocLinks.length && 'IntersectionObserver' in window) {
    const targets = tocLinks.map(link => document.getElementById(link.hash.slice(1))).filter(Boolean)
    const visible = new Set()
    const mark = () => {
      const current = targets.find(t => visible.has(t)) ?? null
      if (!current) return
      for (const link of tocLinks) link.setAttribute('aria-current', String(link.hash.slice(1) === current.id))
    }
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) entry.isIntersecting ? visible.add(entry.target) : visible.delete(entry.target)
      mark()
    }, { rootMargin: '-64px 0px -65% 0px' })
    targets.forEach(t => observer.observe(t))
  }

  // ---------------------------------------------------------- search
  const input = document.getElementById('search-input')
  const list = document.getElementById('search-results')
  if (!input || !list) return
  let results = []
  let active = -1

  const escape = text => text.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
  const terms = query => query.toLowerCase().split(/\s+/).filter(t => t.length > 1 || /\w/.test(t)).slice(0, 6)

  function score(entry, words) {
    const title = entry.t.toLowerCase()
    const heading = entry.h.toLowerCase()
    const text = entry.x.toLowerCase()
    let total = 0
    for (const word of words) {
      let found = 0
      if (heading.includes(word)) found += heading.startsWith(word) ? 12 : 8
      if (title.includes(word)) found += 5
      if (text.includes(word)) found += 1 + Math.min(3, text.split(word).length - 2)
      if (!found) return 0
      total += found
    }
    return total
  }

  function snippet(text, words) {
    const lower = text.toLowerCase()
    const at = Math.max(0, Math.min(...words.map(w => { const i = lower.indexOf(w); return i < 0 ? Infinity : i })) - 40)
    let cut = text.slice(at, at + 150)
    if (at > 0) cut = '…' + cut
    if (at + 150 < text.length) cut += '…'
    let html = escape(cut)
    for (const word of words) {
      html = html.replace(new RegExp(`(${word.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi'), '<mark>$1</mark>')
    }
    return html
  }

  function render(query) {
    const words = terms(query)
    const index = window.ARCHON_SEARCH ?? []
    results = words.length
      ? index.map(entry => ({ entry, s: score(entry, words) })).filter(r => r.s > 0).sort((a, b) => b.s - a.s).slice(0, 12)
      : []
    active = results.length ? 0 : -1
    if (!words.length) {
      list.hidden = true
      input.setAttribute('aria-expanded', 'false')
      return
    }
    list.innerHTML = results.length
      ? results.map(({ entry }, i) => `<li role="option" id="result-${i}" aria-selected="${i === active}"><a href="${entry.u}" tabindex="-1"><span class="result-title">${escape(entry.h || entry.t)}</span><span class="result-where">${escape(entry.s)} · ${escape(entry.t)}</span><span class="result-text">${snippet(entry.x, words)}</span></a></li>`).join('')
      : `<li class="search-empty" role="option" aria-disabled="true">Nothing matches “${escape(query)}”.</li>`
    list.hidden = false
    input.setAttribute('aria-expanded', 'true')
    input.setAttribute('aria-activedescendant', active >= 0 ? `result-${active}` : '')
  }

  function move(step) {
    if (!results.length) return
    active = (active + step + results.length) % results.length
    list.querySelectorAll('[role="option"]').forEach((li, i) => li.setAttribute('aria-selected', String(i === active)))
    input.setAttribute('aria-activedescendant', `result-${active}`)
    list.querySelector(`#result-${active}`)?.scrollIntoView({ block: 'nearest' })
  }

  const close = () => {
    list.hidden = true
    input.setAttribute('aria-expanded', 'false')
  }

  input.addEventListener('input', () => render(input.value))
  input.addEventListener('focus', () => { if (input.value) render(input.value) })
  input.addEventListener('keydown', event => {
    if (event.key === 'ArrowDown') { event.preventDefault(); move(1) }
    else if (event.key === 'ArrowUp') { event.preventDefault(); move(-1) }
    else if (event.key === 'Enter' && active >= 0) { event.preventDefault(); location.href = results[active].entry.u; close() }
    else if (event.key === 'Escape') { if (!list.hidden) close(); else { input.value = ''; input.blur() } }
  })
  document.addEventListener('click', event => { if (!event.target.closest('.search')) close() })
  document.addEventListener('keydown', event => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName) || document.activeElement?.isContentEditable
    if (event.key === '/' && !typing && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault()
      input.focus()
      input.select()
    } else if (event.key === 'Escape' && document.body.classList.contains('nav-open')) {
      setMenu(false)
      menu.focus()
    }
  })
})()
