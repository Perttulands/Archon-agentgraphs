export function isTextEditingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  // A radio or checkbox just chosen is not text, so view shortcuts still apply.
  if (target instanceof HTMLInputElement) return target.type !== 'radio' && target.type !== 'checkbox'
  return target.tagName === 'TEXTAREA' || target.isContentEditable
}

/** Menus and dialogs above a floating window own Escape, including nested menus. */
export function hasWindowOverlay(): boolean {
  return [...document.querySelectorAll('[role="dialog"], [role="alertdialog"], [role="menu"]')]
    .some(surface => !surface.matches('[data-window-id]'))
}
