# Archon design language

Every cockpit surface draws from this language. It extends CHROTE's design
system and records what Archon adds. Detail-driven design work
(`detail-driven-design`) briefs from it, and only the arc's orchestrator
changes it; a variant that needs more proposes the addition in its set.

## Where it lives

- **Tokens and doctrine** are CHROTE's: `DESIGN-SYSTEM.md` in the CHROTE
  repository. JetBrains Mono is the only font. The palette is monochrome plus one
  accent, and colour carries meaning only. Nothing moves under the pointer,
  words come before icons, each surface has one primary action, and nothing
  asks a question in a dialog. Motion survives only where it confirms an
  action. Toasts fade in over 120 ms, hold 1800 ms and fade out over 200 ms.
- **The served theme** applies the tokens: `src/internal/api/theme_default.json`
  and `dashboard/src/theme/theme.ts`.
- **The running contract** for what the cockpit shows and does is
  [CONTRACT.md](CONTRACT.md). This file holds the grammar and words the
  contract does not.

## Reading and editing

- Archon targets big screens. It has no phone layouts.
- Reading and editing happen in one place. Every part of a mission opens where
  it lives, in a floating window, rather than in a fixed side panel or a
  read-only mode.
- Notes are how the operator records and communicates ideas; agents answer
  there, or explain a step in plain terms.
- A run's result is the work it produced. Its outputs and files open at the
  operator's fingertips through the file viewers ported from CHROTE, and a
  gate's rubric opens from the gate. Output folders lead with their nested
  videos and contact sheets. Cited images open from thumbnails; child windows
  open beside their source window. A single small model opens on the first click.
- The Flow view shows sequence, feeds and gating as a line; the canvas shows
  the graph.

## One name per thing

Use these words in the cockpit, the CLI, the skill and notes. The data names
in parentheses appear only in JSON and TOML.

- **Mission**: the reusable graph. Never "board".
- **Input card**: the mission's entry. **Input**: a value each run supplies.
- **Formation**: a unit of work on the canvas. In sentences for the operator,
  "step" is the same thing ("this step"); never a third word.
- **Slot**: a position in a formation. It shows its harness, model and effort.
  **Role**: optional generic role text on a slot (persona card in storage).
  **Seat**: a slot's running agent session.
- **Gate**, **End node**, **judge chain**, **pushback edge**, **run**: as the
  contract defines them.
- **Note**: a thread on the mission or on one node. It holds the operator's
  intent and the conversation about it. The operator writes notes and never
  has to write a brief.
- **Brief**: what a formation's seats are told. The managing agent writes it
  from the notes; the operator may edit it.
- **Question**: what a human gate asks the operator (a request in data).
  **Answer**: the operator's verdict at that gate, Approve or Send back, with
  a response.
- **The operator** is `human:ui` in data. **The agent** is the managing agent,
  the session the operator works with through the CLI; it is never a seat.

## Interaction grammar

Each rule holds on every surface.

- **Click** opens a card's node window beside the card, in the free space
  nearest it (CONTRACT, windows), and selects the card. A click on a wire
  selects the wire.
- **Delete** (or Backspace) with the canvas focused removes the selection
  through the undoable delete. It never acts while a text field has focus.
- **Undo and redo** are visible words in the toolbar with their chords,
  Ctrl+Z and Ctrl+Shift+Z. Each edit that changes the mission is one step.
- **Right-click** opens the object's context menu, a flat sheet of words with
  each action's chord.
- **Drag** starts after a small movement: 6 px for staffing drags, 3 px for
  moving a card. Plain clicks never nudge a card.
- **A drop on a card resolves or explains.** A wire or agent dropped on a
  card goes to its obvious target (the card's free input, its first empty
  slot). When no target fits, a short reason appears at the drop point for the
  toast's duration. Nothing is dropped silently.
- **Creating keeps every keystroke.** A create gesture puts the caret where
  typing goes, with any placeholder selected, so the first key typed after
  the gesture lands.
- **A new node appears where it was asked for**, at the pointer or the drop
  point, nudged only as far as needed to clear its neighbours, and in view.
- **Esc** closes the topmost open surface.
- **Destructive actions** confirm in place: the control reads its own
  confirmation until a second press within three seconds.

## The agent and the operator

The operator steers a mission through its managing agent
([PRODUCT.md](PRODUCT.md)); the cockpit shows what both see.

- Where an entry or edit is marked as the agent's, the mark is a word or weight
  before it is a colour; a new colour is an addition to the colour rule.
- Nothing the operator is reading moves when something is added; motion only
  confirms.
- Drafts save with blanks; only validation and admission reject gaps.
