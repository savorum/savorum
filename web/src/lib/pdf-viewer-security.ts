// PDF scripting and dynamic code evaluation are independent controls. Keep
// both disabled even though the current PDF.js release defaults scripting off;
// the restrictive application CSP remains a separate defense-in-depth layer.
export const pdfViewerSecurityOptions = Object.freeze({
  enableScripting: false,
  isEvalSupported: false,
  enableXfa: false,
})
