import React from 'react'
import { api } from '../api.js'
import {
  Button, Card, CardBody, Badge, Input, Field, Textarea, Empty, Spinner, cx,
} from '../components/ui.jsx'

const roleTone = {
  sender: 'blue', trigger: 'green', operator: 'yellow', admin: 'red',
}
const roleDesc = {
  sender: '只能发送 webhook',
  trigger: '发送 webhook + 请求里带任务创建',
  operator: '发送 + 查看请求与任务',
  admin: '全部权限（含令牌管理）',
}

function PermGrid({ perms, labels, onToggle, disabled }) {
  return (
    <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
      {Object.entries(labels).map(([perm, label]) => {
        const on = perms.includes(perm)
        return (
          <button
            key={perm}
            type="button"
            disabled={disabled}
            aria-pressed={on}
            onClick={() => onToggle(perm)}
            className={cx(
              'flex items-start gap-2 rounded-md border px-2.5 py-1.5 text-left transition-all duration-150',
              'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-1',
              'disabled:cursor-not-allowed disabled:opacity-50',
              on
                ? 'border-[#0078d4]/40 bg-[#0078d4]/8'
                : 'border-[#e1e1e1] bg-white hover:border-[#d2d0ce]'
            )}
          >
            <span
              className={cx(
                'mt-[3px] grid size-3.5 shrink-0 place-items-center rounded-[3px] border transition-colors duration-150',
                on ? 'border-[#0078d4] bg-[#0078d4] text-white' : 'border-[#a19f9d] bg-white'
              )}
            >
              {on ? (
                <svg viewBox="0 0 12 12" className="size-2.5" fill="none" stroke="currentColor" strokeWidth="2.4">
                  <path d="M2.5 6.2 5 8.6l4.5-5" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              ) : null}
            </span>
            <span className="min-w-0">
              <span className="block font-mono text-[11px] text-[#605e5c]">{perm}</span>
              <span className="block text-[12px] leading-tight text-[#323130]">{label}</span>
            </span>
          </button>
        )
      })}
    </div>
  )
}

function TokenForm({ initial, endpoints, meta, onDone, onCancel }) {
  const editing = !!initial
  const [name, setName] = React.useState(initial?.name || '')
  const [role, setRole] = React.useState(initial?.role || 'sender')
  const [perms, setPerms] = React.useState(initial?.perms || [])
  const [scopes, setScopes] = React.useState(initial?.scopes || [])
  const [note, setNote] = React.useState(initial?.note || '')
  const [enabled, setEnabled] = React.useState(initial ? initial.enabled : true)
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')

  const rolePermsMap = {}
  ;(meta?.roles || []).forEach((r) => { rolePermsMap[r.role] = r.perms })
  const preset = rolePermsMap[role] || []

  const pickRole = (r) => {
    setRole(r)
    setPerms(rolePermsMap[r] || [])
  }
  const togglePerm = (p) =>
    setPerms((cur) => (cur.includes(p) ? cur.filter((x) => x !== p) : [...cur, p]))
  const toggleScope = (s) =>
    setScopes((cur) => (cur.includes(s) ? cur.filter((x) => x !== s) : [...cur, s]))

  const submit = async (e) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      const body = { name: name.trim(), role, perms, scopes, note: note.trim(), enabled }
      if (editing) {
        // PUT 空值=不改：这里全量提交，perms/scopes 传 [] 表示显式清空
        await api.updateToken(initial.id, body)
      } else {
        await api.createToken(body)
      }
      onDone()
    } catch (ex) {
      setErr(ex.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="名称" hint="给谁用、干什么">
          <Input value={name} onChange={(e) => setName(e.target.value)} required maxLength={200}
            placeholder="例如：ci-deploy 只发 webhook" />
        </Field>
        <Field label="角色" hint={roleDesc[role]}>
          <div className="flex flex-wrap gap-1.5">
            {(meta?.roles || []).map((r) => (
              <button key={r.role} type="button" aria-pressed={role === r.role} onClick={() => pickRole(r.role)}
                className={cx(
                  'rounded-md px-2.5 h-8 text-[12.5px] font-semibold transition-all duration-150 active:scale-[0.97]',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-1',
                  role === r.role ? 'bg-[#0078d4] text-white shadow-fluent4' : 'bg-white border border-[#e1e1e1] text-[#605e5c] hover:border-[#d2d0ce]'
                )}>
                {r.role}
              </button>
            ))}
          </div>
        </Field>
      </div>

      <Field label="权限" hint="选角色已预填，可再微调">
        <PermGrid perms={perms} labels={meta?.perm_labels || {}} onToggle={togglePerm} />
      </Field>

      <Field label="端点作用域" hint="不勾选 = 不限制（所有端点）">
        <div className="flex flex-wrap gap-1.5">
          {(endpoints || []).map((ep) => {
            const on = scopes.includes(ep.slug)
            return (
              <button key={ep.slug} type="button" aria-pressed={on} onClick={() => toggleScope(ep.slug)}
                className={cx(
                  'inline-flex items-center gap-1.5 rounded-md border px-2.5 h-8 font-mono text-[12px] transition-all duration-150 active:scale-[0.97]',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-1',
                  on ? 'border-[#0078d4]/50 bg-[#0078d4]/8 text-[#005a9e]' : 'border-[#e1e1e1] bg-white text-[#605e5c] hover:border-[#d2d0ce]'
                )}>
                {on ? '✓' : ''}{ep.slug}
              </button>
            )
          })}
          {!endpoints?.length ? <span className="text-[12px] text-[#a19f9d]">暂无端点</span> : null}
        </div>
      </Field>

      <Field label="备注（可选）">
        <Textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)}
          placeholder="用途、联系人、轮换计划…" />
      </Field>

      <label className="flex items-center gap-2 text-[13px] text-[#323130]">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)}
          className="size-4 accent-[#0078d4]" />
        启用此令牌
      </label>

      {err ? <p className="text-[12.5px] text-[#a4262c]">{err}</p> : null}

      <div className="flex justify-end gap-2 pt-1">
        <Button type="button" variant="secondary" onClick={onCancel}>取消</Button>
        <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
          {busy ? '保存中…' : editing ? '保存' : '创建令牌'}
        </Button>
      </div>
    </form>
  )
}

function CreatedPanel({ plain, onClose }) {
  return (
    <div className="rounded-lg border border-[#00cc6a]/40 bg-[#00cc6a]/6 p-4">
      <p className="text-[13px] font-semibold text-[#0b6a3d]">令牌已创建 —— 明文只显示这一次</p>
      <div className="mt-2 flex items-center gap-2">
        <code className="min-w-0 flex-1 break-all rounded-md bg-white px-2.5 py-2 font-mono text-[12px] text-[#323130] border border-[#e1e1e1]">
          {plain}
        </code>
        <Button variant="primary" size="sm" onClick={() => navigator.clipboard.writeText(plain)}>复制</Button>
      </div>
      <p className="mt-2 text-[12px] text-[#605e5c]">
        服务端只保存 sha256 摘要。调用方式：<code className="font-mono text-[11px]">X-Api-Token: &lt;明文&gt;</code>
      </p>
      <div className="mt-3 text-right">
        <Button variant="secondary" size="sm" onClick={onClose}>我已保存，关闭</Button>
      </div>
    </div>
  )
}

export default function Tokens() {
  const [data, setData] = React.useState(null)   // {tokens, all_perms, perm_labels, roles}
  const [endpoints, setEndpoints] = React.useState([])
  const [loading, setLoading] = React.useState(true)
  const [err, setErr] = React.useState('')
  const [creating, setCreating] = React.useState(false)
  const [editing, setEditing] = React.useState(null) // token object
  const [created, setCreated] = React.useState(null) // plain text
  const [confirmDel, setConfirmDel] = React.useState(null)
  const [me, setMe] = React.useState(null)

  const load = React.useCallback(async () => {
    try {
      setErr('')
      const [d, eps, w] = await Promise.all([api.tokens(), api.endpoints(), api.whoami()])
      setData(d)
      setEndpoints(eps)
      setMe(w?.me || null)
    } catch (ex) {
      setErr(ex.message)
    } finally {
      setLoading(false)
    }
  }, [])
  React.useEffect(() => { load() }, [load])

  const canManage = me && (me.perms || []).includes('token.manage')

  const toggleEnabled = async (t) => {
    await api.updateToken(t.id, { enabled: !t.enabled })
    load()
  }

  const doDelete = async () => {
    if (!confirmDel) return
    await api.deleteToken(confirmDel.id)
    setConfirmDel(null)
    load()
  }

  if (loading) return <div className="flex justify-center py-24"><Spinner /></div>
  if (err) return <Empty title="加载失败" hint={err} />

  const tokens = data?.tokens || []

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight text-[#323130]">访问令牌</h1>
          <p className="mt-1 text-[13px] text-[#605e5c]">
            给不同调用方发不同令牌：能只发 webhook，也能允许请求里直接下单建任务。库内只存 sha256。
          </p>
        </div>
        <Button variant="primary" onClick={() => { setCreating(true); setEditing(null); setCreated(null) }}>
          新建令牌
        </Button>
      </div>

      {created ? <CreatedPanel plain={created} onClose={() => setCreated(null)} /> : null}

      {creating || editing ? (
        <Card hover={false}>
          <CardBody>
            <h2 className="mb-4 text-[15px] font-semibold text-[#323130]">
              {editing ? `编辑令牌 · ${editing.prefix}…` : '新建令牌'}
            </h2>
            <TokenForm
              initial={editing}
              endpoints={endpoints}
              meta={data}
              onDone={() => { setCreating(false); setEditing(null); load() }}
              onCancel={() => { setCreating(false); setEditing(null) }}
            />
          </CardBody>
        </Card>
      ) : null}

      {canManage === false ? (
        <Empty title="没有令牌管理权限" hint="当前令牌缺少 token.manage" />
      ) : tokens.length === 0 ? (
        <Empty title="还没有令牌" hint="创建第一个令牌后，所有管理接口与 webhook 都将要求携带它" />
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {tokens.map((t) => (
            <Card key={t.id}>
              <CardBody>
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="truncate text-[14.5px] font-semibold text-[#323130]">{t.name}</h3>
                      <Badge tone={roleTone[t.role] || 'gray'}>{t.role}</Badge>
                      {!t.enabled ? <Badge tone="red">已停用</Badge> : null}
                    </div>
                    <p className="mt-1 font-mono text-[11.5px] text-[#a19f9d]">{t.prefix}…</p>
                  </div>
                  <div className="flex shrink-0 gap-1.5">
                    <Button size="sm" variant="subtle" onClick={() => toggleEnabled(t)}>
                      {t.enabled ? '停用' : '启用'}
                    </Button>
                    <Button size="sm" variant="secondary" onClick={() => { setEditing(t); setCreating(false); setCreated(null) }}>
                      编辑
                    </Button>
                    <Button size="sm" variant="subtle" className="text-[#a4262c] hover:bg-[#a4262c]/8"
                      onClick={() => setConfirmDel(t)}>
                      删除
                    </Button>
                  </div>
                </div>

                <div className="mt-3 flex flex-wrap gap-1.5">
                  {(t.perms && t.perms.length
                    ? t.perms
                    : (data?.roles || []).find((r) => r.role === t.role)?.perms || []
                  ).map((p) => (
                    <span key={p} title={data?.perm_labels?.[p] || p}
                      className="rounded border border-[#e1e1e1] bg-[#f3f2f1] px-1.5 py-[1px] font-mono text-[11px] text-[#605e5c]">
                      {p}
                    </span>
                  ))}
                  {!t.perms?.length ? (
                    <span className="text-[11px] text-[#a19f9d]">（按角色 {t.role} 预设）</span>
                  ) : null}
                </div>

                <div className="mt-2.5 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-[#605e5c]">
                  <span>
                    作用域：
                    {t.scopes?.length
                      ? t.scopes.map((s) => <code key={s} className="font-mono">{s}</code>).reduce((a, b) => [a, '、', b])
                      : '全部端点'}
                  </span>
                  {t.note ? <span className="basis-full text-[#a19f9d]">{t.note}</span> : null}
                  <span>最近使用：{t.last_used_at ? new Date(t.last_used_at * 1000).toLocaleString() : '从未'}</span>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {confirmDel ? (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-[16vh] px-4">
          <div className="absolute inset-0 bg-[#323130]/25 backdrop-blur-sm animate-fade" onClick={() => setConfirmDel(null)} />
          <div className="relative w-full max-w-sm rounded-lg border border-white/60 bg-white/70 backdrop-blur-xl shadow-fluent64 animate-rise p-5">
            <h3 className="text-[15px] font-semibold text-[#323130]">删除令牌？</h3>
            <p className="mt-1.5 text-[12.5px] text-[#605e5c]">
              「{confirmDel.name}」将被立即作废，使用它的调用方会收到 401。此操作不可撤销。
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setConfirmDel(null)}>取消</Button>
              <Button variant="primary" className="bg-[#a4262c] hover:bg-[#a4262c]/90" onClick={doDelete}>确认删除</Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
