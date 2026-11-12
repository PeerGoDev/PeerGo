import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "~/lib/utils"

const buttonVariantClasses = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-[color,background-color,border-color,box-shadow,filter] outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30 active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "border-0 bg-primary sheen text-primary-foreground shadow-button hover:brightness-110 active:brightness-95 aria-expanded:brightness-110",
        outline:
          "border-input bg-transparent text-foreground hover:bg-accent aria-expanded:bg-accent",
        outlineDestructive:
          "border-destructive/35 bg-transparent text-destructive hover:border-destructive/50 hover:bg-destructive/10 focus-visible:border-destructive/40 focus-visible:ring-destructive/20",
        secondary:
          "border-border bg-secondary text-secondary-foreground hover:border-input hover:bg-accent aria-expanded:bg-accent",
        actionSuccess:
          "bg-action-success text-action-success-foreground hover:brightness-110 focus-visible:border-action-success/40 focus-visible:ring-action-success/20",
        actionInfo:
          "bg-info text-info-foreground hover:brightness-110 focus-visible:border-info/40 focus-visible:ring-info/20",
        actionDestructive:
          "bg-action-destructive text-action-destructive-foreground hover:brightness-110 focus-visible:border-action-destructive/40 focus-visible:ring-action-destructive/20",
        soft: "bg-primary/10 text-link hover:bg-primary/15 aria-expanded:bg-primary/15",
        softDestructive:
          "bg-destructive/10 text-destructive hover:bg-destructive/15 focus-visible:border-destructive/40 focus-visible:ring-destructive/20 dark:bg-destructive/15 dark:hover:bg-destructive/25",
        ghost: "text-foreground hover:bg-accent aria-expanded:bg-accent",
        destructive:
          "bg-destructive text-white hover:brightness-110 focus-visible:border-destructive/40 focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40",
        link: "text-link underline-offset-4 hover:underline",
      },
      size: {
        default:
          "h-(--control-h) gap-2 px-4 has-data-[icon=inline-end]:pr-3 has-data-[icon=inline-start]:pl-3",
        xs: "h-6 gap-1 rounded-md px-2 text-xs in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3",
        sm: "h-(--control-h-sm) gap-1.5 px-3 in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-2.5 has-data-[icon=inline-start]:pl-2.5 [&_svg:not([class*='size-'])]:size-3.5",
        legacySm:
          "h-(--control-h-sm) gap-1 px-3 font-normal [&_svg:not([class*='size-'])]:size-3.5",
        lg: "h-10 gap-2 px-6 has-data-[icon=inline-end]:pr-5 has-data-[icon=inline-start]:pl-5",
        legacy: "h-(--control-h) gap-1 px-4 font-normal",
        icon: "size-(--control-h)",
        "icon-xs":
          "size-6 rounded-md in-data-[slot=button-group]:rounded-lg [&_svg:not([class*='size-'])]:size-3.5",
        "icon-sm":
          "size-(--control-h-sm) in-data-[slot=button-group]:rounded-lg",
        "icon-lg": "size-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

// Links styled as buttons call this directly, so the base `border-transparent`
// must be merged away here or it would hide the outline variant's border.
function buttonVariants(...args: Parameters<typeof buttonVariantClasses>) {
  return cn(buttonVariantClasses(...args))
}

function Button({
  className,
  variant = "default",
  size = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={buttonVariants({ variant, size, className })}
      {...props}
    />
  )
}

export { Button, buttonVariants }
