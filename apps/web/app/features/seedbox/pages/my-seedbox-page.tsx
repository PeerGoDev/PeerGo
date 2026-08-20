import { useState, type FormEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import { CircleAlertIcon, ServerIcon } from "lucide-react"
import { Link } from "react-router"

import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert"
import { Badge } from "~/components/ui/badge"
import { Button, buttonVariants } from "~/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "~/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "~/components/ui/field"
import { Input } from "~/components/ui/input"
import { Skeleton } from "~/components/ui/skeleton"
import { Textarea } from "~/components/ui/textarea"
import { useWebSession } from "~/features/auth/api/session.mutations"
import {
  mySeedboxReportsQueryOptions,
  useCreateSeedboxReport,
} from "~/features/seedbox/api/seedbox.queries"
import { ApiProblemError } from "~/shared/api/problem"
import { PageHeader, PageLayout } from "~/shared/components/page-layout"
import { formatDateTime } from "~/shared/formatters/date-time"

export function MySeedboxPage() {
  const session = useWebSession()
  const reports = useQuery({
    ...mySeedboxReportsQueryOptions(),
    enabled: Boolean(session.data),
  })
  return (
    <PageLayout>
      <PageHeader
        title="盒子申报"
        description="申报自己的远程做种主机，经审核后获得用户专属盒子识别。"
      />
      {session.isPending ? <Skeleton className="h-64 w-full" /> : null}
      {!session.isPending && !session.data ? (
        <Card>
          <CardHeader>
            <CardTitle>需要登录</CardTitle>
            <CardDescription>登录后才能申报和查看自己的盒子。</CardDescription>
          </CardHeader>
          <CardFooter>
            <Link to="/login" className={buttonVariants()}>
              前往登录
            </Link>
          </CardFooter>
        </Card>
      ) : null}
      {session.data ? (
        <>
          <SeedboxReportForm csrfToken={session.data.csrf_token} />
          <Card>
            <CardHeader>
              <CardTitle>我的申报记录</CardTitle>
              <CardDescription>
                待审核记录不会获得盒子倍率；批准后由 Tracker 热加载。
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {reports.isPending ? <Skeleton className="h-28 w-full" /> : null}
              {reports.isError ? (
                <Alert variant="destructive">
                  <CircleAlertIcon />
                  <AlertTitle>申报记录暂时无法读取</AlertTitle>
                  <AlertDescription>请稍后刷新页面。</AlertDescription>
                </Alert>
              ) : null}
              {reports.data?.items.length === 0 ? (
                <p className="py-6 text-center text-sm text-muted-foreground">
                  还没有盒子申报。
                </p>
              ) : null}
              {reports.data?.items.map((report) => (
                <section key={report.id} className="rounded-md border p-4">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-mono text-sm">{report.address}</span>
                    <Badge
                      variant={
                        report.status === "rejected"
                          ? "destructive"
                          : report.status === "pending"
                            ? "secondary"
                            : "outline"
                      }
                    >
                      {report.status === "pending"
                        ? "待审核"
                        : report.status === "approved"
                          ? "已批准"
                          : "已驳回"}
                    </Badge>
                  </div>
                  <p className="mt-2 text-sm text-muted-foreground">
                    {report.provider} ·{" "}
                    {report.bandwidth_mbps.toLocaleString("zh-CN")} Mbps ·{" "}
                    {formatDateTime(report.submitted_at)}
                  </p>
                  {report.decision_reason ? (
                    <p className="mt-2 text-sm">
                      审核说明：{report.decision_reason}
                    </p>
                  ) : null}
                </section>
              ))}
            </CardContent>
          </Card>
        </>
      ) : null}
    </PageLayout>
  )
}

function SeedboxReportForm({ csrfToken }: { csrfToken: string }) {
  const mutation = useCreateSeedboxReport()
  const [address, setAddress] = useState("")
  const [provider, setProvider] = useState("")
  const [bandwidth, setBandwidth] = useState("")
  const [statement, setStatement] = useState("")
  const [error, setError] = useState("")
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const bandwidthMbps = Number(bandwidth)
    if (
      !address.trim() ||
      provider.trim().length < 2 ||
      !Number.isSafeInteger(bandwidthMbps) ||
      bandwidthMbps < 1 ||
      statement.trim().length < 10
    ) {
      setError("请填写单个 IP、服务商、整数带宽和至少 10 个字符的说明。")
      return
    }
    setError("")
    try {
      await mutation.mutateAsync({
        csrfToken,
        idempotencyKey: crypto.randomUUID(),
        body: {
          address: address.trim(),
          provider: provider.trim(),
          bandwidth_mbps: bandwidthMbps,
          statement: statement.trim(),
        },
      })
      setAddress("")
      setProvider("")
      setBandwidth("")
      setStatement("")
    } catch (caught) {
      setError(
        caught instanceof ApiProblemError
          ? caught.detail || caught.message
          : "盒子申报失败，请稍后重试。"
      )
    }
  }
  return (
    <form onSubmit={submit}>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ServerIcon />
            提交盒子信息
          </CardTitle>
          <CardDescription>
            只接受你实际使用的单个主机 IP；服务商共享网段不能由用户自行申报。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <div className="grid gap-4 md:grid-cols-3">
              <Field>
                <FieldLabel htmlFor="seedbox-address">主机 IP</FieldLabel>
                <Input
                  id="seedbox-address"
                  value={address}
                  onChange={(event) => setAddress(event.target.value)}
                  placeholder="203.0.113.8"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="seedbox-provider">服务商</FieldLabel>
                <Input
                  id="seedbox-provider"
                  value={provider}
                  onChange={(event) => setProvider(event.target.value)}
                  placeholder="服务商名称"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="seedbox-bandwidth">
                  带宽（Mbps）
                </FieldLabel>
                <Input
                  id="seedbox-bandwidth"
                  type="number"
                  min="1"
                  step="1"
                  value={bandwidth}
                  onChange={(event) => setBandwidth(event.target.value)}
                />
              </Field>
            </div>
            <Field data-invalid={Boolean(error)}>
              <FieldLabel htmlFor="seedbox-statement">申报说明</FieldLabel>
              <Textarea
                id="seedbox-statement"
                rows={3}
                value={statement}
                onChange={(event) => setStatement(event.target.value)}
                placeholder="说明主机用途、套餐和可供管理员核验的信息"
              />
              <FieldDescription>
                不要填写密码、Tracker passkey 或服务商登录凭据。
              </FieldDescription>
              {error ? <FieldError>{error}</FieldError> : null}
            </Field>
          </FieldGroup>
        </CardContent>
        <CardFooter className="justify-end">
          <Button disabled={mutation.isPending} type="submit">
            提交审核
          </Button>
        </CardFooter>
      </Card>
    </form>
  )
}
