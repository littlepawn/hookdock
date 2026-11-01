const TOKEN_KEY = 'hookdock.token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(v) {
  if (v) localStorage.setItem(TOKEN_KEY, v)
  else localStorage.removeItem(TOKEN_KEY)
}

/* 用新令牌探测一次身份；成功才落盘，避免存进错令牌把界面搞成一片 401 */
export async function verifyToken(v) {
  const res = await fetch('/api/whoami', { headers: v ? { 'X-Api-Token': v } : {} })
  const data = await res.json().catch(() => null)
  return { ok: res.ok, status: res.status, data }
}

async function req(method, path, body) {
  const headers = {}
  const t = getToken()
  if (t) headers['X-Api-Token'] = t
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { raw: text }
  }
  if (!res.ok) {
    const msg = (data && (data.error || data.raw)) || `HTTP ${res.status}`
    const err = new Error(msg)
    err.status = res.status
    throw err
  }
  return data
}

export const api = {
  whoami: () => req('GET', '/api/whoami'),
  overview: () => req('GET', '/api/overview'),
  endpoints: () => req('GET', '/api/endpoints'),
  endpoint: (key) => req('GET', `/api/endpoints/${encodeURIComponent(key)}`),
  createEndpoint: (b) => req('POST', '/api/endpoints', b),
  updateEndpoint: (key, b) => req('PUT', `/api/endpoints/${encodeURIComponent(key)}`, b),
  deleteEndpoint: (key) => req('DELETE', `/api/endpoints/${encodeURIComponent(key)}`),
  requests: (key, limit = 50, before = 0) =>
    req('GET', `/api/endpoints/${encodeURIComponent(key)}/requests?limit=${limit}${before ? `&before=${before}` : ''}`),
  request: (id) => req('GET', `/api/requests/${encodeURIComponent(id)}`),
  makeJob: (id, b) => req('POST', `/api/requests/${encodeURIComponent(id)}/make-job`, b),
  rules: (key) => req('GET', `/api/endpoints/${encodeURIComponent(key)}/rules`),
  createRule: (key, b) => req('POST', `/api/endpoints/${encodeURIComponent(key)}/rules`, b),
  deleteRule: (id) => req('DELETE', `/api/rules/${id}`),
  jobs: () => req('GET', '/api/jobs'),
  job: (id) => req('GET', `/api/jobs/${encodeURIComponent(id)}`),
  createJob: (b) => req('POST', '/api/jobs', b),
  updateJob: (id, b) => req('PUT', `/api/jobs/${encodeURIComponent(id)}`, b),
  deleteJob: (id) => req('DELETE', `/api/jobs/${encodeURIComponent(id)}`),
  toggleJob: (id) => req('POST', `/api/jobs/${encodeURIComponent(id)}/toggle`),
  runJob: (id) => req('POST', `/api/jobs/${encodeURIComponent(id)}/run`),
  jobRuns: (id) => req('GET', `/api/jobs/${encodeURIComponent(id)}/runs`),
  runs: (q = '') => req('GET', `/api/runs?${q}`),
  run: (id) => req('GET', `/api/runs/${encodeURIComponent(id)}`),
  retryRun: (id) => req('POST', `/api/runs/${encodeURIComponent(id)}/retry`),
  tokens: () => req('GET', '/api/tokens'),
  createToken: (b) => req('POST', '/api/tokens', b),
  updateToken: (id, b) => req('PUT', `/api/tokens/${id}`, b),
  deleteToken: (id) => req('DELETE', `/api/tokens/${id}`),
}

export function subscribe(onEvent) {
  const t = getToken()
  const es = new EventSource('/api/stream' + (t ? `?token=${encodeURIComponent(t)}` : ''))
  es.addEventListener('message', (e) => {
    try {
      onEvent(JSON.parse(e.data))
    } catch {
      /* 忽略坏帧 */
    }
  })
  es.onerror = () => {
    /* EventSource 自带重连 */
  }
  return () => es.close()
}
