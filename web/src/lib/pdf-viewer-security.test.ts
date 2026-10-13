import assert from "node:assert/strict"
import test from "node:test"

import { pdfViewerSecurityOptions } from "./pdf-viewer-security.ts"

test("PDF preview explicitly disables scripting and dynamic evaluation", () => {
  assert.deepEqual(pdfViewerSecurityOptions, {
    enableScripting: false,
    isEvalSupported: false,
    enableXfa: false,
  })
  assert.equal(Object.isFrozen(pdfViewerSecurityOptions), true)
})
