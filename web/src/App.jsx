import React from 'react'
import { RouterProvider, Link, useRouter, match } from './router.jsx'
import { getToken, setToken, api, subscribe } from './api.js'
import { Button, Field, Input, cx } from './components/ui.jsx'
import Overview from './pages/Overview.jsx'
import Endpoints from './pages/Endpoints.jsx'
import EndpointDetail from './pages/EndpointDetail.jsx'
import Jobs from './pages/Jobs.jsx'
import JobDetail from './pages/JobDetail.jsx'
import Docs from './pages/Docs.jsx'
import Tokens from './pages/Tokens.jsx'

/* ---------------- 品牌标 ---------------- */
function Mark({ className }) {
  return (
    <span
      className={cx(
        'grid size-7 place-items-center rounded-md bg-[#0078d4] text-white shadow-fluent4',
        'transition-transform duration-150 ease-out group-hover:scale-105',
        className
      )}
    >
      <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2.2">
        <path d="M12 3v9a4 4 0 0 0 4 4h1" strokeLinecap="round" />
        <circle cx="17" cy="16" r="3.2" strokeLinecap="round" />
      </svg>
    </span>
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

function Layout({ children }) {
  const [tokenOpen, setTokenOpen] = React.useState(false)
  const [live, setLive] = React.useState(false)

  React.useEffect(() => {
    const off = subscribe(() => setLive(true))
    const t = setTimeout(() => setLive(true), 1200)
    return () => {
      off()
      clearTimeout(t)
    }
  }, [])

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
            <Button
              variant="subtle"
              size="sm"
              onClick={() => setTokenOpen(true)}
              title="设置管理令牌"
            >
              令牌
            </Button>
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

export default function App() {
  return (
    <RouterProvider>
      <Layout>
        <Routes />
      </Layout>
    </RouterProvider>
  )
}

export { api }
