import { reactRouter } from "@react-router/dev/vite"
import tailwindcss from "@tailwindcss/vite"
import { configDefaults, defineConfig } from "vitest/config"

const coreProxy = {
  "/api": "http://127.0.0.1:8080",
  "/rss": "http://127.0.0.1:8080",
  "/healthz": "http://127.0.0.1:8080",
}

export default defineConfig(({ mode }) => ({
  resolve: { tsconfigPaths: true },
  // Vitest transforms JSX itself. Running the Framework plugin inside its
  // isolated DOM workers injects the dev preamble twice and breaks component
  // tests, so the plugin is only active for real app builds/dev servers.
  plugins: mode === "test" ? [] : [tailwindcss(), reactRouter()],
  server: {
    proxy: coreProxy,
  },
  preview: {
    proxy: coreProxy,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./app/test/setup.ts"],
    exclude: [...configDefaults.exclude, "e2e/**"],
    css: true,
    // The suite renders several full shadcn/Base UI pages. Bounding worker
    // count keeps interaction tests deterministic while local services run.
    maxWorkers: 4,
  },
}))
