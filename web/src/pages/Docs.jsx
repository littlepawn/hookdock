import React from 'react'
import { OPENAPI } from '../openapi.gen.js'
import { Badge, Button, Card, CardBody, CardTitle, CopyButton } from '../components/ui.jsx'

const NAV = [
  ['overview', '概览'],
  ['auth', '认证'],
  ['hook', '接收事件'],
  ['endpoints', '端点管理'],
  ['rules', '规则'],
  ['jobs', '任务'],
  ['runs', '运行'],
  ['errors', '错误码'],
]

function Sec({ id, title, children }) {
  return (
    <Card hover={false} id={`doc-${id}`} className="scroll-mt-24">
      <CardBody>
        <CardTitle>{title}</CardTitle>
        <div className="mt-3 space-y-3 text-[13px] leading-relaxed text-[#323130]">{children}</div>
      </CardBody>
    </Card>
  )
}

function Code({ children, label }) {
  return (
    <div className="group relative">
      {label ? (
        <p className="mb-1 text-[11.5px] font-semibold text-[#605e5c]">{label}</p>
      ) : null}
      <pre className="scroll-thin overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-3 font-mono text-[11.5px] leading-relaxed text-[#323130] whitespace-pre-wrap break-all">
        {children}
      </pre>
      <div className="absolute top-0 right-0 opacity-0 transition-opacity duration-150 group-hover:opacity-100">
        <CopyButton text={String(children).trim()} />
      </div>
    </div>
  )
}

export default function Docs({ origin }) {
  const [active, setActive] = React.useState('overview')
  React.useEffect(() => {
    const el = document.getElementById(`doc-${active}`)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [active])

  const base = origin || window.location.origin

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">API 文档</h1>
        <p className="mt-1 text-[12.5px] text-[#605e5c]">
          机器可读版本：
          <a
            href="/openapi.json"
            className="rounded text-[#0078d4] hover:underline focus-visible:ring-2 focus-visible:ring-[#0078d4]"
            target="_blank"
            rel="noreferrer"
          >
            /openapi.json
          </a>
          （OpenAPI 3.1，可导入 Apifox / Postman / Insomnia）
        </p>
      </div>

      <nav className="flex flex-wrap gap-1.5">
        {NAV.map(([id, label]) => (
          <button
            key={id}
            onClick={() => setActive(id)}
            className={
              'h-7 rounded-full px-3 text-[12px] font-semibold transition-all duration-150 ease-out active:scale-[0.97] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2 ' +
              (active === id
                ? 'bg-[#0078d4] text-white shadow-fluent2'
                : 'border border-[#e1e1e1] bg-white text-[#323130] hover:bg-[#f3f2f1]')
            }
          >
            {label}
          </button>
        ))}
      </nav>

      <div className="space-y-4">
        <Sec id="overview" title="概览">
          <p>
            hookdock 是 webhook 接收 + 定时任务网关：外部系统 POST 事件到 <code className="rounded bg-[#f3f2f1] px-1 font-mono text-[11.5px]">/hook/&lt;slug&gt;</code>
            ，请求被完整记录；命中规则的请求自动生成定时任务并按计划执行 shell 命令。
          </p>
          <Code label="Base URL">{base}</Code>
          <p>
            管理 API 以 <code className="rounded bg-[#f3f2f1] px-1 font-mono text-[11.5px]">/api</code> 开头，全部需要管理员令牌；hook 接收接口按端点自身配置鉴权。完整 paths 见 openapi.json。
          </p>
        </Sec>

        <Sec id="auth" title="认证">
          <p>管理 API 在请求头携带令牌（在页面右上角设置）：</p>
          <Code label="Header">{`X-Admin-Token: <管理员令牌>`}</Code>
          <p>hook 接收端点支持三种鉴权（端点设置里二选一，可不设即公开）：</p>
          <ul className="ml-4 list-disc space-y-1">
            <li>静态 Token：请求头 <code className="font-mono text-[11.5px]">X-Hook-Token</code> 等于端点 secret</li>
            <li>HMAC 签名：<code className="font-mono text-[11.5px]">X-Hub-Signature-256: sha256=&lt;hex(HMAC-SHA256(body, secret))&gt;</code>（GitHub 风格）</li>
            <li>无鉴权：secret 留空</li>
          </ul>
        </Sec>

        <Sec id="hook" title="接收事件">
          <p>向端点推送事件，body 任意（JSON / form / 纯文本），命中规则即返回生成的任务：</p>
          <Code label="请求">{`POST /hook/<slug>
X-Hook-Token: <secret>
Content-Type: application/json

{"action":"deploy","repo":"看山工具"}`}</Code>
          <Code label="响应 200">{`{
  "ok": true,
  "request_id": "rq_xxxx",
  "matched_jobs": ["jb_xxxx"]
}`}</Code>
          <p>
            payload 支持 <code className="font-mono text-[11.5px]">{'{{ payload.action }}'}</code> 点路径与数组下标，在任务命令里做模板替换；任务实际执行时以任务创建时刻的 payload 为准。
          </p>
        </Sec>

        <Sec id="endpoints" title="端点管理">
          <Code label="列出 / 新建">{`GET  /api/endpoints          → 端点数组（含 24h 请求计数）
POST /api/endpoints          → body: {"name":"部署","slug":"deploy","secret":"","rate_limit":0}
GET  /api/endpoints/<key>    → 单个端点
GET  /api/endpoints/<key>/requests?limit=50   → 该端点请求列表
GET  /api/endpoints/<key>/rules              → 该端点规则列表
POST /api/endpoints/<key>/rules              → 新建规则`}</Code>
          <p>字段说明：slug 即接收地址路径段（URL 安全）；secret 留空表示公开接收；rate_limit 为每分钟最大请求数，0 不限。</p>
        </Sec>

        <Sec id="rules" title="规则">
          <p>规则决定「什么样的请求生成什么样的定时任务」：</p>
          <Code label="新建规则">{`POST /api/endpoints/<key>/rules
{
  "name": "deploy 触发部署检查",
  "match_type": "body",        // body | query | header
  "match_key": "action",       // body 点路径 / query 名 / header 名
  "match_op": "eq",            // eq | neq | contains | exists
  "match_value": "deploy",
  "schedule": "@every 5m",     // @every 30s / 5m / 2h 或 5 段 cron "0 3 * * *"
  "exec_argv": ["/bin/sh","-c","deploy check {{ payload.repo }}"],
  "timeout_sec": 60,
  "max_attempts": 3
}`}</Code>
          <p>规则命中后立即创建任务并按 schedule 循环执行；修改规则用 <code className="font-mono text-[11.5px]">PUT /api/rules/&lt;id&gt;</code>，删除用 DELETE。</p>
        </Sec>

        <Sec id="jobs" title="任务">
          <Code label="任务 API">{`GET  /api/jobs                        → 任务数组
POST /api/jobs                        → 手工建任务（字段同规则）
GET  /api/jobs/<id>                   → 单个任务
POST /api/jobs/<id>/toggle            → 启用/暂停
POST /api/jobs/<id>/run               → 立即运行一次
GET  /api/jobs/<id>/runs?limit=100    → 该任务运行历史
DELETE /api/jobs/<id>                 → 删除任务及运行记录`}</Code>
          <p>调度器每秒扫描 next_run_at，到点入队；暂停的任务不再产生新运行。</p>
        </Sec>

        <Sec id="runs" title="运行与重试">
          <Code label="运行 API">{`GET  /api/runs?limit=200              → 最近运行（全局）
GET  /api/runs/<id>                   → 单次运行（含完整输出/退出码）
POST /api/runs/<id>/retry             → 死信重投（重新入队，attempt 归 1）`}</Code>
          <p>
            运行状态机：queued → running → succeeded / failed / timeout；失败按指数退避自动重试（约 2^attempt 秒 + 抖动），attempt 用尽后进入 <Badge tone="red">dead</Badge>
            ，可随时重投。执行超时会杀死整个进程组（含子进程），输出上限 256KB。
          </p>
        </Sec>

        <Sec id="errors" title="错误码">
          <ul className="ml-4 list-disc space-y-1">
            <li><Badge tone="red">401</Badge> 缺失或错误的 X-Admin-Token / X-Hook-Token / HMAC 签名</li>
            <li><Badge tone="gray">404</Badge> 端点（slug 不存在）或资源不存在</li>
            <li><Badge tone="yellow">429</Badge> 超出端点每分钟限流</li>
            <li><Badge tone="blue">413</Badge> 请求体超过 max_body 上限（内容被截断记录）</li>
          </ul>
          <p>实时事件流：GET /api/stream（SSE），事件类型 request.received / job.created / run.started / run.finished 等。</p>
        </Sec>
      </div>

      <Card hover={false}>
        <CardBody>
          <CardTitle>OpenAPI 原文</CardTitle>
          <details className="mt-2">
            <summary className="cursor-pointer rounded text-[12.5px] font-semibold text-[#0078d4] focus-visible:ring-2 focus-visible:ring-[#0078d4]">
              展开 / 收起
            </summary>
            <pre className="scroll-thin mt-2 max-h-96 overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-3 font-mono text-[11px] leading-relaxed text-[#323130]">
              {OPENAPI}
            </pre>
          </details>
          <div className="mt-3">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => navigator.clipboard.writeText(OPENAPI)}
            >
              复制 JSON
            </Button>
          </div>
        </CardBody>
      </Card>
    </div>
  )
}
