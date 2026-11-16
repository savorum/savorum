import assert from "node:assert/strict"
import fs from "node:fs"
import path from "node:path"
import test from "node:test"

import { type ESTree, lineAt, parseProgram, sourceOf, walk } from "../../scripts/syntax.ts"

interface Violation {
  file: string
  line: number
  text: string
  type: string
}

function findHardcodedStrings(dir: string): Violation[] {
  const violations: Violation[] = []

  function checkFile(filePath: string) {
    const content = fs.readFileSync(filePath, "utf8")
    const report = (node: ESTree.Span, text: string, type: string) =>
      violations.push({
        file: path.relative(dir, filePath),
        line: lineAt(content, node.start),
        text,
        type,
      })

    function checkExpr(expr: ESTree.Node) {
      const text =
        expr.type === "Literal" && typeof expr.value === "string"
          ? expr.value.trim()
          : expr.type === "TemplateLiteral" && expr.expressions.length === 0
            ? (expr.quasis[0]?.value.cooked ?? "").trim()
            : undefined
      if (text !== undefined) {
        // Flag string literals with words that are not URLs, css units, or technical IDs
        if (
          text &&
          /[a-zA-Z]{3,}/.test(text) &&
          !text.startsWith("/") &&
          !text.startsWith("#") &&
          !text.includes("px") &&
          !text.includes("rem") &&
          !text.includes("(") &&
          !text.includes(")")
        )
          report(expr, text, "JsxExpressionStringLiteral")
      } else if (expr.type === "ConditionalExpression") {
        checkExpr(expr.consequent)
        checkExpr(expr.alternate)
      }
    }

    walk(parseProgram(filePath, content), (node, ancestors) => {
      const parent = ancestors.at(-1)
      const inChildren = parent?.type === "JSXElement" || parent?.type === "JSXFragment"
      if (node.type === "JSXText") {
        const text = node.value.trim()
        if (!text || !/[a-zA-Z]{2,}/.test(text)) return
        for (let index = ancestors.length - 1; index >= 0; index--) {
          const ancestor = ancestors[index]
          if (ancestor?.type === "JSXFragment") continue
          if (ancestor?.type !== "JSXElement") break
          const tag = sourceOf(content, ancestor.openingElement.name).toLowerCase()
          if (tag === "kbd" || tag === "style" || tag === "script" || tag === "svg") return
        }
        report(node, text, "JsxText")
      } else if (node.type === "JSXExpressionContainer" && inChildren) {
        checkExpr(node.expression)
      }
    })
  }

  function visitDirectory(currentDir: string) {
    for (const entry of fs.readdirSync(currentDir, { withFileTypes: true })) {
      const full = path.join(currentDir, entry.name)
      if (entry.isDirectory()) {
        if (entry.name !== "node_modules" && entry.name !== "dist" && entry.name !== "paraglide") {
          visitDirectory(full)
        }
      } else if (entry.name.endsWith(".tsx") && !entry.name.endsWith(".test.tsx")) {
        checkFile(full)
      }
    }
  }

  visitDirectory(dir)
  return violations
}

test("no hardcoded user-facing strings in UI component JSX", () => {
  const srcDir = path.resolve(import.meta.dirname, "..")
  const violations = findHardcodedStrings(srcDir)

  const unexpected = violations.filter((v) => {
    if (v.file === "components/status.tsx" && v.text === "Loading…") return false
    // Counter pluralization.
    if (v.text === "recipe" || v.text === "recipes") return false
    return true
  })

  assert.deepEqual(
    unexpected,
    [],
    `Found unlocalized user-facing strings in JSX:\n${unexpected.map((u) => `  ${u.file}:${u.line} - "${u.text}" (${u.type})`).join("\n")}`,
  )
})
