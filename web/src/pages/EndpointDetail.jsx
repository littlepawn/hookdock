import React from 'react'
import { api } from '../api.js'
import { Link, useRouter } from '../router.jsx'
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardTitle,
  CopyButton,
  Empty,
  Field,
  Input,
  Select,
  Spinner,
  Textarea,
  absTime,
  cx,
  relTime,
} from '../components/ui.jsx'

/* ---------------- 代码块 ---------------- */
function Code({ children, className }) {
  return (
    <pre
      className={cx(
        'scroll-thin max-h-72 overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-3',
        'font-mono text-[11.5px] leading-relaxed text-[#323130] whitespace-pre-wrap break-all',
        className
      )}
    >
      {children}
    </pre>
  )
}

function pretty(s) {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

/* ---------------- 请求详情 ---------------- */
function RequestDetail({ id, onClose }) {
  const [r, setR] = React.useState(null)
  const [tab, setTab] = React.useState('body')
  const [busy, setBusy] = React.useState(false)
  const [msg, setMsg] = React.useState('')

  React.useEffect(() => {
    setR(null)
    setMsg('')
    api.request(id).then(setR).catch((e) => setMsg(e.message))
  }, [id])

  const makeJob = async () => {
    setBusy(true)
    setMsg('')
    try {
      const job = await api.makeJob(id, {
        name: `来自 ${id}`,
        schedule: '@every 5m',
        exec_argv: JSON.stringify(['/bin/sh', '-c', 'echo received; echo "$HOOKDOCK_PAYLOAD"']),
        timeout_sec: 60,
        max_attempts: 3,
      })
      setMsg(`已创建任务 ${job.name}（每 5 分钟），可在「任务」页调整`)
    } catch (e) {
      setMsg('创建失败：' + e.message)
    } finally {
      setBusy(false)
    }
  }

  const tabs = [
    ['body', '原始 body'],
    ['payload', '解析后 payload'],
    ['headers', 'Headers'],
    ['meta', '元信息'],
  ]

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div className="absolute inset-0 bg-[#323130]/25 backdrop-blur-sm animate-fade" onClick={onClose} />
      <div className="relative flex h-full w-full max-w-2xl flex-col border-l border-white/60 bg-white/70 backdrop-blur-xl shadow-fluent64 animate-rise">
        <div className="flex items-center justify-between border-b border-[#e1e1e1] px-5 py-3">
          <div className="min-w-0">
            <p className="truncate font-mono text-[12.5px] font-semibold text-[#323130]">{id}</p>
            {r ? (
              <p className="mt-0.5 text-[11.5px] text-[#a19f9d]">
                {r.method} {r.path} · {absTime(r.received_at)} · {r.remote_ip}
              </p>
            ) : null}
          </div>
          <Button variant="secondary" size="sm" onClick={onClose}>
            关闭
          </Button>
        </div>

        {!r ? (
          <div className="flex flex-1 items-center justify-center gap-2 text-[13px] text-[#605e5c]">
            <Spinner /> 加载中…
          </div>
        ) : (
          <>
            <div className="flex gap-1 border-b border-[#e1e1e1] px-4 py-2">
              {tabs.map(([k, label]) => (
                <button
                  key={k}
                  onClick={() => setTab(k)}
                  className={cx(
                    'rounded-md px-2.5 h-7 text-[12.5px] font-semibold transition-all duration-150 ease-out active:scale-[0.97]',
                    'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2',
                    tab === k
                      ? 'bg-[#0078d4]/10 text-[#005a9e]'
                      : 'text-[#605e5c] hover:bg-[#f3f2f1]'
                  )}
                >
                  {label}
                </button>
              ))}
            </div>

            <div className="flex-1 overflow-auto scroll-thin p-5">
              {tab === 'body' && (
                <>
                  {r.truncated ? (
                    <p className="mb-2 text-[12px] text-[#8a6800]">
                      已按端点上限截断，原始 {r.body_bytes} 字节
                    </p>
                  ) : null}
                  <Code>{r.body || '（空）'}</Code>
                </>
              )}
              {tab === 'payload' && <Code>{pretty(r.payload)}</Code>}
              {tab === 'headers' && <Code>{pretty(r.headers)}</Code>}
              {tab === 'meta' && (
                <dl className="space-y-2 text-[12.5px]">
                  {[
                    ['方法', r.method],
                    ['路径', r.path],
                    ['Query', r.query || '—'],
                    ['Content-Type', r.content_type || '—'],
                    ['来源 IP', r.remote_ip],
                    ['User-Agent', r.user_agent || '—'],
                    ['鉴权', `${r.auth_kind} · ${r.auth_ok ? '通过' : '失败'}`],
                    ['字节数', `${r.body_bytes}`],
                    ['接收时间', absTime(r.received_at)],
                  ].map(([k, v]) => (
                    <div key={k} className="flex gap-3">
                      <dt className="w-28 shrink-0 text-[#a19f9d]">{k}</dt>
                      <dd className="min-w-0 flex-1 break-all text-[#323130]">{v}</dd>
                    </div>
                  ))}
                </dl>
              )}
            </div>

            <div className="border-t border-[#e1e1e1] px-5 py-3">
              {msg ? <p className="mb-2 text-[12px] text-[#005a9e]">{msg}</p> : null}
              <div className="flex flex-wrap gap-2">
                <Button variant="primary" size="sm" onClick={makeJob} disabled={busy}>
                  {busy ? <Spinner className="border-white/40 border-t-white" /> : null} 转成定时任务
                </Button>
                <CopyButton text={r.body} label="复制 body" />
                <CopyButton text={r.payload} label="复制 payload" />
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

/* ---------------- 规则 ---------------- */
function RuleForm({ slug, onDone }) {
  const [open, setOpen] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [f, setF] = React.useState({
    name: '',
    source: 'body',
    path: 'action',
    op: 'eq',
    value: 'deploy',
    job_name: '',
    schedule: '@every 5m',
    exec_argv: '["/bin/sh","-c","echo hi"]',
    timeout_sec: 60,
    max_attempts: 3,
    mode: 'recurring',
    enabled: true,
  })
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })

  if (!open)
    return (
      <Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
        新增规则
      </Button>
    )

  const submit = async () => {
    setBusy(true)
    setErr('')
    try {
      await api.createRule(slug, {
        ...f,
        timeout_sec: Number(f.timeout_sec) || 60,
        max_attempts: Number(f.max_attempts) || 3,
      })
      setOpen(false)
      onDone()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card hover={false} className="mt-3">
      <CardBody>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="规则名">
            <Input value={f.name} onChange={set('name')} placeholder="main 分支推送" />
          </Field>
          <Field label="匹配来源">
            <Select value={f.source} onChange={set('source')}>
              <option value="body">body（JSON 字段）</option>
              <option value="query">query 参数</option>
              <option value="header">header</option>
            </Select>
          </Field>
          <Field
            label="字段路径"
            hint={f.source === 'body' ? '如 action 或 user.login' : '键名'}
          >
            <Input value={f.path} onChange={set('path')} />
          </Field>
          <Field label="比较方式">
            <Select value={f.op} onChange={set('op')}>
              <option value="eq">等于</option>
              <option value="contains">包含</option>
              <option value="prefix">前缀</option>
              <option value="regex">正则</option>
              <option value="exists">存在</option>
            </Select>
          </Field>
          <Field label="比较值">
            <Input value={f.value} onChange={set('value')} />
          </Field>
          <Field label="生成的任务名">
            <Input value={f.job_name} onChange={set('job_name')} placeholder="部署检查" />
          </Field>
          <Field
            label="命中后行为"
            hint={f.mode === 'recurring' ? '同一规则只维护一条任务' : '每次请求各建一条一次性任务'}
          >
            <Select value={f.mode} onChange={set('mode')}>
              <option value="recurring">常驻任务（按调度循环执行）</option>
              <option value="per_event">每次事件各建一条</option>
            </Select>
          </Field>
          <Field label="调度" hint="@every 30s / @every 5m / 5 段 cron">
            <Input value={f.schedule} onChange={set('schedule')} />
          </Field>
          <Field label="超时（秒）">
            <Input value={f.timeout_sec} onChange={set('timeout_sec')} />
          </Field>
          <div className="md:col-span-2">
            <Field label="执行命令（JSON 字符串数组）" hint="支持 {{ payload.x }} 模板">
              <Textarea rows={2} value={f.exec_argv} onChange={set('exec_argv')} />
            </Field>
          </div>
        </div>
        {err ? <p className="mt-3 text-[12.5px] text-[#a4262c]">{err}</p> : null}
        <div className="mt-4 flex gap-2">
          <Button variant="primary" size="sm" onClick={submit} disabled={busy}>
            {busy ? <Spinner className="border-white/40 border-t-white" /> : null} 保存规则
          </Button>
          <Button variant="secondary" size="sm" onClick={() => setOpen(false)}>
            取消
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}

const OP_LABEL = { eq: '等于', contains: '包含', prefix: '前缀', regex: '正则', exists: '存在' }

/* ---------------- 页面 ---------------- */
export default function EndpointDetail({ slug }) {
  const { navigate } = useRouter()
  const [ep, setEp] = React.useState(null)
  const [reqs, setReqs] = React.useState(null)
  const [rules, setRules] = React.useState([])
  const [tab, setTab] = React.useState('requests')
  const [sel, setSel] = React.useState(null)
  const [err, setErr] = React.useState('')
  const [live, setLive] = React.useState(0)

  const load = React.useCallback(async () => {
    try {
      const [e, rs, rl] = await Promise.all([
        api.endpoint(slug),
        api.requests(slug, 50),
        api.rules(slug),
      ])
      setEp(e)
      setReqs(rs)
      setRules(rl)
      setErr('')
    } catch (e) {
      setErr(e.message)
    }
  }, [slug])

  React.useEffect(() => {
    load()
  }, [load, live])

  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const url = `${origin}/hook/${slug}`
  const curl = `curl -X POST ${url} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"action":"deploy","repo":"demo"}'`

  if (err)
    return (
      <Card hover={false} className="p-6">
        <p className="text-[13.5px] font-semibold text-[#a4262c]">加载失败：{err}</p>
        <Button className="mt-4" variant="primary" onClick={() => navigate('/endpoints')}>
          返回端点列表
        </Button>
      </Card>
    )

  if (!ep)
    return (
      <div className="flex items-center gap-2 py-20 text-[13px] text-[#605e5c]">
        <Spinner /> 加载中…
      </div>
    )

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-1.5 text-[12.5px] text-[#a19f9d]">
        <Link to="/endpoints" className="hover:text-[#0078d4] focus-visible:ring-2 focus-visible:ring-[#0078d4] rounded">
          端点
        </Link>
        <span>/</span>
        <span className="text-[#605e5c]">{ep.title || ep.slug}</span>
      </div>

      <Card hover={false}>
        <CardBody>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">
                  {ep.title || ep.slug}
                </h1>
                <Badge tone={ep.enabled ? 'green' : 'gray'}>
                  {ep.enabled ? '启用' : '停用'}
                </Badge>
                <Badge tone="gray">鉴权 {ep.auth}</Badge>
              </div>
              <p className="mt-1.5 max-w-2xl text-[12.5px] leading-relaxed text-[#605e5c]">
                {ep.description || '（无描述）'}
              </p>
            </div>
            <Button variant="secondary" size="sm" onClick={() => setLive((v) => v + 1)}>
              刷新
            </Button>
          </div>

          <div className="mt-4 grid gap-3 md:grid-cols-2">
            <div>
              <p className="mb-1.5 text-[11.5px] font-semibold text-[#605e5c]">接收地址</p>
              <div className="flex items-center gap-1">
                <code className="min-w-0 flex-1 truncate rounded-md border border-[#e1e1e1] bg-[#faf9f8] px-2.5 py-1.5 font-mono text-[11.5px] text-[#323130]">
                  {url}
                </code>
                <CopyButton text={url} />
              </div>
            </div>
            <div>
              <p className="mb-1.5 text-[11.5px] font-semibold text-[#605e5c]">调用示例</p>
              <Code className="max-h-24">{curl}</Code>
            </div>
          </div>
        </CardBody>
      </Card>

      <div className="flex gap-1">
        {[
          ['requests', `请求 ${reqs ? reqs.length : ''}`],
          ['rules', `规则 ${rules.length}`],
        ].map(([k, label]) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={cx(
              'h-8 rounded-md px-3 text-[13px] font-semibold transition-all duration-150 ease-out active:scale-[0.97]',
              'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2',
              tab === k
                ? 'bg-white text-[#005a9e] shadow-fluent4 border border-[#e1e1e1]'
                : 'text-[#605e5c] hover:bg-[#ebebeb]'
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'requests' ? (
        <Card hover={false}>
          <CardBody>
            <CardTitle hint="点击查看详情">收到的请求</CardTitle>
            {!reqs ? (
              <div className="flex items-center gap-2 py-10 text-[13px] text-[#605e5c]">
                <Spinner /> 加载中…
              </div>
            ) : reqs.length === 0 ? (
              <Empty title="还没有请求" hint={`向 ${url} 发一条试试`} />
            ) : (
              <ul className="divide-y divide-[#f0f0f0]">
                {reqs.map((r) => (
                  <li key={r.id}>
                    <button
                      onClick={() => setSel(r.id)}
                      className="group flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left transition-all duration-150 ease-out hover:bg-[#faf9f8] active:scale-[0.995] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                    >
                      <Badge tone={r.auth_ok ? 'blue' : 'red'}>{r.method}</Badge>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-mono text-[12px] text-[#323130]">
                          {r.body ? r.body.slice(0, 160) : '（空 body）'}
                        </span>
                        <span className="mt-0.5 block truncate text-[11px] text-[#a19f9d]">
                          {r.remote_ip} · {r.body_bytes}B · {r.content_type || 'unknown'}
                        </span>
                      </span>
                      <span className="shrink-0 text-[11px] text-[#a19f9d]">
                        {relTime(r.received_at)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      ) : (
        <Card hover={false}>
          <CardBody>
            <div className="flex items-center justify-between">
              <CardTitle hint="命中即生成任务">规则</CardTitle>
              <RuleForm slug={slug} onDone={load} />
            </div>
            {rules.length === 0 ? (
              <Empty title="还没有规则" hint="加一条规则，让命中的请求自动变成定时任务" />
            ) : (
              <ul className="space-y-2">
                {rules.map((r) => (
                  <li
                    key={r.id}
                    className="rounded-md border border-[#f0f0f0] bg-[#faf9f8] px-3 py-2.5"
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge tone={r.enabled ? 'green' : 'gray'}>
                        {r.enabled ? '启用' : '停用'}
                      </Badge>
                      <span className="text-[13px] font-semibold text-[#323130]">{r.name}</span>
                      <span className="font-mono text-[11.5px] text-[#605e5c]">
                        {r.source}.{r.path} {OP_LABEL[r.op] || r.op} “{r.value}”
                      </span>
                      <span className="ml-auto flex items-center gap-2">
                        {r.mode === 'per_event' ? (
                          <Badge tone="yellow">每次事件</Badge>
                        ) : (
                          <Badge tone="blue">{r.schedule}</Badge>
                        )}
                        <button
                          onClick={async () => {
                            await api.deleteRule(r.id)
                            load()
                          }}
                          className="rounded-md px-2 py-1 text-[11.5px] font-semibold text-[#e81123] transition-all duration-150 ease-out hover:bg-[#fdf3f4] active:scale-[0.97] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                        >
                          删除
                        </button>
                      </span>
                    </div>
                    <p className="mt-1 truncate font-mono text-[11px] text-[#a19f9d]">
                      {r.exec_argv}
                    </p>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      )}

      {sel ? <RequestDetail id={sel} onClose={() => setSel(null)} /> : null}
    </div>
  )
}
