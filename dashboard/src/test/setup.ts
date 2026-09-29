import '@testing-library/jest-dom'

// Ported from CHROTE dashboard/src/test/setup.ts. jsdom draws nothing: its
// canvas has no context unless a native package is installed, and xterm asks.
HTMLCanvasElement.prototype.getContext = (() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext

// jsdom ships no matchMedia. xterm.js queries it for device pixel ratio the
// moment a terminal opens, so without this every terminal test throws.
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    media: query,
    matches: false,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}
