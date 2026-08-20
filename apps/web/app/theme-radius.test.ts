import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { describe, expect, it } from "vitest"

describe("PtYes-compatible radius scale", () => {
  it("uses the fixed Tailwind radius steps instead of proportional rounding", () => {
    const css = readFileSync(resolve(process.cwd(), "app/app.css"), "utf8")

    expect(css).toContain("--radius-sm: 0.25rem;")
    expect(css).toContain("--radius-md: 0.375rem;")
    expect(css).toContain("--radius-lg: var(--radius);")
    expect(css).toContain("--radius-xl: 0.75rem;")
    expect(css).toContain("--radius-2xl: 1rem;")
    expect(css).toContain("--radius-3xl: 1.5rem;")
    expect(css).toContain("--radius-4xl: 2rem;")
  })
})
