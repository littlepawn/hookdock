import React from 'react'
import { api } from '../api.js'
import { Link } from '../router.jsx'
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardTitle,
  Empty,
  Field,
  Input,
  Spinner,
  Textarea,
  relTime,
} from '../components/ui.jsx'

function NewJobForm({ onDone }) {
  const [open, setOpen] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [f, setF] = React.useState({
    name: '',
    schedule: '@every 5m',
    exec_argv: '["/bin/sh","-c","date"]',
    payload: '{}',
    timeout_sec: 60,
    max_attempts: 3,
  })
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })

  if (!open)
    return (
      <Button variant="primary" onClick={() => setOpen(true)}>
        新建任务
      </Button>
    )

  const submit = async () => {
    setBusy(true)
    setErr('')
    try {
      await api.createJob({
        ...f,
        timeout_sec: Number(f.timeout_sec) || 60,
        max_attempts: Number(f.max_attempts) || 3,
      })
      setOpen(false)
      setF({ ...f, name: '' })
      onDone()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card hover={false}>
      <CardBody>
        <CardTitle hint="独立于端点的手工任务">新建任务</CardTitle>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="任务名">
            <Input value={f.name} onChange={set('name')} placeholder="每日巡检" />
          </Field>
          <Field label="调度" hint="@every 30s / 0 3 * * *">
            <Input value={f.schedule} onChange={set('schedule')} />
          </Field>
          <Field label="超时（秒）">
            <Input value={f.timeout_sec} onChange={set('timeout_sec')} />
          </Field>
          <Field label="最大尝试次数">
            <Input value={f.max_attempts} onChange={set('max_attempts')} />
          </Field>
          <div className="md:col-span-2">
            <Field label="执行命令（JSON 字符串数组）">
              <Textarea rows={2} value={f.exec_argv} onChange={set('exec_argv')} />
            </Field>
          </div>
          <div className="md:col-span-2">
            <Field label="payload（JSON，供 {{ payload.x }} 使用）">
              <Textarea rows={2} value={f.payload} onChange={set('payload')} />
            </Field>
          </div>
        </div>
        {err ? <p className="mt-3 text-[12.5px] text-[#a4262c]">{err}</p> : null}
        <div className="mt-4 flex gap-2">
          <Button variant="primary" onClick={submit} disabled={busy}>
            {busy ? <Spinner className="border-white/40 border-t-white" /> : null} 创建
          </Button>
          <Button variant="secondary" onClick={() => setOpen(false)}>
            取消
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}

export default function Jobs() {
  const [jobs, setJobs] = React.useState(null)
  const [runs, setRuns] = React.useState([])
  const [err, setErr] = React.useState('')
  const [busyId, setBusyId] = React.useState('')

  const load = React.useCallback(async () => {
    try {
      const [js, rs] = await Promise.all([api.jobs(), api.runs('limit=200')])
      setJobs(js)
      setRuns(rs)
      setErr('')
    } catch (e) {
      setErr(e.message)
    }
  }, [])

  React.useEffect(() => {
    load()
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  }, [load])

  const statsFor = (id) => {
    const mine = runs.filter((r) => r.job_id === id)
    const ok = mine.filter((r) => r.status === 'succeeded').length
    const bad = mine.filter((r) => ['failed', 'timeout', 'dead'].includes(r.status)).length
    const last = mine[0]
    return { total: mine.length, ok, bad, last }
  }

  const act = async (id, fn) => {
    setBusyId(id)
    try {
      await fn()
      await load()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusyId('')
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">任务</h1>
          <p className="mt-1 text-[12.5px] text-[#605e5c]">
            失败按指数退避重试，超时杀整个进程组，连续失败进死信可一键重投。
          </p>
        </div>
        <NewJobForm onDone={load} />
      </div>

      {err ? (
        <Card hover={false} className="p-5">
          <p className="text-[13px] text-[#a4262c]">{err}</p>
        </Card>
      ) : null}

      {!jobs ? (
        <div className="flex items-center gap-2 py-16 text-[13px] text-[#605e5c]">
          <Spinner /> 加载中…
        </div>
      ) : jobs.length === 0 ? (
        <Card hover={false}>
          <Empty title="还没有任务" hint="新建一个，或让端点规则自动生成" />
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {jobs.map((j) => {
            const s = statsFor(j.id)
            const nextIn = j.next_run_at ? j.next_run_at - Math.floor(Date.now() / 1000) : null
            return (
              <Card key={j.id} className="flex flex-col">
                <CardBody className="flex flex-1 flex-col">
                  <div className="flex items-start justify-between gap-3">
                    <Link
                      to={`/jobs/${j.id}`}
                      className="min-w-0 rounded focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                    >
                      <h2 className="truncate text-[15px] font-semibold text-[#323130] hover:text-[#0078d4]">
                        {j.name}
                      </h2>
                      <p className="mt-0.5 truncate font-mono text-[11px] text-[#a19f9d]">
                        {j.id}
                      </p>
                    </Link>
                    <Badge tone={j.enabled ? 'green' : 'gray'}>
                      {j.enabled ? '启用' : '暂停'}
                    </Badge>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-1.5">
                    <Badge tone="blue">{j.schedule}</Badge>
                    <Badge tone="gray">超时 {j.timeout_sec}s</Badge>
                    <Badge tone="gray">重试 {j.max_attempts}</Badge>
                    {j.rule_id ? <Badge tone="yellow">来自规则</Badge> : null}
                  </div>

                  <p className="mt-3 truncate font-mono text-[11px] text-[#605e5c]">
                    {j.exec_argv}
                  </p>

                  <div className="mt-3 grid grid-cols-3 gap-2 rounded-md bg-[#faf9f8] px-3 py-2 text-center">
                    <div>
                      <p className="text-[15px] font-semibold text-[#323130]">{s.total}</p>
                      <p className="text-[10.5px] text-[#a19f9d]">运行</p>
                    </div>
                    <div>
                      <p className="text-[15px] font-semibold text-[#0b8f52]">{s.ok}</p>
                      <p className="text-[10.5px] text-[#a19f9d]">成功</p>
                    </div>
                    <div>
                      <p className="text-[15px] font-semibold text-[#a4262c]">{s.bad}</p>
                      <p className="text-[10.5px] text-[#a19f9d]">失败</p>
                    </div>
                  </div>

                  <p className="mt-2 text-[11.5px] text-[#a19f9d]">
                    {j.enabled
                      ? nextIn !== null && nextIn > -60
                        ? `下次 ${nextIn > 0 ? `${nextIn}s 后` : '即将'}`
                        : '等待调度'
                      : '已暂停'}
                    {s.last ? ` · 上次 ${relTime(s.last.created_at)}` : ''}
                  </p>

                  <div className="mt-3 flex flex-wrap items-center gap-1.5 border-t border-[#f0f0f0] pt-3">
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={busyId === j.id}
                      onClick={() => act(j.id, () => api.runJob(j.id))}
                    >
                      立即运行
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busyId === j.id}
                      onClick={() => act(j.id, () => api.toggleJob(j.id))}
                    >
                      {j.enabled ? '暂停' : '启用'}
                    </Button>
                    <Link
                      to={`/jobs/${j.id}`}
                      className="inline-flex h-8 items-center rounded-md border border-[#e1e1e1] bg-white px-3 text-[13px] font-semibold text-[#323130] shadow-fluent2 transition-all duration-150 ease-out hover:-translate-y-0.5 hover:bg-[#f3f2f1] hover:shadow-fluent4 active:scale-[0.97] active:translate-y-0 active:shadow-none focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                    >
                      历史
                    </Link>
                    <button
                      onClick={() => {
                        if (confirm(`删除任务「${j.name}」及其全部运行记录？`))
                          act(j.id, () => api.deleteJob(j.id))
                      }}
                      className="ml-auto rounded-md px-2 h-8 text-[12px] font-semibold text-[#e81123] transition-all duration-150 ease-out hover:bg-[#fdf3f4] active:scale-[0.97] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                    >
                      删除
                    </button>
                  </div>
                </CardBody>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
