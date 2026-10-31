import React from 'react'

const RouterCtx = React.createContext({ path: '/', navigate: () => {} })

export function useRouter() {
  return React.useContext(RouterCtx)
}

function normalize(p) {
  if (!p) return '/'
  return p.length > 1 && p.endsWith('/') ? p.slice(0, -1) : p
}

export function RouterProvider({ children }) {
  const [path, setPath] = React.useState(normalize(window.location.pathname))

  React.useEffect(() => {
    const onPop = () => setPath(normalize(window.location.pathname))
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = React.useCallback((to, opts = {}) => {
    const target = normalize(to)
    if (target === normalize(window.location.pathname)) return
    if (opts.replace) window.history.replaceState({}, '', target)
    else window.history.pushState({}, '', target)
    setPath(target)
    window.scrollTo({ top: 0, behavior: 'instant' })
  }, [])

  return <RouterCtx.Provider value={{ path, navigate }}>{children}</RouterCtx.Provider>
}

export function Link({ to, className, children, ...rest }) {
  const { navigate } = useRouter()
  return (
    <a
      href={to}
      className={className}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
        e.preventDefault()
        navigate(to)
      }}
      {...rest}
    >
      {children}
    </a>
  )
}

// match('/e/:slug', '/e/demo') -> { slug: 'demo' }
export function match(pattern, path) {
  const pa = normalize(pattern).split('/').filter(Boolean)
  const pb = normalize(path).split('/').filter(Boolean)
  if (pa.length !== pb.length) return null
  const params = {}
  for (let i = 0; i < pa.length; i++) {
    if (pa[i].startsWith(':')) params[pa[i].slice(1)] = decodeURIComponent(pb[i])
    else if (pa[i] !== pb[i]) return null
  }
  return params
}
