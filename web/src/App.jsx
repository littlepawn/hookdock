import React from 'react'
import { RouterProvider, Link, useRouter, match } from './router.jsx'
import { getToken, setToken, verifyToken, api, subscribe } from './api.js'
import { Button, Field, Input, Spinner, cx } from './components/ui.jsx'
import Overview from './pages/Overview.jsx'
import Endpoints from './pages/Endpoints.jsx'
import EndpointDetail from './pages/EndpointDetail.jsx'
import Jobs from './pages/Jobs.jsx'
import JobDetail from './pages/JobDetail.jsx'
import Docs from './pages/Docs.jsx'
import Tokens from './pages/Tokens.jsx'

/* ---------------- 品牌标 ---------------- */
function Mark({ className }) {
  // 品牌图：h + 锚钩 monogram（hookdock/design/logo-c），tile 圆角约 20%
  return (
    <img
      src="/logo-512.png"
      alt="hookdock"
      className={cx(
        'size-7 rounded-[20%] shadow-fluent4',
        'transition-transform duration-150 ease-out group-hover:scale-105',
        className
      )}
    />
  )
}

/* ---------------- 管理令牌弹窗 ---------------- */
function TokenDialog({ open, onClose }) {
  const [v, setV] = React.useState(getToken())
  React.useEffect(() => {
    if (open) setV(getToken())
  }, [open])
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[14vh] px-4">
      <div
        className="absolute inset-0 bg-[#323130]/25 backdrop-blur-sm animate-fade"
        onClick={onClose}
      />
      <div className="relative w-full max-w-md rounded-lg border border-white/60 bg-white/70 backdrop-blur-xl shadow-fluent64 animate-rise">
        <div className="p-5">
          <h3 className="text-[15px] font-semibold text-[#323130]">本机访问令牌</h3>
          <p className="mt-1 text-[12.5px] text-[#605e5c]">
            粘贴你的 API 令牌明文（<code className="font-mono text-[11.5px]">hk_…</code>），
            通过 <code className="font-mono text-[11.5px]">X-Api-Token</code> 头携带。仅保存在本机浏览器。
            「令牌」页可创建/管理各类令牌。
          </p>
          <div className="mt-4">
            <Field label="X-Api-Token">
              <Input
                type="password"
                value={v}
                placeholder="留空表示服务端未启用令牌"
                onChange={(e) => setV(e.target.value)}
              />
            </Field>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                setToken(v.trim())
                onClose()
                window.location.reload()
              }}
            >
              保存并刷新
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

/* ---------------- 布局 ---------------- */
const NAV = [
  { to: '/', label: '概览', exact: true },
  { to: '/endpoints', label: '端点' },
  { to: '/jobs', label: '任务' },
  { to: '/tokens', label: '令牌' },
  { to: '/docs', label: '文档' },
]

function NavLink({ item }) {
  const { path } = useRouter()
  const active = item.exact ? path === item.to : path.startsWith(item.to)
  return (
    <Link
      to={item.to}
      className={cx(
        'relative rounded-md px-3 h-8 inline-flex items-center text-[13.5px] font-semibold',
        'transition-all duration-150 ease-out active:scale-[0.97]',
        'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2',
        active
          ? 'text-[#005a9e] bg-[#0078d4]/8'
          : 'text-[#605e5c] hover:text-[#323130] hover:bg-[#f3f2f1]'
      )}
    >
      {item.label}
      <span
        className={cx(
          'absolute inset-x-3 -bottom-[9px] h-[2px] rounded-full bg-[#0078d4] transition-all duration-150',
          active ? 'opacity-100 scale-x-100' : 'opacity-0 scale-x-50'
        )}
      />
    </Link>
  )
}

function Layout({ children, me, authRequired, onLogout }) {
  const [tokenOpen, setTokenOpen] = React.useState(false)
  const [live, setLive] = React.useState(false)
  const [menuOpen, setMenuOpen] = React.useState(false)
  const menuRef = React.useRef(null)

  React.useEffect(() => {
    const off = subscribe(() => setLive(true))
    const t = setTimeout(() => setLive(true), 1200)
    return () => {
      off()
      clearTimeout(t)
    }
  }, [])

  React.useEffect(() => {
    if (!menuOpen) return
    const close = (e) => {
      if (menuRef.current && !menuRef.current.contains(e.target)) setMenuOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [menuOpen])

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-[#e1e1e1] bg-white/70 backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-2 px-4 md:px-6 lg:px-8">
          <Link to="/" className="group mr-2 flex items-center gap-2">
            <Mark />
            <span className="text-[15px] font-semibold tracking-tight text-[#323130]">
              hookdock
            </span>
          </Link>
          <nav className="flex items-center gap-0.5">
            {NAV.map((n) => (
              <NavLink key={n.to} item={n} />
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-2">
            <span className="hidden items-center gap-1.5 text-[12px] text-[#a19f9d] sm:flex">
              <span
                className={cx(
                  'size-1.5 rounded-full transition-colors duration-200',
                  live ? 'bg-[#00cc6a]' : 'bg-[#ffb900]'
                )}
              />
              {live ? '实时连接' : '连接中'}
            </span>
            <div className="relative" ref={menuRef}>
              <button
                type="button"
                onClick={() => setMenuOpen((o) => !o)}
                aria-haspopup="menu"
                aria-expanded={menuOpen}
                className={cx(
                  'flex items-center gap-2 rounded-md px-2.5 h-8 text-[13px] font-medium',
                  'transition-all duration-150 ease-out active:scale-[0.97]',
                  'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2',
                  menuOpen ? 'bg-[#0078d4]/8 text-[#005a9e]' : 'text-[#323130] hover:bg-[#f3f2f1]'
                )}
                title="账号"
              >
                <span className="grid size-5 place-items-center rounded-full bg-[#0078d4] text-[10px] font-semibold text-white">
                  {(me?.name || '?').slice(0, 1).toUpperCase()}
                </span>
                <span className="max-w-[120px] truncate">{me?.name || '未登录'}</span>
              </button>
              {menuOpen ? (
                <div
                  role="menu"
                  className="absolute right-0 top-10 w-60 rounded-lg border border-[#e1e1e1] bg-white shadow-fluent16 animate-rise"
                >
                  <div className="border-b border-[#e1e1e1] px-4 py-3">
                    <p className="text-[13.5px] font-semibold text-[#323130]">{me?.name || '—'}</p>
                    <p className="mt-0.5 flex items-center gap-1.5 text-[12px] text-[#605e5c]">
                      <span className="rounded bg-[#0078d4]/8 px-1.5 py-[1px] font-mono text-[11px] text-[#005a9e]">
                        {me?.role || 'unknown'}
                      </span>
                      <span className="text-[#a19f9d]">{me?.kind === 'env' ? '环境管理员' : me?.kind === 'token' ? '访问令牌' : '开放模式'}</span>
                    </p>
                    {me?.perms?.length ? (
                      <p className="mt-1.5 text-[11px] text-[#a19f9d]">{me.perms.length} 项权限</p>
                    ) : null}
                  </div>
                  <div className="p-1.5">
                    {!authRequired ? (
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => { setMenuOpen(false); setTokenOpen(true) }}
                        className="w-full rounded-md px-3 py-2 text-left text-[13px] text-[#323130] transition-colors duration-150 hover:bg-[#f3f2f1]"
                      >
                        填入访问令牌
                      </button>
                    ) : null}
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => { setMenuOpen(false); onLogout() }}
                      className="w-full rounded-md px-3 py-2 text-left text-[13px] text-[#a4262c] transition-colors duration-150 hover:bg-[#fde7e9]"
                    >
                      退出登录
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 py-6 md:px-6 lg:px-8">{children}</main>

      <footer className="mx-auto max-w-6xl px-4 pb-10 pt-2 text-[12px] text-[#a19f9d] md:px-6 lg:px-8">
        hookdock v0.1 · webhook 接收与规则化定时任务 ·{' '}
        <a
          className="text-[#0078d4] hover:underline focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2 rounded"
          href="/openapi.json"
          target="_blank"
          rel="noreferrer"
        >
          OpenAPI 3.1
        </a>
      </footer>

      <TokenDialog open={tokenOpen} onClose={() => setTokenOpen(false)} />
    </div>
  )
}

/* ---------------- 路由表 ---------------- */
function Routes() {
  const { path } = useRouter()

  if (match('/', path)) return <Overview />
  if (match('/endpoints', path)) return <Endpoints />
  const ep = match('/endpoints/:slug', path)
  if (ep) return <EndpointDetail slug={ep.slug} />
  if (match('/jobs', path)) return <Jobs />
  const jd = match('/jobs/:id', path)
  if (jd) return <JobDetail id={jd.id} />
  if (match('/tokens', path)) return <Tokens />
  if (match('/docs', path)) return <Docs />
  return (
    <div className="py-24 text-center">
      <p className="text-[15px] font-semibold text-[#605e5c]">页面不存在</p>
      <Link to="/" className="mt-2 inline-block text-[13px] text-[#0078d4] hover:underline">
        回到概览
      </Link>
    </div>
  )
}

/* ---------------- 登录门 ---------------- */
/* 服务端要求鉴权（有令牌或配了管理员）而浏览器没有可用令牌时，整页挡在登录页 */
function LoginGate({ onLogin }) {
  const [v, setV] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [hint, setHint] = React.useState('')

  const submit = async (e) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setErr('')
    const { ok, data } = await verifyToken(v.trim())
    if (ok) {
      setToken(v.trim())
      onLogin(data?.me || null)
      return
    }
    setErr(data?.error || '登录失败，请检查令牌')
    setBusy(false)
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-[#faf9f8] px-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center justify-center gap-2.5">
          <Mark className="size-9" />
          <span className="text-[22px] font-semibold tracking-tight text-[#323130]">hookdock</span>
        </div>
        <div className="rounded-lg border border-white/60 bg-white/70 shadow-fluent16 animate-rise">
          <form className="p-6" onSubmit={submit}>
            <h1 className="text-[17px] font-semibold text-[#323130]">登录 hookdock</h1>
            <p className="mt-1.5 text-[12.5px] leading-relaxed text-[#605e5c]">
              粘贴访问令牌明文（<code className="font-mono text-[11.5px]">hk_…</code> 或管理员令牌），
              通过 <code className="font-mono text-[11.5px]">X-Api-Token</code> 头鉴权。仅保存在本机浏览器。
            </p>
            {hint ? (
              <p className="mt-3 rounded-md bg-[#f3f2f1] px-3 py-2 text-[12px] text-[#605e5c]">{hint}</p>
            ) : null}
            {err ? (
              <p role="alert" className="mt-3 rounded-md bg-[#fde7e9] px-3 py-2 text-[12.5px] font-medium text-[#a4262c]">
                {err}
              </p>
            ) : null}
            <div className="mt-4">
              <Field label="访问令牌">
                <Input
                  type="password"
                  value={v}
                  autoFocus
                  placeholder="hk_…"
                  aria-label="访问令牌"
                  onChange={(e) => setV(e.target.value)}
                />
              </Field>
            </div>
            <Button type="submit" variant="primary" className="mt-5 w-full" disabled={busy}>
              {busy ? '验证中…' : '登录'}
            </Button>
          </form>
        </div>
        <p className="mt-5 text-center text-[11.5px] text-[#a19f9d]">
          webhook 接收与规则化定时任务 ·{' '}
          <a className="text-[#0078d4] hover:underline" href="/openapi.json" target="_blank" rel="noreferrer">
            OpenAPI 3.1
          </a>
        </p>
      </div>
    </div>
  )
}

/* ---------------- 应用入口：先探测身份，再决定门禁 ---------------- */
function Gate() {
  const { path } = useRouter()
  const [state, setState] = React.useState('loading') // loading | open | authed
  const [me, setMe] = React.useState(null)
  const [authRequired, setAuthRequired] = React.useState(true)

  const probe = React.useCallback(async () => {
    setState('loading')
    const { ok, data } = await verifyToken(getToken())
    if (ok) {
      setMe(data?.me || null)
      setAuthRequired(!!data?.auth_required)
      setState('authed')
    } else if (data?.auth_required === false) {
      // 服务端无任何令牌且未配管理员：开放模式，不设门
      setMe(data?.me || null)
      setAuthRequired(false)
      setState('open')
    } else {
      setAuthRequired(true)
      setState('login')
    }
  }, [])

  React.useEffect(() => {
    probe()
  }, [probe, path])

  if (state === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  if (state === 'login') return <LoginGate onLogin={(m) => { setMe(m); setState('authed') }} />

  return (
    <Layout me={me} authRequired={authRequired} onLogout={() => { setToken(''); setMe(null); setState('login') }}>
      <Routes />
    </Layout>
  )
}

export default function App() {
  return (
    <RouterProvider>
      <Gate />
    </RouterProvider>
  )
}

export { api }
