import { fileURLToPath, URL } from "node:url"

import solid from "vite-plugin-solid"
import { defineConfig } from "vitest/config"

export default defineConfig({
  plugins: [solid()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "styled-system": fileURLToPath(new URL("./styled-system", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    pool: "vmThreads",
    maxWorkers: process.env.CI ? 2 : undefined,
    // The default limit derives from host memory, which a CI container
    // reports instead of its own limit, so workers are never recycled.
    vmMemoryLimit: process.env.CI ? "512MB" : undefined,
    fsModuleCache: true,
    include: [
      "src/features/**/*.test.{ts,tsx}",
      "src/api/**/*.test.ts",
      "src/components/**/*.test.{ts,tsx}",
    ],
    exclude: ["**/*.node.test.ts"],
    restoreMocks: true,
    coverage: {
      provider: "v8",
      include: ["src/features/**/*.{ts,tsx}", "src/api/**/*.ts", "src/components/**/*.{ts,tsx}"],
      reporter: ["text", "json-summary"],
      thresholds: {
        statements: 20,
        branches: 18,
        functions: 18,
        lines: 24,
      },
    },
  },
})
