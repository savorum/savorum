import { paraglideVitePlugin } from "@inlang/paraglide-js"
import { tanstackStart } from "@tanstack/solid-start/plugin/vite"
import { defineConfig } from "vite"
import solidPlugin from "vite-plugin-solid"

import { messageExportsPlugin } from "./scripts/generate-messages.ts"

export default defineConfig(({ command }) => ({
  resolve: { tsconfigPaths: true },
  build: {
    rolldownOptions: {
      checks: {
        // Panda's generated Solid factories use valid pure annotations in a
        // position Rolldown currently warns about. This does not affect output.
        invalidAnnotation: false,
      },
      output: {
        codeSplitting: {
          groups: [
            {
              name: "app",
              tags: ["$initial"],
            },
            {
              name: "library",
              test: /\/src\/routes\/index\.tsx\?tsr-split=component$/,
            },
          ],
        },
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/openapi.json": "http://127.0.0.1:8080",
      "/llms.txt": "http://127.0.0.1:8080",
      "/AGENTS.md": "http://127.0.0.1:8080",
      "/.well-known": "http://127.0.0.1:8080",
      "/oauth": "http://127.0.0.1:8080",
      "/mcp": "http://127.0.0.1:8080",
    },
  },
  // TanStack prerender fetches Vite's preview URL from inside the build
  // container. Pin it to IPv4 so Docker hosts without bridge IPv6 do not
  // resolve localhost to an unreachable ::1 listener.
  preview: { host: "127.0.0.1" },
  plugins: [
    command === "serve" && paraglideVitePlugin({ project: "./project.inlang" }),
    messageExportsPlugin(),
    tanstackStart({
      spa: {
        enabled: true,
        prerender: {
          outputPath: "/index.html",
        },
      },
    }),
    solidPlugin({ ssr: true }),
  ],
}))
