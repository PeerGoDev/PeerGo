import type { components } from "~/generated/api"

type CapabilityAction = components["schemas"]["CapabilityAction"]
type CapabilityList = components["schemas"]["CapabilityList"]

export function hasCapability(
  capabilities: CapabilityList | undefined,
  action: CapabilityAction
) {
  return (
    capabilities?.items.some((capability) => capability.action === action) ??
    false
  )
}
