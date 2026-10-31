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
  Select,
  Spinner,
  Textarea,
} from '../components/ui.jsx'

function NewEndpointForm({ onDone }) {
  const [open, setOpen] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [f, setF] = React.useState({
    slug: '',
    title: '',
    description: '',
    auth: 'none',
    secret: '',
    max_body: 1048576,
    retain_days: 30,
    rate_limit: 0,
    enabled: true,
  })

  const set = (k) => (e) => setF({ ...f, [k]: e.target.value })

  if (!open)
    return (
      <Button variant="primary" onClick={() => setOpen(true)}>
        新建端点
      </Button>
    )

  const submit = async () => {
    setBusy(true)
    setErr('')
    try {
      await api.createEndpoint({
        ...f,
        max_body: Number(f.max_body) || 1048576,
        retain_days: Number(f.retain_days) || 30,
        rate_limit: Number(f.rate_limit) || 0,
      })
      setOpen(false)
      setF({ ...f, slug: '', title: '', description: '', secret: '' })
      onDone()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card hover={false} className="w-full">
      <CardBody>
        <CardTitle hint="slug 即对外路径">新建端点</CardTitle>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="slug" hint="只能用于 URL，建议小写短横线">
            <Input value={f.slug} onChange={set('slug')} placeholder="github" />
          </Field>
          <Field label="名称">
            <Input value={f.title} onChange={set('title')} placeholder="GitHub 推送" />
          </Field>
          <Field label="鉴权方式">
            <Select value={f.auth} onChange={set('auth')}>
              <option value="none">无</option>
              <option value="token">令牌（X-Hook-Token）</option>
              <option value="hmac">HMAC-SHA256（X-Hub-Signature-256）</option>
            </Select>
          </Field>
          <Field label="密钥" hint={f.auth === 'none' ? '无鉴权时忽略' : 'HMAC 时作为签名密钥'}>
            <Input
              type="password"
              value={f.secret}
              onChange={set('secret')}
              placeholder="留空则不做校验"
              disabled={f.auth === 'none'}
            />
          </Field>
          <Field label="Body 上限（字节）">
            <Input value={f.max_body} onChange={set('max_body')} />
          </Field>
          <Field label="保留天数">
            <Input value={f.retain_days} onChange={set('retain_days')} />
          </Field>
          <Field label="限流（次/分钟，0=不限）">
            <Input value={f.rate_limit} onChange={set('rate_limit')} />
          </Field>
          <div className="md:col-span-2">
            <Field label="描述">
              <Textarea
                rows={2}
                className="font-sans text-[13px]"
                value={f.description}
                onChange={set('description')}
                placeholder="这个端点接收什么请求"
              />
            </Field>
          </div>
        </div>
        {err ? <p className="mt-3 text-[12.5px] text-[#a4262c]">{err}</p> : null}
        <div className="mt-4 flex gap-2">
          <Button variant="primary" onClick={submit} disabled={busy || !f.slug}>
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

export default function Endpoints() {
  const [eps, setEps] = React.useState(null)
  const [err, setErr] = React.useState('')
  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  const load = React.useCallback(async () => {
    try {
      setEps(await api.endpoints())
      setErr('')
    } catch (e) {
      setErr(e.message)
    }
  }, [])

  React.useEffect(() => {
    load()
  }, [load])

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">端点</h1>
          <p className="mt-1 text-[12.5px] text-[#605e5c]">
            每个端点是一条对外地址，收到的请求全量留档，规则命中时自动生成任务。
          </p>
        </div>
        <NewEndpointForm onDone={load} />
      </div>

      {err ? (
        <Card hover={false} className="p-5">
          <p className="text-[13px] text-[#a4262c]">加载失败：{err}</p>
        </Card>
      ) : null}

      {!eps ? (
        <div className="flex items-center gap-2 py-16 text-[13px] text-[#605e5c]">
          <Spinner /> 加载中…
        </div>
      ) : eps.length === 0 ? (
        <Card hover={false}>
          <Empty title="还没有端点" hint="点右上角「新建端点」创建第一个" />
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {eps.map((e) => (
            <Card key={e.id} className="flex flex-col">
              <CardBody className="flex flex-1 flex-col">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <h2 className="truncate text-[15px] font-semibold text-[#323130]">
                      {e.title || e.slug}
                    </h2>
                    <p className="mt-0.5 truncate font-mono text-[11.5px] text-[#a19f9d]">
                      /hook/{e.slug}
                    </p>
                  </div>
                  <Badge tone={e.enabled ? 'green' : 'gray'}>
                    {e.enabled ? '启用' : '停用'}
                  </Badge>
                </div>

                <p className="mt-3 line-clamp-2 min-h-[34px] text-[12.5px] leading-relaxed text-[#605e5c]">
                  {e.description || '（无描述）'}
                </p>

                <div className="mt-3 flex flex-wrap gap-1.5">
                  <Badge tone="gray">鉴权 {e.auth}</Badge>
                  <Badge tone="gray">保留 {e.retain_days} 天</Badge>
                  <Badge tone="gray">限流 {e.rate_limit || '不限'}</Badge>
                </div>

                <div className="mt-4 flex items-center gap-2 border-t border-[#f0f0f0] pt-3">
                  <Link
                    to={`/endpoints/${e.slug}`}
                    className="inline-flex h-8 items-center rounded-md bg-[#0078d4] px-3 text-[13px] font-semibold text-white shadow-fluent4 transition-all duration-150 ease-out hover:-translate-y-0.5 hover:bg-[#106ebe] hover:shadow-fluent8 active:scale-[0.97] active:translate-y-0 active:shadow-none focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
                  >
                    打开
                  </Link>
                  <code className="min-w-0 flex-1 truncate rounded-md border border-[#e1e1e1] bg-[#faf9f8] px-2 py-1.5 font-mono text-[11px] text-[#605e5c]">
                    {origin}/hook/{e.slug}
                  </code>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
