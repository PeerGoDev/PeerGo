import { reactRouter } from "@react-router/dev/vite"
import tailwindcss from "@tailwindcss/vite"
import { configDefaults, defineConfig } from "vitest/config"

// Overridable so a dev Core on a non-default port (for example when 8080 is
// occupied by another local service) can back this dev server.
const coreTarget = process.env.PEERGO_CORE_PROXY ?? "http://127.0.0.1:8080"
const coreProxy = {
  "/api": coreTarget,
  "/rss": coreTarget,
  "/healthz": coreTarget,
}

export default defineConfig(({ mode }) => ({
  resolve: { tsconfigPaths: true },
  build: {
    // Keep this namespace stable after the 2026-08 cache-policy correction.
    // It prevents previously cached negative /assets responses from shadowing
    // a newly published content-addressed file with the same hash.
    assetsDir: "assets/v2",
  },
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
    // Node >= 22 defines a globalThis.localStorage that is non-functional
    // without --localstorage-file (an empty object whose setItem/clear are
    // undefined). Vitest's jsdom environment skips window keys that already
    // exist on globalThis, so that broken stub shadows jsdom's real Storage
    // and every localStorage-backed test throws. Disabling the built-in Web
    // Storage in test workers lets jsdom's implementation through.
    execArgv: ["--no-experimental-webstorage"],
  },
}))
