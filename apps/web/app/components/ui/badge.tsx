import { mergeProps } from "@base-ui/react/merge-props"
import { useRender } from "@base-ui/react/use-render"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "~/lib/utils"

const badgeVariants = cva(
  "group/badge inline-flex h-[1.125rem] w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md border border-transparent px-1.5 text-2xs leading-none font-medium whitespace-nowrap transition-colors focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 has-data-[icon=inline-end]:pr-1 has-data-[icon=inline-start]:pl-1 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3!",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a]:hover:brightness-110",
        secondary:
          "border-border bg-muted text-muted-foreground [a]:hover:bg-accent [a]:hover:text-foreground",
        primary: "bg-primary/12 text-link [a]:hover:bg-primary/20",
        success:
          "bg-success/12 text-success-foreground [a]:hover:bg-success/20",
        warning:
          "bg-warning/12 text-warning-foreground [a]:hover:bg-warning/20",
        info: "bg-info/12 text-info [a]:hover:bg-info/20",
        destructive:
          "bg-destructive/12 text-destructive focus-visible:ring-destructive/20 dark:focus-visible:ring-destructive/40 [a]:hover:bg-destructive/20",
        destructiveSolid:
          "bg-destructive text-white focus-visible:ring-destructive/30 [a]:hover:brightness-110",
        outline:
          "border-input text-foreground [a]:hover:bg-accent [a]:hover:text-foreground",
        ghost: "hover:bg-accent hover:text-foreground",
        link: "text-link underline-offset-4 hover:underline",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  render,
  ...props
}: useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: "span",
    props: mergeProps<"span">(
      {
        className: cn(badgeVariants({ variant }), className),
      },
      props
    ),
    render,
    state: {
      slot: "badge",
      variant,
    },
  })
}

export { Badge, badgeVariants }
