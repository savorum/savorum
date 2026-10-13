export const MAX_RESIDENT_CANVASES = 4
export const MAX_RESIDENT_PIXELS = 12_000_000
export const MAX_CANVAS_PIXELS = 4_000_000
export const MAX_CANVAS_EDGE = 4096

export interface CanvasSize {
  width: number
  height: number
  scale: number
  pixels: number
}

/**
 * Caps a PDF.js viewport before a backing canvas is allocated. CSS keeps the
 * requested zoom; this scale only reduces raster density when a display-sized
 * canvas would exceed the browser memory budget.
 */
export function constrainCanvas(width: number, height: number): CanvasSize {
  if (!(width > 0) || !(height > 0)) return { width: 1, height: 1, scale: 1, pixels: 1 }
  const edgeScale = Math.min(1, MAX_CANVAS_EDGE / width, MAX_CANVAS_EDGE / height)
  const pixelScale = Math.min(1, Math.sqrt(MAX_CANVAS_PIXELS / (width * height)))
  const scale = Math.min(edgeScale, pixelScale)
  const constrainedWidth = Math.max(1, Math.floor(width * scale))
  const constrainedHeight = Math.max(1, Math.floor(height * scale))
  return {
    width: constrainedWidth,
    height: constrainedHeight,
    scale,
    pixels: constrainedWidth * constrainedHeight,
  }
}

export interface ResidentCanvas {
  number: number
  pixels: number
}

/** Chooses the least useful canvases to release before admitting another. */
export function canvasesToEvict(
  residents: readonly ResidentCanvas[],
  incomingPixels: number,
  wanted: ReadonlySet<number>,
): number[] {
  let count = residents.length
  let pixels = residents.reduce((total, resident) => total + resident.pixels, 0)
  const center =
    wanted.size > 0 ? [...wanted].reduce((total, number) => total + number, 0) / wanted.size : 1
  const candidates = [...residents].sort((left, right) => {
    const leftWanted = wanted.has(left.number) ? 1 : 0
    const rightWanted = wanted.has(right.number) ? 1 : 0
    if (leftWanted !== rightWanted) return leftWanted - rightWanted
    return Math.abs(right.number - center) - Math.abs(left.number - center)
  })
  const evicted: number[] = []
  for (const candidate of candidates) {
    if (count < MAX_RESIDENT_CANVASES && pixels + incomingPixels <= MAX_RESIDENT_PIXELS) break
    evicted.push(candidate.number)
    count--
    pixels -= candidate.pixels
  }
  return evicted
}

/**
 * Owns cancellers for one rendering generation. Replacing or disposing it
 * synchronously cancels every loading/render task still owned by the old work.
 */
export class PreviewWork {
  #generation = 0
  #cancellers = new Set<() => void>()

  replace(): number {
    this.cancel()
    return ++this.#generation
  }

  current(generation: number): boolean {
    return generation === this.#generation
  }

  retain(cancel: () => void): () => void {
    this.#cancellers.add(cancel)
    return () => this.#cancellers.delete(cancel)
  }

  cancel(): void {
    for (const cancel of this.#cancellers) cancel()
    this.#cancellers.clear()
  }

  dispose(): void {
    this.cancel()
    this.#generation++
  }
}
