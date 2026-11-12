import { Switch as SwitchPrimitive } from "@base-ui/react/switch"

import { cn } from "~/lib/utils"

function Switch({
  className,
  size = "default",
  ...props
}: SwitchPrimitive.Root.Props & {
  size?: "sm" | "default"
}) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn(
        "peer group/switch relative inline-flex shrink-0 items-center rounded-full px-0.5 transition-colors outline-none after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:ring-3 focus-visible:ring-primary/30 aria-invalid:ring-3 aria-invalid:ring-destructive/30 data-[size=default]:h-4 data-[size=default]:w-7 data-[size=sm]:h-3.5 data-[size=sm]:w-6 data-checked:bg-primary data-unchecked:bg-input data-disabled:cursor-not-allowed data-disabled:opacity-50",
        className
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="pointer-events-none block rounded-full bg-white shadow-xs ring-0 transition-transform group-data-[size=default]/switch:size-3 group-data-[size=sm]/switch:size-2.5 group-data-[size=default]/switch:data-checked:translate-x-3 group-data-[size=sm]/switch:data-checked:translate-x-2.5 data-unchecked:translate-x-0 dark:data-unchecked:bg-muted-foreground"
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
