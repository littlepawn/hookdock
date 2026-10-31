import React from 'react'
import { api } from '../api.js'
import { Link } from '../router.jsx'
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
  absTime,
  cx,
  relTime,
} from '../components/ui.jsx'

function RunRow({ r, onRetry, busy }) {
  const [open, setOpen] = React.useState(false)
  const [detail, setDetail] = React.useState(null)

  const toggle = async () => {
    if (!open && !detail) {
      try {
        setDetail(await api.run(r.id))
      } catch {
        /* 忽略 */
      }
    }
    setOpen((v) => !v)
  }

  return (
    <li className="rounded-md border border-[#f0f0f0] transition-all duration-150 ease-out hover:border-[#e1e1e1]">
      <button
        onClick={toggle}
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left transition-all duration-150 ease-out active:scale-[0.995] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2 rounded-md"
      >
        <StatusBadge status={r.status} />
        <span className="text-[11.5px] text-[#605e5c]">
          第 {r.attempt}/{r.max_attempts} 次
        </span>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-[#a19f9d]">
          {r.output?.split('\n')[0] || r.error || '—'}
        </span>
        <span className="shrink-0 text-[11px] text-[#a19f9d]">{relTime(r.created_at)}</span>
        <span
          className={cx(
            'shrink-0 text-[#a19f9d] transition-transform duration-150',
            open && 'rotate-90'
          )}
        >
          <svg viewBox="0 0 24 24" className="size-3.5" fill="none" stroke="currentColor" strokeWidth="2.5">
            <path d="M9 6l6 6-6 6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
      </button>
      {open ? (
        <div className="animate-fade border-t border-[#f0f0f0] px-3 py-3">
          {!detail ? (
            <div className="flex items-center gap-2 py-2 text-[12px] text-[#605e5c]">
              <Spinner /> 加载…
            </div>
          ) : (
            <>
              <dl className="mb-2 grid grid-cols-2 gap-x-4 gap-y-1 text-[11.5px] md:grid-cols-4">
                <div>
                  <dt className="text-[#a19f9d]">退出码</dt>
                  <dd className="font-mono text-[#323130]">
                    {detail.exit_code === null ? '—' : detail.exit_code}
                  </dd>
                </div>
                <div>
                  <dt className="text-[#a19f9d]">开始</dt>
                  <dd className="text-[#323130]">{absTime(detail.started_at)}</dd>
                </div>
                <div>
                  <dt className="text-[#a19f9d]">结束</dt>
                  <dd className="text-[#323130]">{absTime(detail.finished_at)}</dd>
                </div>
                <div>
                  <dt className="text-[#a19f9d]">下次重试</dt>
                  <dd className="text-[#323130]">
                    {detail.next_attempt_at ? relTime(detail.next_attempt_at) : '—'}
                  </dd>
                </div>
              </dl>
              {detail.error ? (
                <p className="mb-2 rounded-md bg-[#fdf3f4] px-2.5 py-1.5 text-[12px] text-[#a4262c]">
                  {detail.error}
                </p>
              ) : null}
              <pre className="scroll-thin max-h-64 overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap break-all text-[#323130]">
                {detail.output || '（无输出）'}
                {detail.truncated ? '\n…（输出已截断）' : ''}
              </pre>
              <div className="mt-2 flex gap-2">
                <CopyButton text={detail.output || ''} label="复制输出" />
                {['dead', 'failed', 'timeout'].includes(r.status) ? (
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={busy}
                    onClick={() => onRetry(r.id)}
                  >
                    重新入队
                  </Button>
                ) : null}
              </div>
            </>
          )}
        </div>
      ) : null}
    </li>
  )
}

export default function JobDetail({ id }) {
  const [job, setJob] = React.useState(null)
  const [runs, setRuns] = React.useState(null)
  const [err, setErr] = React.useState('')
  const [busy, setBusy] = React.useState(false)

  const load = React.useCallback(async () => {
    try {
      const [j, rs] = await Promise.all([api.job(id), api.jobRuns(id)])
      setJob(j)
      setRuns(rs)
      setErr('')
    } catch (e) {
      setErr(e.message)
    }
  }, [id])

  React.useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const act = async (fn) => {
    setBusy(true)
    try {
      await fn()
      await load()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  if (err)
    return (
      <Card hover={false} className="p-6">
        <p className="text-[13.5px] font-semibold text-[#a4262c]">加载失败：{err}</p>
        <Link to="/jobs" className="mt-3 inline-block text-[13px] text-[#0078d4] hover:underline">
          返回任务列表
        </Link>
      </Card>
    )

  if (!job)
    return (
      <div className="flex items-center gap-2 py-20 text-[13px] text-[#605e5c]">
        <Spinner /> 加载中…
      </div>
    )

  const nextIn = job.next_run_at ? job.next_run_at - Math.floor(Date.now() / 1000) : null

  return (
    <div className="space-y-5">
      <div className="flex items-center gap-1.5 text-[12.5px] text-[#a19f9d]">
        <Link to="/jobs" className="hover:text-[#0078d4] rounded focus-visible:ring-2 focus-visible:ring-[#0078d4]">
          任务
        </Link>
        <span>/</span>
        <span className="text-[#605e5c]">{job.name}</span>
      </div>

      <Card hover={false}>
        <CardBody>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">
                  {job.name}
                </h1>
                <Badge tone={job.enabled ? 'green' : 'gray'}>
                  {job.enabled ? '启用' : '暂停'}
                </Badge>
                <Badge tone="blue">{job.schedule}</Badge>
              </div>
              <p className="mt-1 font-mono text-[11.5px] text-[#a19f9d]">{job.id}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="primary"
                size="sm"
                disabled={busy}
                onClick={() => act(() => api.runJob(job.id))}
              >
                立即运行
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={busy}
                onClick={() => act(() => api.toggleJob(job.id))}
              >
                {job.enabled ? '暂停' : '启用'}
              </Button>
            </div>
          </div>

          <div className="mt-4 grid gap-3 md:grid-cols-2">
            <div>
              <p className="mb-1 text-[11.5px] font-semibold text-[#605e5c]">执行命令</p>
              <pre className="scroll-thin overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-2.5 font-mono text-[11.5px] text-[#323130] whitespace-pre-wrap break-all">
                {job.exec_argv}
              </pre>
            </div>
            <div>
              <p className="mb-1 text-[11.5px] font-semibold text-[#605e5c]">payload</p>
              <pre className="scroll-thin max-h-24 overflow-auto rounded-md border border-[#e1e1e1] bg-[#faf9f8] p-2.5 font-mono text-[11.5px] text-[#323130] whitespace-pre-wrap break-all">
                {job.payload}
              </pre>
            </div>
          </div>

          <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-[#f0f0f0] pt-3 text-[12px] md:grid-cols-4">
            <div>
              <dt className="text-[#a19f9d]">下次执行</dt>
              <dd className="text-[#323130]">
                {job.enabled
                  ? nextIn === null
                    ? '—'
                    : nextIn > 0
                      ? `${nextIn} 秒后`
                      : '即将'
                  : '已暂停'}
              </dd>
            </div>
            <div>
              <dt className="text-[#a19f9d]">超时</dt>
              <dd className="text-[#323130]">{job.timeout_sec} 秒</dd>
            </div>
            <div>
              <dt className="text-[#a19f9d]">最大尝试</dt>
              <dd className="text-[#323130]">{job.max_attempts} 次</dd>
            </div>
            <div>
              <dt className="text-[#a19f9d]">来源</dt>
              <dd className="text-[#323130]">
                {job.rule_id ? `规则 #${job.rule_id}` : '手工创建'}
              </dd>
            </div>
          </dl>
        </CardBody>
      </Card>

      <Card hover={false}>
        <CardBody>
          <CardTitle hint={`${runs ? runs.length : 0} 条`}>运行历史</CardTitle>
          {!runs ? (
            <div className="flex items-center gap-2 py-8 text-[13px] text-[#605e5c]">
              <Spinner /> 加载中…
            </div>
          ) : runs.length === 0 ? (
            <Empty title="还没有运行记录" hint="点「立即运行」产生第一条" />
          ) : (
            <ul className="space-y-1.5">
              {runs.map((r) => (
                <RunRow
                  key={r.id}
                  r={r}
                  busy={busy}
                  onRetry={(rid) => act(() => api.retryRun(rid))}
                />
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
