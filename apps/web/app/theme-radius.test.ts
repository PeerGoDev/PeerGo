import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { describe, expect, it } from "vitest"

describe("Direction I radius scale", () => {
  it("uses the 13px-based fixed radius steps instead of proportional rounding", () => {
    const css = readFileSync(resolve(process.cwd(), "app/app.css"), "utf8")

    expect(css).toContain("--radius: 0.8125rem;")
    expect(css).toContain("--radius-sm: 0.5rem;")
    expect(css).toContain("--radius-md: 0.625rem;")
    expect(css).toContain("--radius-lg: var(--radius);")
    expect(css).toContain("--radius-xl: 1rem;")
    expect(css).toContain("--radius-2xl: 1.25rem;")
    expect(css).toContain("--radius-3xl: 1.625rem;")
    expect(css).toContain("--radius-4xl: 2rem;")
  })
})
