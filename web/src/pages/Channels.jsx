import React from 'react'
import { api } from '../api.js'
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
  cx,
  relTime,
} from '../components/ui.jsx'

const GROUP_LABEL = { domestic: '国内', international: '国际', generic: '通用' }

function defaultsFor(def) {
  const cfg = {}
  for (const f of def?.fields || []) {
    if (f.default) cfg[f.key] = f.default
    else cfg[f.key] = ''
  }
  return cfg
}

function ConfigField({ f, value, onChange }) {
  if (f.type === 'select') {
    return (
      <Field label={f.label} hint={f.help}>
        <Select value={value || ''} onChange={(e) => onChange(e.target.value)}>
          {!f.required ? <option value="">默认</option> : null}
          {(f.options || []).map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </Select>
      </Field>
    )
  }
  if (f.type === 'textarea') {
    return (
      <Field label={f.label} hint={f.help}>
        <Textarea
          rows={3}
          value={value || ''}
          placeholder={f.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      </Field>
    )
  }
  const inputType = f.type === 'password' || f.secret ? 'password' : f.type === 'number' ? 'number' : 'text'
  return (
    <Field label={f.label} hint={f.help}>
      <Input
        type={inputType}
        value={value || ''}
        placeholder={f.placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  )
}

function ChannelForm({ channel, types, onDone, onCancel, trigger }) {
  const editing = !!channel
  const [open, setOpen] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [name, setName] = React.useState('')
  const [type, setType] = React.useState('bark')
  const [config, setConfig] = React.useState({})
  const [enabled, setEnabled] = React.useState(true)

  const def = types.find((t) => t.type === type)
  const setCfg = (k, v) => setConfig((c) => ({ ...c, [k]: v }))

  const openForm = () => {
    if (editing) {
      setName(channel.name)
      setType(channel.type)
      setConfig({ ...(channel.config || {}) })
      setEnabled(!!channel.enabled)
    } else {
      setName('')
      setType(types[0]?.type || 'bark')
      setConfig(defaultsFor(types[0]))
      setEnabled(true)
    }
    setErr('')
    setOpen(true)
  }

  const changeType = (v) => {
    setType(v)
    const d = types.find((t) => t.type === v)
    setConfig(editing && v === channel.type ? { ...(channel.config || {}) } : defaultsFor(d))
  }

  if (!open) {
    return trigger ? (
      <span onClick={openForm}>{trigger}</span>
    ) : (
      <Button variant="primary" onClick={openForm}>
        新建渠道
      </Button>
    )
  }

  const submit = async () => {
    setBusy(true)
    setErr('')
    const body = { name, type, config, enabled }
    try {
      if (editing) await api.updateChannel(channel.id, body)
      else await api.createChannel(body)
      setOpen(false)
      onDone && onDone()
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card hover={false}>
      <CardBody>
        <CardTitle hint={editing ? `渠道 ${channel.id}` : '配置一次，可被多个任务复用'}>
          {editing ? '编辑推送渠道' : '新建推送渠道'}
        </CardTitle>
        <div className="grid gap-3 md:grid-cols-2">
          <Field label="渠道名">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="我的 iPhone / 运维群" />
          </Field>
          <Field label="类型" hint={def?.desc}>
            <Select value={type} onChange={(e) => changeType(e.target.value)}>
              {['domestic', 'international', 'generic'].map((g) => {
                const list = types.filter((t) => t.group === g)
                if (!list.length) return null
                return (
                  <optgroup key={g} label={GROUP_LABEL[g]}>
                    {list.map((t) => (
                      <option key={t.type} value={t.type}>
                        {t.label}
                      </option>
                    ))}
                  </optgroup>
                )
              })}
            </Select>
          </Field>
          {(def?.fields || []).map((f) => (
            <div key={f.key} className={f.type === 'textarea' ? 'md:col-span-2' : ''}>
              <ConfigField
                f={f}
                value={config[f.key]}
                onChange={(v) => setCfg(f.key, v)}
              />
            </div>
          ))}
        </div>
        <label className="mt-3 flex items-center gap-2 text-[13px] text-[#323130]">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="size-4 accent-[#0078d4]"
          />
          启用
        </label>
        {err ? (
          <p role="alert" className="mt-3 rounded-md bg-[#fde7e9] px-3 py-2 text-[12.5px] text-[#a4262c]">
            {err}
          </p>
        ) : null}
        <div className="mt-4 flex gap-2">
          <Button variant="primary" onClick={submit} disabled={busy}>
            {busy ? <Spinner className="border-white/40 border-t-white" /> : null}{' '}
            {editing ? '保存' : '创建'}
          </Button>
          <Button
            variant="secondary"
            onClick={() => {
              setOpen(false)
              onCancel && onCancel()
            }}
          >
            取消
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}

function ChannelCard({ ch, typeMeta, types, onChanged }) {
  const [busy, setBusy] = React.useState('')
  const [msg, setMsg] = React.useState(null)

  const test = async () => {
    setBusy('test')
    setMsg(null)
    try {
      await api.testChannel(ch.id)
      setMsg({ ok: true, text: '测试消息已发送' })
    } catch (e) {
      setMsg({ ok: false, text: e.message })
    } finally {
      setBusy('')
      onChanged && onChanged()
    }
  }

  return (
    <Card className="flex flex-col">
      <CardBody className="flex flex-1 flex-col">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="truncate text-[15px] font-semibold text-[#323130]">{ch.name}</h2>
            <p className="mt-0.5 text-[11.5px] text-[#a19f9d]">
              {typeMeta?.label || ch.type}
              {typeMeta?.group ? ` · ${GROUP_LABEL[typeMeta.group]}` : ''}
            </p>
          </div>
          <Badge tone={ch.enabled ? 'green' : 'gray'}>{ch.enabled ? '启用' : '停用'}</Badge>
        </div>

        <p className="mt-3 text-[11.5px] text-[#a19f9d]">
          {ch.last_sent_at ? `最近发送 ${relTime(ch.last_sent_at)}` : '尚未发送'}
        </p>
        {ch.last_error ? (
          <p className="mt-1 rounded-md bg-[#fdf3f4] px-2.5 py-1.5 text-[11.5px] text-[#a4262c] break-all">
            {ch.last_error}
          </p>
        ) : (
          <p className="mt-1 text-[11.5px] text-[#0b8f52]">最近一次投递正常</p>
        )}

        {msg ? (
          <p
            className={cx(
              'mt-2 rounded-md px-2.5 py-1.5 text-[11.5px] break-all',
              msg.ok ? 'bg-[#eefbf4] text-[#0b6a3d]' : 'bg-[#fde7e9] text-[#a4262c]'
            )}
          >
            {msg.text}
          </p>
        ) : null}

        <div className="mt-auto flex flex-wrap items-center gap-1.5 border-t border-[#f0f0f0] pt-3 mt-3">
          <Button variant="primary" size="sm" disabled={!!busy} onClick={test}>
            {busy === 'test' ? <Spinner className="border-white/40 border-t-white" /> : null} 发送测试
          </Button>
          <ChannelForm
            channel={ch}
            types={types}
            onDone={onChanged}
            trigger={
              <button
                type="button"
                className="inline-flex h-8 items-center rounded-md border border-[#e1e1e1] bg-white px-3 text-[13px] font-semibold text-[#323130] shadow-fluent2 transition-all duration-150 ease-out hover:-translate-y-0.5 hover:bg-[#f3f2f1] hover:shadow-fluent4 active:scale-[0.97] active:translate-y-0 active:shadow-none focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
              >
                编辑
              </button>
            }
          />
          <button
            onClick={async () => {
              if (!confirm(`删除渠道「${ch.name}」？已绑定它的任务将不再推送。`)) return
              try {
                await api.deleteChannel(ch.id)
                onChanged && onChanged()
              } catch (e) {
                setMsg({ ok: false, text: e.message })
              }
            }}
            className="ml-auto rounded-md px-2 h-8 text-[12px] font-semibold text-[#e81123] transition-all duration-150 ease-out hover:bg-[#fdf3f4] active:scale-[0.97] focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2"
          >
            删除
          </button>
        </div>
      </CardBody>
    </Card>
  )
}

export default function Channels() {
  const [channels, setChannels] = React.useState(null)
  const [types, setTypes] = React.useState([])
  const [err, setErr] = React.useState('')

  const load = React.useCallback(async () => {
    try {
      const [cs, ts] = await Promise.all([api.channels(), api.channelTypes()])
      setChannels(cs)
      setTypes(ts.types || [])
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
          <h1 className="text-[20px] font-semibold tracking-tight text-[#323130]">推送</h1>
          <p className="mt-1 text-[12.5px] text-[#605e5c]">
            配置一次渠道，在任务「成功」或「失败（重试用尽）」时按需推送。
          </p>
        </div>
        <ChannelForm types={types} onDone={load} />
      </div>

      {err ? (
        <Card hover={false} className="p-5">
          <p className="text-[13px] text-[#a4262c]">{err}</p>
        </Card>
      ) : null}

      {!channels ? (
        <div className="flex items-center gap-2 py-16 text-[13px] text-[#605e5c]">
          <Spinner /> 加载中…
        </div>
      ) : channels.length === 0 ? (
        <Card hover={false}>
          <Empty title="还没有推送渠道" hint="点「新建渠道」，支持 Bark、企业微信、钉钉、飞书、Telegram、Slack 等" />
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {channels.map((ch) => (
            <ChannelCard
              key={ch.id}
              ch={ch}
              typeMeta={types.find((t) => t.type === ch.type)}
              types={types}
              onChanged={load}
            />
          ))}
        </div>
      )}
    </div>
  )
}
