/**
 * The browser APIs jsdom leaves out and Ark's overlays rely on to open:
 * scrolling, scroll-into-view, and element observation.
 */
const layout = Element.prototype as {
  scrollTo?: (...args: unknown[]) => void
  scrollIntoView?: (...args: unknown[]) => void
}
layout.scrollTo ??= () => {}
layout.scrollIntoView ??= () => {}

const environment = globalThis as { ResizeObserver?: unknown }
environment.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}
