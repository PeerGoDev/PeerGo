import "@testing-library/jest-dom/vitest"
import { cleanup } from "@testing-library/react"
import { afterEach } from "vitest"

class ResizeObserverStub implements ResizeObserver {
  disconnect() {}

  observe(_target: Element, _options?: ResizeObserverOptions) {}

  unobserve(_target: Element) {}
}

globalThis.ResizeObserver ??= ResizeObserverStub

document.elementFromPoint ??= () => null

afterEach(() => {
  cleanup()
})
