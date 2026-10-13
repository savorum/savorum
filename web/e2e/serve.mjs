// Starts the Savorum binary against a throwaway library for the browser tests.
// The binary carries the frontend and the document renderer, so the tests run
// against what a reader would actually install.
import { spawn } from "node:child_process"
import { access, chmod, mkdtemp, mkdir, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const port = process.argv[2] ?? "8123"
const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const binary = resolve(webRoot, "../bin/savorum")

try {
  await access(binary)
} catch {
  console.error(`No Savorum binary at ${binary}. Build one first with "just build".`)
  process.exit(1)
}

const directory = await mkdtemp(join(tmpdir(), "savorum-e2e-"))
// The browser fixture is a PDF-signature document whose extracted text is
// supplied by this throwaway command. This keeps document import coverage
// deterministic on machines without Poppler while production still uses its
// ordinary local extractor.
const tools = join(directory, "tools")
await mkdir(tools)
// Extraction asks Poppler for word geometry rather than a transcript, so the
// stand-in writes the same bounding-box report Poppler would: one line per line
// of the fixture, laid out down a single column.
const layout = join(tools, "layout.awk")
await writeFile(
  layout,
  [
    'BEGIN { print "<html><body><doc><page width=\\"612\\" height=\\"792\\">" }',
    "/^%%SAVORUM_TEXT%%$/ { seen = 1; next }",
    "seen {",
    '  gsub(/&/, "\\&amp;"); gsub(/</, "\\&lt;"); gsub(/>/, "\\&gt;")',
    "  top = 50 + 20 * line++",
    '  printf "<flow><block><line>"',
    "  left = 50",
    "  count = split($0, words, / /)",
    "  for (i = 1; i <= count; i++) {",
    '    if (words[i] == "") continue',
    "    width = 6 * length(words[i])",
    '    printf "<word xMin=\\"%.1f\\" yMin=\\"%.1f\\" xMax=\\"%.1f\\" yMax=\\"%.1f\\">%s</word>", left, top, left + width, top + 12, words[i]',
    "    left = left + width + 4",
    "  }",
    '  print "</line></block></flow>"',
    "}",
    'END { print "</page></doc></body></html>" }',
    "",
  ].join("\n"),
)
const pdftotext = join(tools, "pdftotext")
await writeFile(
  pdftotext,
  `#!/bin/sh\ntest "$#" = 7 && test "$1" = -f && test "$2" = 1 && test "$3" = -l && test "$4" = 1 && test "$5" = -bbox-layout && test "$7" = - || exit 9\nawk -f "${layout}" "$6"\n`,
)
await chmod(pdftotext, 0o755)
const pdfinfo = join(tools, "pdfinfo")
await writeFile(pdfinfo, `#!/bin/sh\ntest "$#" = 1 || exit 9\nprintf 'Pages: 1\\n'\n`)
await chmod(pdfinfo, 0o755)
const { SAVORUM_E2E_DATABASE, SAVORUM_E2E_ASSETS, SAVORUM_SCALE_REPORT, ...serverEnvironment } =
  process.env
const server = spawn(
  binary,
  [
    "serve",
    "--database",
    SAVORUM_E2E_DATABASE ?? join(directory, "library.db"),
    ...(SAVORUM_E2E_ASSETS ? ["--assets", SAVORUM_E2E_ASSETS] : []),
    "--listen",
    `127.0.0.1:${port}`,
  ],
  { stdio: "inherit", env: { ...serverEnvironment, PATH: `${tools}:${process.env.PATH ?? ""}` } },
)

const stop = async (code) => {
  server.kill("SIGTERM")
  await rm(directory, { recursive: true, force: true })
  process.exit(code)
}

process.on("SIGTERM", () => void stop(0))
process.on("SIGINT", () => void stop(0))
server.on("exit", (code) => void stop(code ?? 0))
