import { type ESTree, parseSync } from "vite"

export type { ESTree }

/** Parses JavaScript or TypeScript, choosing the dialect from the file extension. */
export function parseProgram(file: string, text: string): ESTree.Program {
  const { program, errors } = parseSync(file, text)
  const [error] = errors
  if (error) throw new Error(`${file}: ${error.message}`)
  return program
}

function isNode(value: unknown): value is ESTree.Node {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { type?: unknown }).type === "string"
  )
}

/** Visits every node depth first; `ancestors` lists the enclosing nodes, nearest last. */
export function walk(
  node: ESTree.Node,
  visit: (node: ESTree.Node, ancestors: readonly ESTree.Node[]) => void,
  ancestors: ESTree.Node[] = [],
): void {
  visit(node, ancestors)
  ancestors.push(node)
  for (const [key, value] of Object.entries(node)) {
    if (key === "parent") continue
    for (const child of Array.isArray(value) ? (value as unknown[]) : [value]) {
      if (isNode(child)) walk(child, visit, ancestors)
    }
  }
  ancestors.pop()
}

/** The source text a node spans. */
export function sourceOf(text: string, node: ESTree.Span): string {
  return text.slice(node.start, node.end)
}

/** The one-based line of a UTF-16 offset. */
export function lineAt(text: string, offset: number): number {
  return text.slice(0, offset).split("\n").length
}
