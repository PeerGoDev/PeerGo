import { act, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TurnstileWidget } from "~/features/auth/components/turnstile-widget"

describe("TurnstileWidget", () => {
  afterEach(() => {
    delete window.turnstile
    document.getElementById("peergo-turnstile-api")?.remove()
  })

  it("uses explicit rendering, binds the flow action, and clears single-use tokens on reset", async () => {
    let options:
      | {
          action: string
          callback: (token: string) => void
          "expired-callback": () => void
        }
      | undefined
    const remove = vi.fn()
    window.turnstile = {
      render: vi.fn((_container, nextOptions) => {
        options = nextOptions
        return "widget-1"
      }),
      remove,
    }
    const onTokenChange = vi.fn()
    const view = render(
      <TurnstileWidget
        enabled
        siteKey="public-site-key"
        action="registration"
        resetKey={0}
        onTokenChange={onTokenChange}
      />
    )

    await waitFor(() => expect(options?.action).toBe("registration"))
    expect(screen.getByLabelText("人机验证")).toBeVisible()

    act(() => options?.callback("single-use-token"))
    expect(onTokenChange).toHaveBeenLastCalledWith("single-use-token")

    act(() => options?.["expired-callback"]())
    expect(onTokenChange).toHaveBeenLastCalledWith("")

    view.rerender(
      <TurnstileWidget
        enabled
        siteKey="public-site-key"
        action="registration"
        resetKey={1}
        onTokenChange={onTokenChange}
      />
    )
    await waitFor(() => expect(remove).toHaveBeenCalledWith("widget-1"))
  })
})
