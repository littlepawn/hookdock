import React from 'react'
import { api, subscribe } from '../api.js'
import { Link, useRouter } from '../router.jsx'
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardTitle,
  CopyButton,
  Empty,
  Spinner,
  StatusBadge,
  cx,
  relTime,
} from '../components/ui.jsx'

function Stat({ label, value, sub, tone = 'blue', icon }) {
  const toneMap = {
    blue: 'bg-[#0078d4]/10 text-[#0078d4]',
    green: 'bg-[#00cc6a]/12 text-[#0b8f52]',
    yellow: 'bg-[#ffb900]/18 text-[#8a6800]',
    red: 'bg-[#e81123]/10 text-[#e81123]',
  }
  return (
    <Card className="p-5">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-[12.5px] font-semibold text-[#605e5c]">{label}</p>
          <p className="mt-1.5 text-[26px] font-semibold leading-none tracking-tight text-[#323130]">
            {value}
          </p>
          {sub ? <p className="mt-2 text-[11.5px] text-[#a19f9d]">{sub}</p> : null}
        </div>
        <span
          className={cx(
            'grid size-8 place-items-center rounded-md transition-transform duration-150 ease-out group-hover:scale-105',
            toneMap[tone]
          )}
        >
          {icon}
        </span>
      </div>
    </Card>
  )
}

function Spark({ data }) {
  const max = Math.max(1, ...data)
  return (
    <div className="flex h-24 items-end gap-[3px]">
      {data.map((v, i) => (
        <div
          key={i}
          className="group/bar relative flex-1 rounded-sm bg-[#0078d4]/25 transition-all duration-200 ease-out hover:bg-[#0078d4]"
          style={{ height: `${Math.max(3, (v / max) * 100)}%` }}
          title={`${23 - i} 小时前 · ${v} 条`}
        />
      ))}
    </div>
  )
}

export default function Overview() {
  const { navigate } = useRouter()
  const [data, setData] = React.useState(null)
  const [err, setErr] = React.useState('')
  const [feed, setFeed] = React.useState([])
  const [pulse, setPulse] = React.useState(0)

  const load = React.useCallback(async () => {
    try {
      const d = await api.overview()
      setData(d)
      setErr('')
    } catch (e) {
      setErr(e.message)
    }
  }, [])

  React.useEffect(() => {
    load()
    const t = setInterval(load, 15000)
    return () => clearInterval(t)
  }, [load])

  React.useEffect(() => {
    const off = subscribe((ev) => {
      if (ev.type === 'request.received' || ev.type === 'run.finished' || ev.type === 'run.retry' || ev.type === 'job.created') {
        setFeed((f) => [{ ...ev, key: `${ev.type}-${Date.now()}-${Math.random()}` }, ...f].slice(0, 14))
        setPulse((p) => p + 1)
      }
    })
    return off
  }, [])

  React.useEffect(() => {
    if (pulse > 0) load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pulse])

  if (err)
    return (
      <Card className="p-6">
        <p className="text-[13.5px] font-semibold text-[#a4262c]">加载失败：{err}</p>
        <p className="mt-1 text-[12.5px] text-[#605e5c]">
          若服务端启用了管理令牌，请点右上角「令牌」填入。
        </p>
        <Button className="mt-4" variant="primary" onClick={load}>
          重试
        </Button>
      </Card>
    )

  if (!data)
    return (
      <div className="flex items-center gap-2 py-20 text-[13px] text-[#605e5c]">
        <Spinner /> 加载中…
      </div>
    )

  const st = data.stats
  const rate = st.runs_24h > 0 ? Math.round((st.ok_24h / st.runs_24h) * 100) : 100
  const primary = data.endpoints[0]
  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  return (
    <div className="space-y-5">
      {/* Hero */}
      <Card className="overflow-hidden" hover={false}>
        <div className="relative p-6 md:p-7">
          <div
            className="pointer-events-none absolute inset-0 opacity-70"
            style={{
              background:
                'radial-gradient(90% 120% at 100% 0%, rgba(0,120,212,0.12) 0%, rgba(0,120,212,0) 55%)',
            }}
          />
          <div className="relative">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="blue">运行中</Badge>
              <span className="text-[12px] text-[#a19f9d]">
                数据每 15 秒刷新 · 事件实时推送
              </span>
            </div>
            <h1 className="mt-3 text-[24px] font-semibold tracking-tight text-[#323130] md:text-[28px]">
              Webhook 收进来，规则自动变成可靠定时任务
            </h1>
            <p className="mt-2 max-w-2xl text-[13.5px] leading-relaxed text-[#605e5c]">
              任意服务把请求 POST 到端点地址即可留档；命中规则的请求会立刻生成带重试、超时与死信重投的任务。
            </p>
            {primary ? (
              <div className="mt-5 flex flex-wrap items-center gap-2">
                <code className="rounded-md border border-[#e1e1e1] bg-[#faf9f8] px-3 py-1.5 font-mono text-[12px] text-[#323130]">
                  {origin}/hook/{primary.slug}
                </code>
                <CopyButton text={`${origin}/hook/${primary.slug}`} label="复制地址" />
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => navigate(`/endpoints/${primary.slug}`)}
                >
                  查看端点
                </Button>
              </div>
            ) : null}
          </div>
        </div>
      </Card>

      {/* 指标 */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat
          label="24 小时请求"
          value={st.requests_24h}
          sub={`最近 1 小时 ${st.requests_1h} 条`}
          icon={
            <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M12 19V5M5 12l7-7 7 7" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          }
        />
        <Stat
          label="24 小时运行"
          value={st.runs_24h}
          sub={`成功 ${st.ok_24h} · 失败 ${st.fail_24h}`}
          tone="green"
          icon={
            <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="8.5" />
              <path d="M12 7.5V12l3 2" strokeLinecap="round" />
            </svg>
          }
        />
        <Stat
          label="成功率"
          value={`${rate}%`}
          sub="按 24 小时运行统计"
          tone={rate >= 95 ? 'green' : rate >= 80 ? 'yellow' : 'red'}
          icon={
            <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M5 13l4 4L19 7" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          }
        />
        <Stat
          label="待处理 / 死信"
          value={`${st.queued} / ${st.dead}`}
          sub={`启用任务 ${st.jobs_enabled} 个`}
          tone={st.dead > 0 ? 'red' : 'yellow'}
          icon={
            <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M12 8v5M12 16.5h.01" strokeLinecap="round" />
              <circle cx="12" cy="12" r="8.5" />
            </svg>
          }
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-5">
        {/* 实时流 */}
        <Card className="lg:col-span-3" hover={false}>
          <CardBody>
            <CardTitle hint="SSE 实时">事件流</CardTitle>
            {feed.length === 0 ? (
              <Empty
                title="等待事件"
                hint="向任意端点发一条请求，或手动运行一个任务，这里会实时出现"
              />
            ) : (
              <ul className="space-y-1.5">
                {feed.map((ev) => (
                  <li
                    key={ev.key}
                    className="animate-rise flex items-start gap-2.5 rounded-md border border-[#f0f0f0] bg-[#faf9f8] px-3 py-2"
                  >
                    <span className="mt-[5px]">
                      {ev.type === 'request.received' ? (
                        <span className="block size-1.5 rounded-full bg-[#0078d4]" />
                      ) : ev.type === 'job.created' ? (
                        <span className="block size-1.5 rounded-full bg-[#ffb900]" />
                      ) : ev.data?.status === 'succeeded' ? (
                        <span className="block size-1.5 rounded-full bg-[#00cc6a]" />
                      ) : (
                        <span className="block size-1.5 rounded-full bg-[#e81123]" />
                      )}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[12.5px] text-[#323130]">
                        {ev.type === 'request.received' && (
                          <>
                            <b className="font-semibold">{ev.data.endpoint}</b> 收到请求 ·{' '}
                            <span className="font-mono text-[11.5px] text-[#605e5c]">
                              {ev.data.method} {ev.data.body_bytes}B
                            </span>
                            {ev.data.matched_jobs?.length > 0 && (
                              <span className="ml-1 text-[#005a9e]">
                                · 命中 {ev.data.matched_jobs.length} 条规则
                              </span>
                            )}
                          </>
                        )}
                        {ev.type === 'job.created' && (
                          <>
                            新建任务 <b className="font-semibold">{ev.data.job_name}</b>
                            {ev.data.rule ? ` · 规则「${ev.data.rule}」` : ''}
                          </>
                        )}
                        {ev.type === 'run.finished' && (
                          <>
                            任务 <b className="font-semibold">{ev.data.job_name || ev.data.job_id}</b>{' '}
                            运行 {ev.data.status}
                          </>
                        )}
                        {ev.type === 'run.retry' && (
                          <>
                            任务运行失败，将在 {Math.max(0, ev.data.next_attempt_at - Math.floor(Date.now() / 1000))} 秒后重试
                            （第 {ev.data.attempt} 次）
                          </>
                        )}
                      </p>
                      {ev.type === 'request.received' && ev.data.preview ? (
                        <p className="mt-0.5 truncate font-mono text-[11px] text-[#a19f9d]">
                          {ev.data.preview}
                        </p>
                      ) : null}
                    </div>
                    <span className="shrink-0 text-[11px] text-[#a19f9d]">
                      {relTime(ev.at)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>

        {/* 24h 曲线 + 端点 */}
        <div className="space-y-4 lg:col-span-2">
          <Card hover={false}>
            <CardBody>
              <CardTitle hint="近 24 小时">请求分布</CardTitle>
              <Spark data={st.hourly || []} />
              <div className="mt-2 flex justify-between text-[11px] text-[#a19f9d]">
                <span>24h 前</span>
                <span>现在</span>
              </div>
            </CardBody>
          </Card>

          <Card hover={false}>
            <CardBody>
              <CardTitle hint={`${data.endpoints.length} 个`}>端点</CardTitle>
              {data.endpoints.length === 0 ? (
                <Empty title="还没有端点" hint="到「端点」页新建一个" />
              ) : (
                <ul className="space-y-1.5">
                  {data.endpoints.map((e) => (
                    <li key={e.id}>
                      <Link
                        to={`/endpoints/${e.slug}`}
                        className="group flex items-center justify-between rounded-md border border-[#f0f0f0] px-3 py-2 transition-all duration-150 ease-out hover:-translate-y-0.5 hover:border-[#e1e1e1] hover:bg-[#faf9f8] hover:shadow-fluent4 focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                      >
                        <span className="min-w-0">
                          <span className="block truncate text-[13px] font-semibold text-[#323130]">
                            {e.title || e.slug}
                          </span>
                          <span className="block truncate font-mono text-[11px] text-[#a19f9d]">
                            /hook/{e.slug}
                          </span>
                        </span>
                        <Badge tone={e.enabled ? 'green' : 'gray'}>
                          {e.enabled ? '启用' : '停用'}
                        </Badge>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </div>
      </div>

      {/* 最近运行 */}
      <Card hover={false}>
        <CardBody>
          <CardTitle hint="最近 8 条">运行记录</CardTitle>
          {data.recent_runs.length === 0 ? (
            <Empty title="还没有运行" hint="在「任务」页点「立即运行」试试" />
          ) : (
            <div className="overflow-x-auto scroll-thin">
              <table className="w-full text-[12.5px]">
                <thead>
                  <tr className="text-left text-[11.5px] uppercase tracking-wide text-[#a19f9d]">
                    <th className="pb-2 pr-3 font-semibold">任务</th>
                    <th className="pb-2 pr-3 font-semibold">状态</th>
                    <th className="pb-2 pr-3 font-semibold">尝试</th>
                    <th className="pb-2 pr-3 font-semibold">时间</th>
                    <th className="pb-2 font-semibold">输出</th>
                  </tr>
                </thead>
                <tbody>
                  {data.recent_runs.map((r) => (
                    <tr
                      key={r.id}
                      className="border-t border-[#f0f0f0] transition-colors duration-150 hover:bg-[#faf9f8]"
                    >
                      <td className="py-2 pr-3">
                        <Link
                          to={`/jobs/${r.job_id}`}
                          className="font-semibold text-[#323130] hover:text-[#0078d4] focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2 rounded"
                        >
                          {r.job_name || r.job_id}
                        </Link>
                      </td>
                      <td className="py-2 pr-3">
                        <StatusBadge status={r.status} />
                      </td>
                      <td className="py-2 pr-3 text-[#605e5c]">
                        {r.attempt}/{r.max_attempts}
                      </td>
                      <td className="py-2 pr-3 text-[#605e5c]">{relTime(r.created_at)}</td>
                      <td className="max-w-[280px] truncate py-2 font-mono text-[11px] text-[#a19f9d]">
                        {r.output?.split('\n')[0] || r.error || '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
