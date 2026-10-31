import React from 'react'

export const cx = (...a) => a.filter(Boolean).join(' ')

/* ---------------- 按钮 ---------------- */
const baseBtn =
  'inline-flex items-center justify-center gap-1.5 rounded-md font-semibold ' +
  'transition-all duration-150 ease-out select-none ' +
  'active:scale-[0.97] active:translate-y-0 active:shadow-none ' +
  'focus:outline-none focus-visible:ring-2 focus-visible:ring-[#0078d4] focus-visible:ring-offset-2 ' +
  'disabled:opacity-40 disabled:pointer-events-none'

const btnVariants = {
  primary:
    'bg-[#0078d4] text-white shadow-fluent4 hover:bg-[#106ebe] hover:-translate-y-0.5 ' +
    'hover:shadow-fluent8',
  secondary:
    'bg-white text-[#323130] border border-[#e1e1e1] shadow-fluent2 hover:bg-[#f3f2f1] ' +
    'hover:-translate-y-0.5 hover:shadow-fluent4',
  subtle:
    'bg-transparent text-[#0078d4] hover:bg-[#0078d4]/8 hover:-translate-y-0.5',
  danger:
    'bg-white text-[#e81123] border border-[#e1e1e1] shadow-fluent2 hover:bg-[#fdf3f4] ' +
    'hover:border-[#e81123]/40 hover:-translate-y-0.5 hover:shadow-fluent4',
}

const btnSizes = {
  sm: 'h-8 px-3 text-[13px]',
  md: 'h-9 px-4 text-[14px]',
  lg: 'h-10 px-5 text-[14px]',
}

export function Button({ variant = 'secondary', size = 'md', className, ...p }) {
  return (
    <button
      className={cx(baseBtn, btnVariants[variant], btnSizes[size], className)}
      {...p}
    />
  )
}

/* ---------------- 卡片 ---------------- */
export function Card({ className, children, hover = true, ...p }) {
  return (
    <div
      className={cx(
        'group relative rounded-lg border border-[#e1e1e1] bg-white shadow-fluent4 reveal',
        'transition-all duration-200 ease-out',
        hover &&
          'hover:-translate-y-1 hover:shadow-fluent16 hover:border-[#d2d0ce]',
        className
      )}
      {...p}
    >
      {children}
    </div>
  )
}

export function CardBody({ className, children }) {
  return <div className={cx('p-5', className)}>{children}</div>
}

export function CardTitle({ children, hint }) {
  return (
    <div className="flex items-baseline justify-between gap-3 mb-4">
      <h2 className="text-[15px] font-semibold text-[#323130] tracking-tight">
        {children}
      </h2>
      {hint ? <span className="text-[12px] text-[#a19f9d]">{hint}</span> : null}
    </div>
  )
}

/* ---------------- 徽章 ---------------- */
const tones = {
  blue: 'bg-[#0078d4]/10 text-[#005a9e] border-[#0078d4]/20',
  green: 'bg-[#00cc6a]/12 text-[#0b6a3d] border-[#00cc6a]/30',
  yellow: 'bg-[#ffb900]/16 text-[#7a5b00] border-[#ffb900]/35',
  red: 'bg-[#e81123]/10 text-[#a4262c] border-[#e81123]/25',
  gray: 'bg-[#f3f2f1] text-[#605e5c] border-[#e1e1e1]',
}

export function Badge({ tone = 'gray', children, className }) {
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1 rounded-md border px-2 py-[2px] text-[11.5px] font-semibold whitespace-nowrap',
        tones[tone],
        className
      )}
    >
      {children}
    </span>
  )
}

export function StatusBadge({ status }) {
  const map = {
    succeeded: ['green', '成功'],
    running: ['blue', '运行中'],
    queued: ['yellow', '排队中'],
    failed: ['red', '失败'],
    timeout: ['red', '超时'],
    dead: ['red', '死信'],
    canceled: ['gray', '已取消'],
  }
  const [tone, label] = map[status] || ['gray', status || '未知']
  return (
    <Badge tone={tone}>
      <span
        className={cx(
          'size-1.5 rounded-full',
          tone === 'green' && 'bg-[#00cc6a]',
          tone === 'blue' && 'bg-[#0078d4]',
          tone === 'yellow' && 'bg-[#ffb900]',
          tone === 'red' && 'bg-[#e81123]',
          tone === 'gray' && 'bg-[#a19f9d]'
        )}
      />
      {label}
    </Badge>
  )
}

/* ---------------- 输入 ---------------- */
const fieldBase =
  'w-full rounded-md border border-[#8a8886] bg-white px-3 text-[14px] text-[#323130] font-sans ' +
  'transition-all duration-150 ease-out placeholder:text-[#a19f9d] ' +
  'focus:border-[#0078d4] focus:outline-none focus:ring-2 focus:ring-[#0078d4] focus:ring-offset-2'

export function Input({ className, ...p }) {
  return <input className={cx(fieldBase, 'h-9', className)} {...p} />
}

export function Textarea({ className, ...p }) {
  return (
    <textarea className={cx(fieldBase, 'py-2 font-mono text-[12.5px]', className)} {...p} />
  )
}

export function Select({ className, children, ...p }) {
  return (
    <select className={cx(fieldBase, 'h-9 pr-8', className)} {...p}>
      {children}
    </select>
  )
}

export function Field({ label, hint, children }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[12.5px] font-semibold text-[#605e5c]">
        {label}
      </span>
      {children}
      {hint ? <span className="mt-1 block text-[11.5px] text-[#a19f9d]">{hint}</span> : null}
    </label>
  )
}

/* ---------------- 其他 ---------------- */
export function Empty({ title, hint }) {
  return (
    <div className="flex flex-col items-center justify-center gap-1 py-14 text-center">
      <div className="mb-2 grid size-11 place-items-center rounded-lg bg-[#f3f2f1] text-[#a19f9d]">
        <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.6">
          <path d="M4 7h16M4 12h10M4 17h6" strokeLinecap="round" />
        </svg>
      </div>
      <p className="text-[13.5px] font-semibold text-[#605e5c]">{title}</p>
      {hint ? <p className="text-[12px] text-[#a19f9d]">{hint}</p> : null}
    </div>
  )
}

export function Spinner({ className }) {
  return (
    <span
      className={cx(
        'inline-block size-3.5 animate-spin rounded-full border border-[#0078d4]/25 border-t-[#0078d4]',
        className
      )}
    />
  )
}

export function CopyButton({ text, label = '复制' }) {
  const [done, setDone] = React.useState(false)
  return (
    <Button
      variant="subtle"
      size="sm"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
        } catch {
          /* 剪贴板不可用时静默 */
        }
        setDone(true)
        setTimeout(() => setDone(false), 1400)
      }}
    >
      {done ? '已复制' : label}
    </Button>
  )
}

export function relTime(ts) {
  if (!ts) return '—'
  const s = Math.floor(Date.now() / 1000 - ts)
  if (s < 5) return '刚刚'
  if (s < 60) return `${s} 秒前`
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  return `${Math.floor(s / 86400)} 天前`
}

export function absTime(ts) {
  if (!ts) return '—'
  const d = new Date(ts * 1000)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
