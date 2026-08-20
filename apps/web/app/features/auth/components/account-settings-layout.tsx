import type { LucideIcon } from "lucide-react"
import {
  BadgeCheckIcon,
  MailCheckIcon,
  ShieldIcon,
  UserIcon,
} from "lucide-react"
import type { ReactNode } from "react"
import { Link } from "react-router"

import { buttonVariants } from "~/components/ui/button"
import { cn } from "~/lib/utils"
import { PageLayout } from "~/shared/components/page-layout"

type AccountSettingsSection = "profile" | "security" | "email" | "permissions"

const accountSettingsItems: Array<{
  section: AccountSettingsSection
  label: string
  to: string
  icon: LucideIcon
}> = [
  {
    section: "profile",
    label: "个人资料",
    to: "/account",
    icon: UserIcon,
  },
  {
    section: "security",
    label: "安全设置",
    to: "/account/security",
    icon: ShieldIcon,
  },
  {
    section: "email",
    label: "邮箱验证",
    to: "/account/email",
    icon: MailCheckIcon,
  },
  {
    section: "permissions",
    label: "我的权限",
    to: "/account/permissions",
    icon: BadgeCheckIcon,
  },
]

export function AccountSettingsLayout({
  active,
  title,
  description,
  children,
  contentClassName,
}: {
  active: AccountSettingsSection
  title: ReactNode
  description: ReactNode
  children: ReactNode
  contentClassName?: string
}) {
  return (
    <PageLayout className="flex-row items-start gap-6 max-md:flex-col">
      <nav
        aria-label="账户设置"
        className="flex w-48 shrink-0 flex-col gap-1 max-md:w-full"
      >
        {accountSettingsItems.map((item) => {
          const current = item.section === active
          return (
            <Link
              key={item.section}
              to={item.to}
              className={cn(
                buttonVariants({
                  variant: current ? "default" : "ghost",
                  size: "sm",
                }),
                "w-full shrink-0 justify-start gap-3 px-3 font-normal",
                !current && "text-muted-foreground"
              )}
              aria-current={current ? "page" : undefined}
            >
              <item.icon />
              {item.label}
            </Link>
          )
        })}
      </nav>
      <div
        className={cn("flex min-w-0 flex-1 flex-col gap-4", contentClassName)}
      >
        <header className="sr-only">
          <h1>{title}</h1>
          <p>{description}</p>
        </header>
        {children}
      </div>
    </PageLayout>
  )
}
