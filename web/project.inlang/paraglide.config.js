export default {
  outdir: "./src/paraglide",
  emitTsDeclarations: true,
  outputStructure: "locale-modules",
  isServer: "import.meta.env?.SSR ?? typeof window === 'undefined'",
  strategy: ["baseLocale"],
}
