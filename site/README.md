# Archon documentation site

A static, dependency-free documentation site for Archon: product, install,
first runs, missions, formations, gates, limits, Pi, operation and reference.
It documents the current source; the release banner on every page says how
it differs from the published v0.1.0.

The canonical docs stay the source of truth: [AGENTS.md](../AGENTS.md),
[docs/PRODUCT.md](../docs/PRODUCT.md), [docs/CONTRACT.md](../docs/CONTRACT.md),
[docs/DESIGN.md](../docs/DESIGN.md) and the [archon skill](../skills/archon/SKILL.md).
The site links to them. When they disagree, fix the site.

## Build and preview

Node 20.19 or newer; nothing to install.

```bash
node site/build.mjs                                  # writes site/dist, fails on any broken link
python3 -m http.server --directory site/dist 8000    # preview at http://127.0.0.1:8000
```

`--out DIR` builds elsewhere. The build happens in a temporary sibling
directory and replaces the output only when it passes, so a failed build
keeps the previous one. `--out` refuses the checkout, any directory
containing it, the site sources, and a non-empty directory that a previous
site build did not make (it leaves a `.archon-docs-build` marker). `--archon /path/to/archon` also runs
`archon mission validate` on every code block marked `validate="mission"`.
The build fails when an internal link or `#anchor` points nowhere, or a
`repo:path#anchor` link names a missing file or Markdown heading.

`site/dist/` is ignored by Git. The output is plain HTML, CSS and JavaScript
with relative links, so any static host can serve it; hosting is not set up.

## Layout

| Path | Holds |
| --- | --- |
| `site.config.mjs` | Title, repository URL, release facts and the navigation. |
| `build.mjs` | The build: layout, headings, code blocks, link checks, search index. |
| `src/pages/*.html` | One HTML fragment per page, headed by a `title`/`nav`/`description`/`lead` comment. |
| `src/assets/` | `site.css` (Console and Paper styles from the cockpit's tokens), `site.js`, the favicon. |
| `src/generated/` | `archon` and `archond` help text that the CLI reference and daemon flags are built from. |
| `tools/gen-reference.mjs` | Regenerates `src/generated/` from built binaries. |

The fonts are copied from `dashboard/public/fonts/` at build time.

## Writing pages

- `<code-block lang="bash" title="...">` holds raw text with a Copy button.
  Write `<` and `&` as they are.
- Backticks in prose become inline code.
- `href="repo:docs/CONTRACT.md#limit-cards"` links to the file on GitHub and is
  checked against this checkout.
- `{{version}}`, `{{commit}}`, `{{release.tag}}`, `{{release.date}}`,
  `{{release.url}}` and `{{repo}}` are filled in. Other `{{...}}` stay as written.
- `{{cli-reference}}` and `{{archond-flags}}` expand from `src/generated/`.

The help is captured with an empty home directory and only `PATH` from the
environment, so host files such as the Codex model cache never reach the
page. After a CLI or daemon flag change, regenerate the references:

```bash
(cd src && go build -o /tmp/archon-bin/archon ./cmd/archon && go build -o /tmp/archon-bin/archond ./cmd/archond)
node site/tools/gen-reference.mjs --bin /tmp/archon-bin           # rewrite
node site/tools/gen-reference.mjs --bin /tmp/archon-bin --check   # fail if stale
```

CI (`docs-site` in `.github/workflows/ci.yml`) builds both commands, runs
`gen-reference.mjs --check` and builds the site with `--archon`, so a stale
reference, a broken link or an invalid mission example fails the push.

When a release is published, update `release` in `site.config.mjs`, the edition
banner in `build.mjs` and `src/pages/versions.html`.
