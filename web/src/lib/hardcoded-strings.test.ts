import assert from "node:assert/strict"
import fs from "node:fs"
import path from "node:path"
import test from "node:test"

import ts from "typescript"

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
    const sourceFile = ts.createSourceFile(filePath, content, ts.ScriptTarget.Latest, true)

    function visit(node: ts.Node) {
      if (ts.isJsxText(node)) {
        const text = node.text.trim()
        if (text && /[a-zA-Z]{2,}/.test(text)) {
          let parent: ts.Node | undefined = node.parent
          let isIgnoredTag = false
          while (parent && (ts.isJsxElement(parent) || ts.isJsxFragment(parent))) {
            if (ts.isJsxElement(parent)) {
              const tag = parent.openingElement.tagName.getText(sourceFile).toLowerCase()
              if (tag === "kbd" || tag === "style" || tag === "script" || tag === "svg") {
                isIgnoredTag = true
                break
              }
            }
            parent = parent.parent
          }
          if (!isIgnoredTag) {
            const { line } = sourceFile.getLineAndCharacterOfPosition(node.getStart())
            violations.push({
              file: path.relative(dir, filePath),
              line: line + 1,
              text,
              type: "JsxText",
            })
          }
        }
      } else if (
        ts.isJsxExpression(node) &&
        node.parent &&
        (ts.isJsxElement(node.parent) || ts.isJsxFragment(node.parent))
      ) {
        function checkExpr(expr?: ts.Expression) {
          if (!expr) return
          if (ts.isStringLiteral(expr) || ts.isNoSubstitutionTemplateLiteral(expr)) {
            const text = expr.text.trim()
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
            ) {
              const { line } = sourceFile.getLineAndCharacterOfPosition(expr.getStart())
              violations.push({
                file: path.relative(dir, filePath),
                line: line + 1,
                text,
                type: "JsxExpressionStringLiteral",
              })
            }
          } else if (ts.isConditionalExpression(expr)) {
            checkExpr(expr.whenTrue)
            checkExpr(expr.whenFalse)
          }
        }
        checkExpr(node.expression)
      }
      ts.forEachChild(node, visit)
    }

    visit(sourceFile)
  }

  function walk(currentDir: string) {
    for (const entry of fs.readdirSync(currentDir, { withFileTypes: true })) {
      const full = path.join(currentDir, entry.name)
      if (entry.isDirectory()) {
        if (entry.name !== "node_modules" && entry.name !== "dist" && entry.name !== "paraglide") {
          walk(full)
        }
      } else if (entry.name.endsWith(".tsx") && !entry.name.endsWith(".test.tsx")) {
        checkFile(full)
      }
    }
  }

  walk(dir)
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
