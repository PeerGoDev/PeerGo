import * as React from "react"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Spinner } from "~/components/ui/spinner"

const turnstileScriptID = "peergo-turnstile-api"
const turnstileScriptURL =
  "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit"

type TurnstileTheme = "light" | "dark" | "auto"

type TurnstileAPI = {
  render: (
    container: HTMLElement,
    options: {
      sitekey: string
      action: string
      theme: TurnstileTheme
      size: "flexible"
      callback: (token: string) => void
      "expired-callback": () => void
      "timeout-callback": () => void
      "error-callback": () => void
    }
  ) => string
  remove: (widgetID: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
  }
}

let turnstileLoader: Promise<TurnstileAPI> | undefined

function loadTurnstile() {
  if (window.turnstile) {
    return Promise.resolve(window.turnstile)
  }
  turnstileLoader ??= new Promise<TurnstileAPI>((resolve, reject) => {
    const ready = () => {
      if (window.turnstile) {
        resolve(window.turnstile)
      } else {
        reject(new Error("Turnstile API loaded without a renderer"))
      }
    }
    const failed = () => reject(new Error("Turnstile API failed to load"))
    const existing = document.getElementById(turnstileScriptID)
    if (existing instanceof HTMLScriptElement) {
      existing.addEventListener("load", ready, { once: true })
      existing.addEventListener("error", failed, { once: true })
      return
    }

    const script = document.createElement("script")
    script.id = turnstileScriptID
    script.src = turnstileScriptURL
    script.async = true
    script.defer = true
    script.addEventListener("load", ready, { once: true })
    script.addEventListener("error", failed, { once: true })
    document.head.append(script)
  }).catch((error) => {
    turnstileLoader = undefined
    throw error
  })
  return turnstileLoader
}

export function TurnstileWidget({
  enabled,
  siteKey,
  action,
  resetKey,
  onTokenChange,
}: {
  enabled: boolean
  siteKey: string
  action: "registration" | "login" | "password_recovery"
  resetKey: number
  onTokenChange: (token: string) => void
}) {
  const containerRef = React.useRef<HTMLDivElement>(null)
  const onTokenChangeRef = React.useRef(onTokenChange)
  const [state, setState] = React.useState<"loading" | "ready" | "error">(
    "loading"
  )

  React.useEffect(() => {
    onTokenChangeRef.current = onTokenChange
  }, [onTokenChange])

  React.useEffect(() => {
    if (!enabled || !siteKey || !containerRef.current) {
      onTokenChangeRef.current("")
      return
    }

    let active = true
    let widgetID: string | undefined
    setState("loading")
    onTokenChangeRef.current("")

    void loadTurnstile()
      .then((turnstile) => {
        if (!active || !containerRef.current) return
        widgetID = turnstile.render(containerRef.current, {
          sitekey: siteKey,
          action,
          theme: "auto",
          size: "flexible",
          callback: (token) => {
            if (active) onTokenChangeRef.current(token)
          },
          "expired-callback": () => {
            if (active) onTokenChangeRef.current("")
          },
          "timeout-callback": () => {
            if (active) onTokenChangeRef.current("")
          },
          "error-callback": () => {
            if (!active) return
            onTokenChangeRef.current("")
            setState("error")
          },
        })
        setState("ready")
      })
      .catch(() => {
        if (!active) return
        onTokenChangeRef.current("")
        setState("error")
      })

    return () => {
      active = false
      onTokenChangeRef.current("")
      if (widgetID && window.turnstile) {
        window.turnstile.remove(widgetID)
      }
    }
  }, [action, enabled, resetKey, siteKey])

  if (!enabled) return null

  return (
    <div className="min-w-0">
      {state === "loading" ? (
        <div
          className="flex min-h-16 items-center justify-center gap-2 rounded-lg border bg-muted/20 text-sm text-muted-foreground"
          aria-live="polite"
        >
          <Spinner />
          正在载入人机验证…
        </div>
      ) : null}
      {state === "error" ? (
        <Alert variant="destructive">
          <AlertTitle>人机验证暂时无法载入</AlertTitle>
          <AlertDescription>
            请检查网络或内容拦截扩展，然后刷新页面重试。
          </AlertDescription>
        </Alert>
      ) : null}
      <div
        ref={containerRef}
        className={state === "ready" ? "min-h-16" : "hidden"}
        aria-label="人机验证"
      />
    </div>
  )
}
