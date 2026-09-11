let ready: Promise<void> | undefined

/** Resolve before mounting the canvas or opening xterm, which measure text. */
export function fontsReady(): Promise<void> {
  return ready ??= (document.fonts
    ? Promise.all([
      document.fonts.load('400 13px "JetBrains Mono"'),
      document.fonts.load('700 13px "JetBrains Mono"'),
    ]).then(() => undefined)
    : Promise.resolve())
}
