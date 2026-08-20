# PeerGo Web

PeerGo 的首版 Web 应用，使用 React 19.2.7、React Router 8.3.0 Framework Mode、TypeScript、TanStack Query、Tailwind CSS 和 shadcn/ui。当前配置为 SPA rendering，开发服务器把 `/api` 与 `/healthz` 代理到 Core API。

## 常用命令

在仓库根目录运行：

```bash
pnpm dev:web
pnpm typecheck:web
pnpm test:web
pnpm build:web
pnpm test:e2e:web
```

`test:e2e:web` 会先生成生产 SPA 构建，再用 Playwright Chromium 在桌面和移动端运行确定性 API fixture；首次运行前执行 `pnpm --filter @peergo/web exec playwright install chromium`。浏览器报告和失败 trace 位于 Git 忽略的 `apps/web/playwright-report` 与 `apps/web/test-results`。

OpenAPI 是接口类型的唯一来源；修改 `contracts/openapi/v1` 后运行：

```bash
pnpm generate:web
```

生成的 `app/generated/api.ts` 禁止手工编辑。

## 组件约定

- shadcn/ui 使用 Base UI 实现，组合触发器时使用 `render`，不要套用 Radix 的 `asChild` 示例。
- 通用原语统一放在 `app/components/ui`；业务组件按 `app/features/<feature>` 归属，不再创建第二套 Button、Card、Field 或 DTO。
- 导入统一使用 `~/` 别名，例如 `import { Button } from "~/components/ui/button"`。
- 新增官方组件前先查询当前 registry，再在本应用目录运行 `pnpm exec shadcn add <component>`。
