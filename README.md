# hookdock

Webhook 接收 + 规则化定时任务网关。单二进制（Go + SQLite，无 CGO），前端 React 19 + Vite + Tailwind v4 内嵌为 embed，一条命令起容器。

## 特性

- **端点**：每个端点一个对外 URL（`/hook/<slug>`），支持 `none` / `token` / `hmac`（`X-Hub-Signature-256`）三种鉴权，可设速率限制与留存天数。
- **留档**：每次收到的请求完整存库（method / query / headers 脱敏 / body / 解析后的 payload），UI 可翻看、可一键转成任务。
- **规则**：按 `body` / `query` / `header` 字段匹配（eq / contains / prefix / regex / exists），命中后产生任务。
  - `recurring`（默认）：同一规则只维护一条常驻任务，命中只刷新 payload 与下次执行时间，不会重复堆积。
  - `per_event`：每次命中各建一条一次性任务，执行完自动停用。
- **任务可靠执行**：`{{ payload.x }}` / `{{ job.name }}` / `{{ run.attempt }}` 模板替换；进程组执行 + 超时 TERM→KILL 杀整棵树；失败指数退避重试（2^n + 抖动），用尽后进死信，死信可人工重投；重启后补跑 misfire。
- **实时**：SSE 推送请求到达 / 任务创建 / 运行开始 / 重试 / 结束。
- **文档**：`/openapi.json` 返回 OpenAPI 3.1，`/docs` 是内嵌的 API 文档页。

## 快速开始

```bash
cp .env.example .env      # 设置 HOOKDOCK_ADMIN_TOKEN
docker compose up -d
# 打开 http://127.0.0.1:8380
```

管理接口（`POST /api/jobs` 等）在设置了 `HOOKDOCK_ADMIN_TOKEN` 后需要 `X-Admin-Token` 头；**未设置时管理接口对外开放，仅限本机或内网使用**。

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `HOOKDOCK_ADDR` | `:8080` | 监听地址 |
| `HOOKDOCK_DATA` | `/data` | 数据目录（SQLite 落在这里） |
| `HOOKDOCK_WORKERS` | `4` | 并发执行 worker 数 |
| `HOOKDOCK_ADMIN_TOKEN` | 空 | 管理接口令牌，为空则管理接口不鉴权 |

## 调度表达式

- `@every 30s` / `@every 5m` / `@every 2h` —— 固定间隔
- `0 3 * * *` —— 5 段 cron（分 时 日 月 周，支持 `*`、`,`、`-`、`/`）

## 开发

```bash
# 后端
go build ./... && go run .            # 需要 web/dist 存在，否则前端是空壳

# 前端
cd web && bun install && bun run dev  # 5273 端口，代理到 8080

# 打包镜像（宿主机交叉编译 + 只 COPY 二进制）
./scripts/build-local.sh hookdock:0.1.0 linux arm64
```

容器内编译 `modernc.org/sqlite` 会 OOM（小内存机器尤其明显），所以构建脚本走宿主机交叉编译。

## 数据

`data/hookdock.db`（SQLite，WAL）。请求按端点的 `retain_days` 自动清理，每小时跑一次。
