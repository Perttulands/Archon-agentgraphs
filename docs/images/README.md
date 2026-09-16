# README images

The README and captures show the Archon product and its Boards and Agents views.

## Header

`archon-header.png` is original artwork generated with the built-in OpenAI
image-generation tool on 2026-09-14. The [exact prompt](archon-header-prompt.txt)
uses the UI's charcoal background, blue connections and gold accents as the
visual reference. The architectural arch is brand artwork, not a screenshot
or a diagram of the runtime. The original generated PNG is stored unchanged.

## Screenshots

All captures use Chromium and the real branded UI introduced in `form-pvl.1`,
with the bundled dark theme. The frozen capture build contains
`index-CP2HVN87.js` and `index-B3tsUi9f.css`.
They were captured on 2026-09-14 in an isolated local workspace using the
repository's delivery board, companion notes and built-in personas.
`agents.png` was recaptured on 2026-09-16 after the Agents view adopted the
Boards chrome. It comes from the deployed cockpit (build `index-xpevjQdj.js`
and `index-BgU8gjJ1.css`), in a fresh browser profile with no run selected.
It shows the delivery board and delivery presets, not an operator run.

| File | Capture |
| --- | --- |
| `workflow.png` | The delivery board after Arrange and Fit, at 1440 × 850. |
| `agents.png` | The Agents view filtered to delivery personas, with Delivery Lead selected and every staffing card, including the execution slots, visible, at 1440 × 900. |
| `terminal-peek.png` | The floating Peek panel, captured directly from the browser after scrolling to earlier output, at 760 × 430. |

The terminal capture uses a seeded run with four bound demo shell sessions on
a private tmux socket. The controller shell prints a demo label and runs
`archon --help`; the other tabs identify its demo workers. No Claude or Codex
model runs in this fixture. It demonstrates the actual terminal connection
and rendering, not completed agent work. The Agents capture shows idle personas.

No screenshot pixels were generated, retouched or composited. The captures
contain no private repository briefs, host paths or operator sessions.
