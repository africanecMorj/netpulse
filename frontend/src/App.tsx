import { FormEvent, ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity, AlertTriangle, ArrowDownLeft, ArrowUpRight, Bell, Check, CheckCircle2, ChevronDown,
  Clock3, Cpu, ExternalLink, Eye, EyeOff, Globe2, HardDrive, LayoutDashboard, LockKeyhole, LogOut,
  Menu, MemoryStick, MoreHorizontal, Network, Plus, RefreshCw, Search, Server, ShieldCheck, Signal,
  Trash2, X, Copy, KeyRound, ArrowRight, RadioTower, CircleDot, ArrowDownRight, Play, Square, Flame
} from 'lucide-react'

type NodeStatus = 'unknown' | 'healthy' | 'degraded' | 'unhealthy'
type NodeInfo = { id: string; name: string; target: string; probe_type: 'http' | 'tcp'; interval_seconds: number; enabled: boolean; status: NodeStatus; last_checked_at?: string; last_latency_ms?: number; last_error?: string; created_at: string }
type Agent = { id: string; name: string; hostname: string; status: string; last_seen_at?: string; created_at: string }
type Incident = { id: string; node_id: string; status: string; severity: string; title: string; details: string; opened_at: string; resolved_at?: string }
type NodeSample = { id: number; node_id: string; checked_at: string; success: boolean; latency_ms?: number; error?: string }
type AgentMetric = { type: string; timestamp: string; hostname?: string; cpu_percent: number; memory_used_bytes: number; memory_total_bytes: number; disk_used_bytes: number; disk_total_bytes: number; network_rx_bytes_per_sec: number; network_tx_bytes_per_sec: number }
type User = { id: string; email: string; created_at: string }
type Session = { user: User; access_token: string; refresh_token: string }
type View = 'overview' | 'topology' | 'nodes' | 'agents' | 'incidents'
const API = (import.meta.env.VITE_API_URL ?? '').replace(/\/$/, '')

class APIError extends Error { status: number; constructor(message: string, status: number) { super(message); this.status = status } }
function readSession(): Session | null { try { const raw = localStorage.getItem('netpulse.session'); return raw ? JSON.parse(raw) as Session : null } catch { return null } }
function saveSession(session: Session | null) { if (session) localStorage.setItem('netpulse.session', JSON.stringify(session)); else localStorage.removeItem('netpulse.session') }
let refreshPromise: Promise<{ access_token: string; refresh_token: string }> | null = null
async function rotateSession(): Promise<{ access_token: string; refresh_token: string }> {
  if (!refreshPromise) {
    refreshPromise = (async () => {
      const current = readSession()
      if (!current?.refresh_token) throw new Error('Session expired')
      const next = await request<{ access_token: string; refresh_token: string }>('/api/v1/auth/refresh', {
        method: 'POST', body: JSON.stringify({ refresh_token: current.refresh_token })
      }, undefined, false)
      const updated = { ...current, access_token: next.access_token, refresh_token: next.refresh_token }
      saveSession(updated)
      window.dispatchEvent(new CustomEvent('netpulse:session', { detail: updated }))
      return next
    })().finally(() => { refreshPromise = null })
  }
  return refreshPromise
}
async function request<T>(path: string, options: RequestInit = {}, token?: string, retry = true): Promise<T> {
  const headers = new Headers(options.headers)
  if (options.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`${API}${path}`, { ...options, headers })
  if (response.status === 401 && token && retry) {
    try {
      const refreshed = await rotateSession()
      return request<T>(path, options, refreshed.access_token, false)
    } catch {
      saveSession(null)
      window.dispatchEvent(new Event('netpulse:logout'))
    }
  }
  if (!response.ok) {
    let message = `Request failed (${response.status})`
    try { const data = await response.json() as { error?: string }; message = data.error || message } catch { /* ignore non-JSON errors */ }
    throw new APIError(message, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
function timeAgo(value?: string) { if (!value) return 'Never'; const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return `${seconds}s ago`; if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`; if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`; return `${Math.floor(seconds / 86400)}d ago` }
function clock(value?: string) { return value ? new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '—' }
function dateLabel(value?: string) { return value ? new Date(value).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : '—' }
function statusLabel(status: string) { return ({ healthy: 'Operational', degraded: 'Degraded', unhealthy: 'Offline', unknown: 'Pending', online: 'Online', offline: 'Offline' } as Record<string, string>)[status] || status }
function bytes(value: number) { if (!value) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const i = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / Math.pow(1024, i)).toFixed(i > 1 ? 1 : 0)} ${units[i]}` }
function pct(used: number, total: number) { return total > 0 ? Math.max(0, Math.min(100, used / total * 100)) : 0 }

function Sparkline({ values, color = '#6ee7c1', fill = true, height = 58 }: { values: number[]; color?: string; fill?: boolean; height?: number }) {
  const width = 400
  const points = useMemo(() => {
    const data = values.length > 1 ? values : [0, 0]
    const max = Math.max(1, ...data) * 1.15
    return data.map((v, i) => `${(i / (data.length - 1)) * width},${height - 5 - (Math.max(0, v) / max) * (height - 12)}`).join(' ')
  }, [values, height])
  const first = points.split(' ')[0]?.split(',')[0] || '0'
  const last = points.split(' ').at(-1)?.split(',')[0] || String(width)
  return <svg className="sparkline-chart" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" aria-label="Metric history">
    {Array.from({ length: 3 }, (_, i) => <line key={i} x1="0" x2={width} y1={12 + i * (height - 18) / 2} y2={12 + i * (height - 18) / 2} stroke="currentColor" strokeOpacity=".08" strokeDasharray="3 6" />)}
    {fill && <polygon points={`${first},${height} ${points} ${last},${height}`} fill={color} opacity=".10" />}
    <polyline points={points} fill="none" stroke={color} strokeWidth="2.5" strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
    <circle cx={last} cy={Number(points.split(' ').at(-1)?.split(',')[1] || height)} r="3.5" fill={color} />
  </svg>
}
function MiniChart({ title, value, suffix = '', values, color, subtitle, icon }: { title: string; value: string; suffix?: string; values: number[]; color: string; subtitle: string; icon: ReactNode }) {
  return <article className="metric-card"><div className="metric-head"><div className={`metric-icon ${color}`} >{icon}</div><span className="metric-kicker">{title}</span><button className="quiet-icon" aria-label={`${title} options`}><MoreHorizontal size={17}/></button></div><div className="metric-value">{value}<span>{suffix}</span></div><div className="metric-foot"><span>{subtitle}</span><span className="trend-up"><ArrowUpRight size={13}/> live</span></div><Sparkline values={values} color={color === 'violet' ? '#ad92ff' : color === 'blue' ? '#73a9ff' : color === 'amber' ? '#ffbd73' : '#6ee7c1'} /></article>
}
function StatusPill({ status }: { status: string }) { return <span className={`status-pill ${status}`}><i />{statusLabel(status)}</span> }
function Modal({ title, eyebrow, onClose, children, wide = false }: { title: string; eyebrow?: string; onClose: () => void; children: ReactNode; wide?: boolean }) { return <div className="modal-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}><section className={`modal ${wide ? 'modal-wide' : ''}`}><div className="modal-head"><div>{eyebrow && <div className="eyebrow">{eyebrow}</div>}<h2>{title}</h2></div><button className="icon-button" onClick={onClose} aria-label="Close"><X size={18}/></button></div>{children}</section></div> }

export default function App() {
  const [session, setSession] = useState<Session | null>(() => readSession())
  const [authMode, setAuthMode] = useState<'login' | 'register'>('login')
  const [authEmail, setAuthEmail] = useState('')
  const [authPassword, setAuthPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [authBusy, setAuthBusy] = useState(false)
  const [authError, setAuthError] = useState('')
  const [nodes, setNodes] = useState<NodeInfo[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [nodeSamples, setNodeSamples] = useState<Record<string, NodeSample[]>>({})
  const [agentMetrics, setAgentMetrics] = useState<Record<string, AgentMetric[]>>({})
  const [view, setView] = useState<View>('overview')
  const [loading, setLoading] = useState(false)
  const [apiError, setApiError] = useState('')
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [search, setSearch] = useState('')
  const [modal, setModal] = useState<'node' | 'agent' | null>(null)
  const [nodeName, setNodeName] = useState('')
  const [nodeTarget, setNodeTarget] = useState('https://example.com')
  const [probeType, setProbeType] = useState<'http' | 'tcp'>('http')
  const [interval, setInterval] = useState('30')
  const [agentName, setAgentName] = useState('Production server')
  const [createdAgent, setCreatedAgent] = useState<{ agent: Agent; enrollment_token: string; expires_at: string } | null>(null)
  const [actionError, setActionError] = useState('')
  const [selectedNode, setSelectedNode] = useState<NodeInfo | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [testControlsEnabled, setTestControlsEnabled] = useState(false)
  const [testServerRunning, setTestServerRunning] = useState(false)
  const [testStressRunning, setTestStressRunning] = useState(false)
  const [testBusy, setTestBusy] = useState(false)
  // Synchronous guard against rapid repeated clicks before React re-renders.
  const stressActionInFlight = useRef(false)
  const [testMessage, setTestMessage] = useState('')
  const [testAgentId, setTestAgentId] = useState<string | null>(null)

  const token = session?.access_token
  const loadDashboard = useCallback(async (quiet = false) => {
    if (!token) return
    if (!quiet) setLoading(true)
    try {
      const [nodeRes, agentRes, incidentRes] = await Promise.all([
        request<{ data: NodeInfo[] }>('/api/v1/nodes', {}, token),
        request<{ data: Agent[] }>('/api/v1/agents', {}, token),
        request<{ data: Incident[] }>('/api/v1/incidents?limit=20', {}, token),
      ])
      setNodes(nodeRes.data || []); setAgents(agentRes.data || []); setIncidents(incidentRes.data || []); setApiError(''); setLastUpdated(new Date())
      const [nodeHistory, agentHistory] = await Promise.all([
        Promise.all((nodeRes.data || []).slice(0, 12).map(async n => {
          try { const res = await request<{ data: NodeSample[] }>(`/api/v1/nodes/${n.id}/metrics?limit=40`, {}, token); return [n.id, (res.data || []).slice().reverse()] as const } catch { return [n.id, []] as const }
        })),
        Promise.all((agentRes.data || []).slice(0, 12).map(async a => {
          try { const res = await request<{ data: AgentMetric[] }>(`/api/v1/agents/${a.id}/metrics?limit=40`, {}, token); return [a.id, (res.data || []).slice().reverse()] as const } catch { return [a.id, []] as const }
        }))
      ])
      setNodeSamples(Object.fromEntries(nodeHistory)); setAgentMetrics(Object.fromEntries(agentHistory))
    } catch (error) { setApiError(error instanceof Error ? error.message : 'Unable to load dashboard') }
    finally { setLoading(false) }
  }, [token])

  const refreshTestServerStatus = useCallback(async () => {
    if (!token) return
    try {
      const status = await request<{ enabled: boolean; running: boolean; stress: boolean }>('/api/v1/dev/test-server', {}, token)
      setTestControlsEnabled(status.enabled)
      setTestServerRunning(status.running)
      setTestStressRunning(status.stress)
    } catch (error) {
      setTestControlsEnabled(false)
      setTestServerRunning(false)
      setTestStressRunning(false)
      setTestMessage(error instanceof Error
        ? `Could not check Docker controls: ${error.message.replaceAll('_', ' ')}`
        : 'Could not check Docker controls. Verify that the backend is running.')
    }
  }, [token])

  useEffect(() => { const onLogout = () => setSession(null); const onSession = (event: Event) => setSession((event as CustomEvent<Session>).detail); window.addEventListener('netpulse:logout', onLogout); window.addEventListener('netpulse:session', onSession); return () => { window.removeEventListener('netpulse:logout', onLogout); window.removeEventListener('netpulse:session', onSession) } }, [])
  useEffect(() => { if (!token) return; void loadDashboard(); void refreshTestServerStatus(); const timer = window.setInterval(() => { void loadDashboard(true); void refreshTestServerStatus() }, 10000); return () => window.clearInterval(timer) }, [token, loadDashboard, refreshTestServerStatus])
  useEffect(() => { saveSession(session) }, [session])

  async function submitAuth(event: FormEvent) {
    event.preventDefault(); setAuthBusy(true); setAuthError('')
    try {
      const result = await request<Session>(`/api/v1/auth/${authMode === 'register' ? 'register' : 'login'}`, { method: 'POST', body: JSON.stringify({ email: authEmail, password: authPassword }) })
      setSession(result); setAuthPassword('')
    } catch (error) { setAuthError(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Authentication failed') }
    finally { setAuthBusy(false) }
  }
  async function logout() {
    const current = session
    setSession(null); setNodes([]); setAgents([]); setIncidents([]); saveSession(null)
    if (current?.refresh_token) { try { await request('/api/v1/auth/logout', { method: 'POST', body: JSON.stringify({ refresh_token: current.refresh_token }) }) } catch { /* local session is already cleared */ } }
  }
  async function createNode(event: FormEvent) {
    event.preventDefault(); setActionError('')
    try {
      await request('/api/v1/nodes', { method: 'POST', body: JSON.stringify({ name: nodeName.trim(), target: nodeTarget.trim(), probe_type: probeType, interval_seconds: Number(interval), enabled: true }) }, token)
      setModal(null); setNodeName(''); await loadDashboard(true)
    } catch (error) { setActionError(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Could not create node') }
  }
  async function createAgent(event: FormEvent) {
    event.preventDefault(); setActionError('')
    try {
      const result = await request<{ agent: Agent; enrollment_token: string; expires_at: string }>('/api/v1/agents', { method: 'POST', body: JSON.stringify({ name: agentName.trim() }) }, token)
      setCreatedAgent(result); await loadDashboard(true)
    } catch (error) { setActionError(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Could not create agent') }
  }
  async function launchTestServer() {
    setTestBusy(true); setTestMessage('Preparing a limited Docker test server…'); setActionError('')
    let agentId: string | null = null
    try {
      const created = await request<{ agent: Agent; enrollment_token: string }>('/api/v1/agents', { method: 'POST', body: JSON.stringify({ name: 'NetPulse Test Server' }) }, token)
      agentId = created.agent.id
      const enrolled = await request<{ agent_id: string; agent_secret: string }>('/api/v1/agents/enroll', { method: 'POST', body: JSON.stringify({ enrollment_token: created.enrollment_token, hostname: 'test-server' }) })
      await request('/api/v1/dev/test-server/start', { method: 'POST', body: JSON.stringify({ agent_secret: enrolled.agent_secret }) }, token)
      setTestAgentId(agentId); setTestServerRunning(true); setTestStressRunning(false)
      setTestMessage('Test server started. Telemetry should appear within 10–20 seconds.')
      await loadDashboard(true)
    } catch (error) {
      if (agentId) { try { await request(`/api/v1/agents/${agentId}`, { method: 'DELETE' }, token) } catch { /* keep the original error visible */ } }
      setTestMessage(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Could not start test server')
    } finally { setTestBusy(false); void refreshTestServerStatus() }
  }

 async function runTestStress(kind: 'cpu' | 'memory') {
  console.log('[stress] button clicked:', kind)
  setTestBusy(true)
  setTestMessage('Starting a bounded 5-minute stress workload…')

  try {
    console.log('[stress] sending POST request')

    const result = await request(
      '/api/v1/dev/test-server/stress',
      {
        method: 'POST',
        body: JSON.stringify({ kind }),
      },
      token,
    )

    console.log('[stress] response:', result)
    setTestStressRunning(true)
    setTestMessage(`${kind === 'cpu' ? 'CPU' : 'Memory'} stress started (maximum 5 minutes).`)
    window.setTimeout(() => void refreshTestServerStatus(), 1000)
  } catch (error) {
    console.error('[stress] failed:', error)
    setTestMessage(error instanceof Error ? error.message : 'Could not start stress workload')
  } finally {
    setTestBusy(false)
  }
}

  async function stopTestStress() {
    if (stressActionInFlight.current) return
    stressActionInFlight.current = true
    setTestBusy(true)
    try {
      await request('/api/v1/dev/test-server/stop-stress', { method: 'POST' }, token)
      setTestStressRunning(false)
      setTestMessage('Stress workload stopped.')
    } catch (error) {
      setTestMessage(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Could not stop stress workload')
    } finally {
      stressActionInFlight.current = false
      setTestBusy(false)
      void refreshTestServerStatus()
    }
  }

  async function stopTestServer() {
    setTestBusy(true); setTestMessage('Stopping the test server…')
    try {
      await request('/api/v1/dev/test-server/stop', { method: 'POST' }, token)
      const testAgent = agents.find(a => a.id === testAgentId) || agents.find(a => a.name === 'NetPulse Test Server')
      if (testAgent) { try { await request(`/api/v1/agents/${testAgent.id}`, { method: 'DELETE' }, token) } catch { /* container is stopped; refresh will show agent state */ } }
      setTestServerRunning(false); setTestStressRunning(false); setTestAgentId(null); setTestMessage('Test server stopped.')
      await loadDashboard(true)
    } catch (error) { setTestMessage(error instanceof Error ? error.message.replaceAll('_', ' ') : 'Could not stop test server') }
    finally { setTestBusy(false); void refreshTestServerStatus() }
  }

  async function deleteNode(node: NodeInfo) {
    if (!confirm(`Delete monitor “${node.name}”? This cannot be undone.`)) return
    try { await request(`/api/v1/nodes/${node.id}`, { method: 'DELETE' }, token); setSelectedNode(null); await loadDashboard(true) } catch (error) { setApiError(error instanceof Error ? error.message : 'Delete failed') }
  }
  async function revokeAgent(agent: Agent) {
    if (!confirm(`Revoke agent “${agent.name}”? Its current credential will stop working.`)) return
    try { await request(`/api/v1/agents/${agent.id}`, { method: 'DELETE' }, token); setSelectedAgent(null); await loadDashboard(true) } catch (error) { setApiError(error instanceof Error ? error.message : 'Revocation failed') }
  }
  async function copyToken() { if (!createdAgent) return; await navigator.clipboard.writeText(createdAgent.enrollment_token); setCopied(true); window.setTimeout(() => setCopied(false), 1600) }

  const onlineNodes = nodes.filter(n => n.status === 'healthy').length
  const degradedNodes = nodes.filter(n => n.status === 'degraded').length
  const downNodes = nodes.filter(n => n.status === 'unhealthy').length
  const onlineAgents = agents.filter(a => a.status === 'online').length
  const openIncidents = incidents.filter(i => i.status === 'open')
  const latencies = nodes.filter(n => typeof n.last_latency_ms === 'number' && n.status === 'healthy').map(n => n.last_latency_ms || 0)
  const avgLatency = latencies.length ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0
  const latestMetric = selectedAgent ? agentMetrics[selectedAgent.id]?.at(-1) : agents.map(a => agentMetrics[a.id]?.at(-1)).filter(Boolean).at(-1)
  const cpuValues = selectedAgent ? (agentMetrics[selectedAgent.id] || []).map(m => m.cpu_percent) : agents.flatMap(a => (agentMetrics[a.id] || []).map(m => m.cpu_percent)).slice(-40)
  const memoryValues = selectedAgent ? (agentMetrics[selectedAgent.id] || []).map(m => pct(m.memory_used_bytes, m.memory_total_bytes)) : agents.flatMap(a => (agentMetrics[a.id] || []).map(m => pct(m.memory_used_bytes, m.memory_total_bytes))).slice(-40)
  const filteredNodes = nodes.filter(n => `${n.name} ${n.target} ${n.probe_type} ${n.status}`.toLowerCase().includes(search.toLowerCase()))
  const filteredAgents = agents.filter(a => `${a.name} ${a.hostname} ${a.status}`.toLowerCase().includes(search.toLowerCase()))
  const navItems: { id: View; label: string; icon: ReactNode; count?: number }[] = [
    { id: 'overview', label: 'Overview', icon: <LayoutDashboard size={17}/> },
    { id: 'topology', label: 'Topology', icon: <Network size={17}/>, count: nodes.length + agents.length },
    { id: 'nodes', label: 'HTTP / TCP monitors', icon: <Globe2 size={17}/>, count: nodes.length },
    { id: 'agents', label: 'Infrastructure agents', icon: <Server size={17}/>, count: agents.length },
    { id: 'incidents', label: 'Incidents', icon: <AlertTriangle size={17}/>, count: openIncidents.length },
  ]

  if (!session) return <div className="auth-screen"><div className="auth-glow glow-a"/><div className="auth-glow glow-b"/><div className="auth-layout"><div className="auth-story"><div className="brand brand-large"><div className="brand-mark"><Activity size={22}/></div><span>netpulse<span className="brand-dot">.</span></span></div><div className="auth-overline"><span className="live-dot"/> OBSERVABILITY, IN FOCUS</div><h1>Your infrastructure.<br/><span>One pulse away.</span></h1><p>Know what is healthy, what is drifting, and what needs you — before users notice.</p><div className="auth-preview"><div className="preview-top"><span><span className="live-dot"/> LIVE SYSTEM MAP</span><span className="preview-coord">50.4501° N / 30.5234° E</span></div><div className="preview-map"><svg viewBox="0 0 500 230" preserveAspectRatio="xMidYMid meet"><defs><linearGradient id="authLine"><stop stopColor="#70f0cb"/><stop offset="1" stopColor="#8a82ff"/></linearGradient><filter id="authGlow"><feGaussianBlur stdDeviation="4" result="blur"/><feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs><g stroke="#7b8ba8" strokeOpacity=".25" strokeWidth="1"><path d="M0 38H500M0 76H500M0 114H500M0 152H500M0 190H500M50 0V230M100 0V230M150 0V230M200 0V230M250 0V230M300 0V230M350 0V230M400 0V230M450 0V230"/></g><g fill="none" stroke="url(#authLine)" strokeWidth="1.4" strokeDasharray="5 6"><path d="M250 115L100 45L50 150"/><path d="M250 115L400 50L450 160"/><path d="M250 115L150 195L340 200"/><path d="M100 45L400 50"/><path d="M50 150L150 195"/></g><g fill="#76f0cd" filter="url(#authGlow)"><circle cx="250" cy="115" r="5"/><circle cx="100" cy="45" r="4"/><circle cx="50" cy="150" r="3"/><circle cx="400" cy="50" r="4"/><circle cx="450" cy="160" r="3"/><circle cx="150" cy="195" r="3"/><circle cx="340" cy="200" r="3"/></g><circle cx="250" cy="115" r="19" fill="#6deac3" fillOpacity=".09" stroke="#6deac3" strokeOpacity=".35"/><circle cx="250" cy="115" r="29" fill="none" stroke="#6deac3" strokeOpacity=".18"><animate attributeName="r" values="20;34;20" dur="3s" repeatCount="indefinite"/></circle></svg><div className="map-label label-core">NETPULSE CORE <i>●</i></div><div className="map-label label-a">edge-eu-01 <i>●</i></div><div className="map-label label-b">db-primary <i>●</i></div></div><div className="preview-stats"><div><small>UPTIME</small><b>99.98<span>%</span></b></div><div><small>ACTIVE NODES</small><b>128</b></div><div><small>INGEST RATE</small><b>4.2<span>k/s</span></b></div></div></div><div className="auth-footer"><span>BUILT FOR SYSTEMS THAT MATTER</span><span>v0.2.0 · EARLY ACCESS</span></div></div><div className="auth-form-wrap"><form className="auth-form" onSubmit={submitAuth}><div className="form-mobile-brand"><div className="brand-mark"><Activity size={20}/></div><span>netpulse<span className="brand-dot">.</span></span></div><div className="auth-form-title"><span className="eyebrow">WELCOME TO NETPULSE</span><h2>{authMode === 'login' ? 'Sign in to your workspace' : 'Create your workspace'}</h2><p>{authMode === 'login' ? 'Your infrastructure is waiting.' : 'Start monitoring in less than a minute.'}</p></div><label className="field-label">Email address<input type="email" autoComplete="email" placeholder="you@company.com" value={authEmail} onChange={e => setAuthEmail(e.target.value)} required /></label><label className="field-label">Password<div className="password-wrap"><input type={showPassword ? 'text' : 'password'} autoComplete={authMode === 'login' ? 'current-password' : 'new-password'} placeholder={authMode === 'register' ? 'At least 12 characters' : 'Enter your password'} value={authPassword} onChange={e => setAuthPassword(e.target.value)} minLength={authMode === 'register' ? 12 : 1} required/><button type="button" className="password-toggle" onClick={() => setShowPassword(v => !v)} aria-label="Toggle password visibility">{showPassword ? <EyeOff size={16}/> : <Eye size={16}/>}</button></div></label>{authError && <div className="form-error"><AlertTriangle size={15}/>{authError}</div>}<button className="primary-button auth-submit" disabled={authBusy}>{authBusy ? <RefreshCw size={16} className="spin"/> : <>{authMode === 'login' ? 'Sign in' : 'Create account'} <ArrowRight size={16}/></>}</button><div className="auth-switch">{authMode === 'login' ? 'New to NetPulse?' : 'Already have an account?'} <button type="button" onClick={() => { setAuthMode(authMode === 'login' ? 'register' : 'login'); setAuthError('') }}>{authMode === 'login' ? 'Create an account' : 'Sign in instead'}</button></div><div className="auth-security"><LockKeyhole size={15}/><span>Encrypted sessions · Access tokens rotate automatically</span></div></form><div className="auth-side-note">Monitoring is only as useful as the signal behind it.</div></div></div></div>

  return <div className="app-shell"><aside className={`sidebar ${sidebarOpen ? 'sidebar-open' : ''}`}><div className="brand"><div className="brand-mark"><Activity size={20}/></div><span>netpulse<span className="brand-dot">.</span></span><button className="sidebar-close icon-button" onClick={() => setSidebarOpen(false)}><X size={17}/></button></div><div className="workspace-switch"><div className="workspace-avatar">{session.user.email.slice(0, 1).toUpperCase()}</div><div className="workspace-copy"><b>Personal workspace</b><small>Infrastructure team</small></div><ChevronDown size={15} className="muted-icon"/></div><div className="nav-caption">OBSERVABILITY</div><nav className="side-nav">{navItems.map(item => <button key={item.id} className={`nav-item ${view === item.id ? 'active' : ''}`} onClick={() => { setView(item.id); setSidebarOpen(false) }}>{item.icon}<span>{item.label}</span>{item.count !== undefined && <small className={item.id === 'incidents' && item.count > 0 ? 'nav-badge alert-badge' : 'nav-badge'}>{item.count}</small>}</button>)}</nav><div className="sidebar-spacer"/><div className="sidebar-health"><div className="health-orb"><Signal size={16}/></div><div><b>Collector status</b><small><i className="live-dot"/> API connected</small></div><span className="health-check"><Check size={14}/></span></div><div className="sidebar-user"><div className="user-avatar">{session.user.email.slice(0, 1).toUpperCase()}</div><div className="user-copy"><b>{session.user.email.split('@')[0]}</b><small>{session.user.email}</small></div><button className="quiet-icon" onClick={logout} title="Sign out"><LogOut size={16}/></button></div></aside><div className={`mobile-scrim ${sidebarOpen ? 'show' : ''}`} onClick={() => setSidebarOpen(false)}/>
    <main className="main-content"><header className="topbar"><div className="topbar-left"><button className="mobile-menu icon-button" onClick={() => setSidebarOpen(true)}><Menu size={19}/></button><div className="breadcrumbs">Workspace <span>/</span> <b>{navItems.find(n => n.id === view)?.label || 'Overview'}</b></div></div><div className="topbar-actions"><div className="system-chip"><span className="live-dot"/> SYSTEM NORMAL</div><button className="icon-button notification-button" title="Incidents" onClick={() => setView('incidents')}><Bell size={17}/>{openIncidents.length > 0 && <i/>}</button><div className="top-divider"/><button className="top-user" onClick={logout} title="Sign out"><span className="user-avatar small-avatar">{session.user.email.slice(0, 1).toUpperCase()}</span><ChevronDown size={14}/></button></div></header>
      <div className="page-content"><section className="page-heading"><div><div className="eyebrow"><span className="eyebrow-line"/> INFRASTRUCTURE / {view.toUpperCase()}</div><h1>{view === 'overview' ? <>Network overview<span className="heading-dot">.</span></> : view === 'topology' ? <>Live topology<span className="heading-dot">.</span></> : view === 'nodes' ? <>Service monitors<span className="heading-dot">.</span></> : view === 'agents' ? <>Infrastructure agents<span className="heading-dot">.</span></> : <>Incident timeline<span className="heading-dot">.</span></>}</h1><p>{view === 'overview' ? 'A live pulse on every endpoint, agent, and critical service.' : view === 'topology' ? 'Explore your infrastructure and follow the paths between systems.' : view === 'nodes' ? 'Track availability and latency across your HTTP and TCP endpoints.' : view === 'agents' ? 'Host-level telemetry from servers connected to your workspace.' : 'Every signal that needs attention, in one place.'}</p></div><div className="heading-actions"><div className="updated-chip"><Clock3 size={13}/><span>{lastUpdated ? `Updated ${clock(lastUpdated.toISOString())}` : 'Waiting for data'}</span></div><button className="secondary-button" onClick={() => void loadDashboard()} disabled={loading}><RefreshCw size={15} className={loading ? 'spin' : ''}/> Refresh</button>{view === 'agents' ? <button className="primary-button" onClick={() => { setModal('agent'); setCreatedAgent(null); setActionError('') }}><Plus size={16}/> Add agent</button> : view === 'overview' ? <><button className="secondary-button" onClick={() => { setView('agents'); setTestMessage(''); void refreshTestServerStatus() }}><Server size={15}/> Test server</button><button className="primary-button" onClick={() => { setModal('node'); setActionError('') }}><Plus size={16}/> Add monitor</button></> : view === 'nodes' ? <button className="primary-button" onClick={() => { setModal('node'); setActionError('') }}><Plus size={16}/> Add monitor</button> : null}</div></section>
      {apiError && <div className="error-banner"><AlertTriangle size={17}/><div><b>Could not refresh the workspace</b><span>{apiError.replaceAll('_', ' ')}. Check the Go API and your session.</span></div><button onClick={() => void loadDashboard()}>Retry</button><button className="error-dismiss" onClick={() => setApiError('')}><X size={15}/></button></div>}
      {view === 'overview' && <>
        <div className="overview-strip"><div className="strip-status"><span className="strip-icon"><ShieldCheck size={19}/></span><div><b>{nodes.length + agents.length === 0 ? 'Your workspace is ready' : downNodes + openIncidents.length === 0 ? 'All systems looking good' : 'Some systems need attention'}</b><span>{nodes.length + agents.length === 0 ? 'Add your first monitor or connect a server agent to start collecting signals.' : `${onlineNodes} monitors healthy · ${onlineAgents} agents online · ${openIncidents.length} open incidents`}</span></div></div><div className="strip-right"><span className="live-dot"/> LIVE MONITORING <span className="strip-separator"/> 10s refresh</div></div>
        <section className="metrics-grid"><MiniChart title="Monitored endpoints" value={String(nodes.length)} values={nodes.map(n => n.last_latency_ms || 0)} color="mint" subtitle={`${onlineNodes} operational`} icon={<Globe2 size={17}/>}/><MiniChart title="Average latency" value={nodes.length ? avgLatency.toFixed(1) : '—'} suffix={nodes.length ? ' ms' : ''} values={nodes.flatMap(n => nodeSamples[n.id] || []).slice(-30).map(s => s.latency_ms || 0)} color="blue" subtitle="Across healthy monitors" icon={<Activity size={17}/>}/><MiniChart title="Open incidents" value={String(openIncidents.length)} values={incidents.slice(-20).map(i => i.status === 'open' ? 1 : 0)} color="amber" subtitle={openIncidents.length ? 'Requires investigation' : 'No active alerts'} icon={<AlertTriangle size={17}/>}/><MiniChart title="Connected agents" value={`${onlineAgents}/${agents.length}`} values={agents.map(a => a.status === 'online' ? 1 : 0)} color="violet" subtitle="Host telemetry sources" icon={<Server size={17}/>}/></section>
        <section className="main-grid"><article className="panel topology-panel" id="topology"><PanelHeading icon={<Network size={16}/>} title="Network topology" subtitle="Relationship map · live status" action={<button className="text-action" onClick={() => setView('topology')}>Explore map <ArrowRight size={14}/></button>}/><Topology nodes={nodes} agents={agents} onNode={setSelectedNode} onAgent={setSelectedAgent}/><div className="topology-legend"><span><i className="legend-dot healthy"/> Operational</span><span><i className="legend-dot degraded"/> Degraded</span><span><i className="legend-dot unhealthy"/> Offline</span><span className="legend-right">{nodes.length + agents.length} monitored entities</span></div></article><article className="panel resources-panel"><PanelHeading icon={<Activity size={16}/>} title="Host telemetry" subtitle={selectedAgent ? selectedAgent.name : 'Latest collected agent metrics'} action={<button className="quiet-icon" onClick={() => setView('agents')}><ExternalLink size={15}/></button>}/>{latestMetric ? <><div className="resource-summary"><div><span>CPU LOAD</span><b>{latestMetric.cpu_percent.toFixed(1)}<small>%</small></b></div><div className="resource-summary-side"><span>LAST SAMPLE</span><b>{timeAgo(latestMetric.timestamp)}</b></div></div><div className="resource-row"><div className="resource-label"><span><Cpu size={15}/> CPU utilization</span><b>{latestMetric.cpu_percent.toFixed(1)}%</b></div><div className="progress-track"><div className="progress-fill mint-fill" style={{ width: `${Math.min(100, latestMetric.cpu_percent)}%` }}/></div></div><div className="resource-row"><div className="resource-label"><span><MemoryStick size={15}/> Memory used</span><b>{pct(latestMetric.memory_used_bytes, latestMetric.memory_total_bytes).toFixed(1)}%</b></div><div className="progress-track"><div className="progress-fill violet-fill" style={{ width: `${pct(latestMetric.memory_used_bytes, latestMetric.memory_total_bytes)}%` }}/></div><small className="resource-detail">{bytes(latestMetric.memory_used_bytes)} of {bytes(latestMetric.memory_total_bytes)}</small></div><div className="resource-row"><div className="resource-label"><span><HardDrive size={15}/> Disk used</span><b>{pct(latestMetric.disk_used_bytes, latestMetric.disk_total_bytes).toFixed(1)}%</b></div><div className="progress-track"><div className="progress-fill blue-fill" style={{ width: `${pct(latestMetric.disk_used_bytes, latestMetric.disk_total_bytes)}%` }}/></div><small className="resource-detail">{bytes(latestMetric.disk_used_bytes)} of {bytes(latestMetric.disk_total_bytes)}</small></div><div className="network-throughput"><div><ArrowDownLeft size={14}/><span>RX</span><b>{bytes(latestMetric.network_rx_bytes_per_sec)}/s</b></div><div><ArrowUpRight size={14}/><span>TX</span><b>{bytes(latestMetric.network_tx_bytes_per_sec)}/s</b></div></div></> : <EmptyState icon={<Activity size={21}/>} title="No telemetry received yet" body="Connect an agent to see CPU, memory, disk, and network metrics here." action={<button className="secondary-button" onClick={() => { setModal('agent'); setCreatedAgent(null) }}><Plus size={14}/> Connect an agent</button>}/>}</article></section>
        <section className="panel chart-panel"><PanelHeading icon={<Activity size={16}/>} title="Resource utilization" subtitle="Historical samples from your connected agents" action={<span className="live-badge"><span className="live-dot"/> AUTO-REFRESH</span>}/>{cpuValues.length > 0 ? <div className="history-chart"><div className="chart-legend"><span><i className="chart-dot mint-dot"/> CPU usage</span><span><i className="chart-dot violet-dot"/> Memory usage</span></div><div className="chart-area"><ChartLine values={cpuValues} color="#6ee7c1"/><ChartLine values={memoryValues} color="#ad92ff"/></div><div className="chart-axis"><span>Oldest sample</span><span>{cpuValues.length} samples</span><span>Latest</span></div></div> : <EmptyState icon={<Activity size={22}/>} title="Your charts will appear here" body="Once an agent connects and starts sending samples, CPU and memory trends will be plotted here."/>}</section>
        <section className="panel table-panel"><PanelHeading icon={<Globe2 size={16}/>} title="Endpoint health" subtitle="Availability checks from your monitors" action={<button className="text-action" onClick={() => setView('nodes')}>All monitors <ArrowRight size={14}/></button>}/><NodeTable nodes={nodes.slice(0, 5)} onSelect={setSelectedNode}/></section>
        <section className="bottom-grid"><article className="panel incidents-panel"><PanelHeading icon={<AlertTriangle size={16}/>} title="Recent incidents" subtitle="Latest changes in service health" action={<button className="text-action" onClick={() => setView('incidents')}>View timeline <ArrowRight size={14}/></button>}/><IncidentList incidents={incidents.slice(0, 4)} nodes={nodes}/></article><article className="panel agents-panel"><PanelHeading icon={<RadioTower size={16}/>} title="Connected agents" subtitle="Host-level telemetry sources" action={<button className="quiet-icon" onClick={() => setView('agents')}><ArrowRight size={15}/></button>}/>{agents.length ? <div className="compact-agents">{agents.slice(0, 4).map(a => <button className="compact-agent" key={a.id} onClick={() => setSelectedAgent(a)}><span className={`agent-mini-icon ${a.status}`}><Server size={15}/></span><span className="agent-mini-copy"><b>{a.name}</b><small>{a.hostname || 'Hostname pending'} · {timeAgo(a.last_seen_at)}</small></span><StatusPill status={a.status}/></button>)}</div> : <EmptyState icon={<Server size={21}/>} title="No agents connected" body="Install the NetPulse agent on a Linux server to start collecting host metrics." action={<button className="secondary-button" onClick={() => { setModal('agent'); setCreatedAgent(null) }}><Plus size={14}/> Add agent</button>}/>}</article></section>
      </>}
      {view === 'topology' && <section className="panel topology-full"><PanelHeading icon={<Network size={16}/>} title="Infrastructure graph" subtitle="Select any node to inspect its status" action={<span className="live-badge"><span className="live-dot"/> LIVE MAP</span>}/><Topology nodes={nodes} agents={agents} onNode={setSelectedNode} onAgent={setSelectedAgent} expanded/><div className="topology-insight-row"><div><span className="insight-dot healthy"/><b>{onlineNodes}</b><small>Healthy monitors</small></div><div><span className="insight-dot violet-dot"/><b>{onlineAgents}</b><small>Online agents</small></div><div><span className="insight-dot unhealthy"/><b>{downNodes}</b><small>Offline endpoints</small></div><div><span className="insight-dot degraded"/><b>{degradedNodes}</b><small>Degraded endpoints</small></div></div></section>}
      {view === 'nodes' && <section className="panel table-panel full-table"><div className="table-toolbar"><div><PanelHeading icon={<Globe2 size={16}/>} title="All monitors" subtitle={`${nodes.length} configured endpoints`}/></div><div className="table-controls"><label className="search-box"><Search size={15}/><input placeholder="Search endpoints..." value={search} onChange={e => setSearch(e.target.value)}/>{search && <button onClick={() => setSearch('')}><X size={13}/></button>}</label><button className="primary-button" onClick={() => { setModal('node'); setActionError('') }}><Plus size={15}/> Add monitor</button></div></div><NodeTable nodes={filteredNodes} onSelect={setSelectedNode} onDelete={deleteNode} expanded/></section>}
      {view === 'agents' && <section className="agent-view"><section className="panel test-control-panel"><div className="test-control-copy"><span className="summary-symbol mint"><Flame size={18}/></span><div><h2>Local test server</h2><p>Launch a capped Ubuntu container with the NetPulse agent and test CPU or memory alerts. Only available when local Docker controls are enabled on the API.</p><div className="test-status-line"><span className={`insight-dot ${testServerRunning ? 'healthy' : 'unhealthy'}`}/>{testServerRunning ? 'Container running' : 'Container stopped'}<span className="test-status-separator">·</span>{testStressRunning ? 'Stress active' : 'No stress workload'}</div></div></div><div className="test-control-actions">{!testControlsEnabled ? <span className="test-disabled-note">Enable local Docker controls in the backend to use this panel.</span> : !testServerRunning ? <button className="primary-button" disabled={testBusy} onClick={() => void launchTestServer()}><Play size={14}/>{testBusy ? 'Working…' : 'Launch test server'}</button> : <>{!testStressRunning ? <><button className="secondary-button" disabled={testBusy} onClick={() => void runTestStress('cpu')}><Cpu size={14}/> Start CPU stress</button><button className="secondary-button" disabled={testBusy} onClick={() => void runTestStress('memory')}><MemoryStick size={14}/> Stress memory</button></> : <button className="secondary-button" disabled={testBusy} onClick={() => void stopTestStress()}><Square size={13}/> Stop stress</button>}<button className="danger-button" disabled={testBusy} onClick={() => void stopTestServer()}><Square size={13}/> Stop test server</button></>}</div>{testMessage && <p className="test-control-message" role="status">{testMessage}</p>}</section><div className="agent-summary-grid"><div className="agent-summary-card"><span className="summary-symbol mint"><RadioTower size={17}/></span><div><small>TOTAL AGENTS</small><b>{agents.length}</b></div></div><div className="agent-summary-card"><span className="summary-symbol blue"><Signal size={17}/></span><div><small>ONLINE NOW</small><b>{onlineAgents}</b></div></div><div className="agent-summary-card"><span className="summary-symbol amber"><Clock3 size={17}/></span><div><small>STALE / OFFLINE</small><b>{agents.length - onlineAgents}</b></div></div></div><section className="panel table-panel full-table"><div className="table-toolbar"><PanelHeading icon={<Server size={16}/>} title="Infrastructure agents" subtitle="Linux host telemetry and agent credentials"/><div className="table-controls"><label className="search-box"><Search size={15}/><input placeholder="Search agents..." value={search} onChange={e => setSearch(e.target.value)}/></label><button className="primary-button" onClick={() => { setModal('agent'); setCreatedAgent(null); setActionError('') }}><Plus size={15}/> Add agent</button></div></div>{filteredAgents.length ? <div className="agents-grid">{filteredAgents.map(a => { const m = agentMetrics[a.id]?.at(-1); return <article key={a.id} className="agent-card" role="button" tabIndex={0} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') setSelectedAgent(a) }} onClick={() => setSelectedAgent(a)}><div className="agent-card-top"><span className={`agent-icon-large ${a.status}`}><Server size={19}/></span><StatusPill status={a.status}/><button className="quiet-icon card-more" onClick={e => { e.stopPropagation(); void revokeAgent(a) }} title="Revoke agent"><MoreHorizontal size={17}/></button></div><h3>{a.name}</h3><p>{a.hostname || 'Waiting for first heartbeat'}</p><div className="agent-card-stats"><div><small>CPU</small><b>{m ? `${m.cpu_percent.toFixed(1)}%` : '—'}</b></div><div><small>MEMORY</small><b>{m ? `${pct(m.memory_used_bytes, m.memory_total_bytes).toFixed(0)}%` : '—'}</b></div><div><small>LAST SEEN</small><b>{timeAgo(a.last_seen_at)}</b></div></div><div className="agent-card-bottom"><span><CircleDot size={12}/> {m ? `${agentMetrics[a.id]?.length || 0} samples collected` : 'Awaiting telemetry'}</span><ArrowRight size={15}/></div></article> })}</div> : <EmptyState icon={<Server size={23}/>} title={agents.length ? 'No matching agents' : 'Bring a server online'} body={agents.length ? 'Try a different search query.' : 'Create an enrollment token, install the NetPulse agent on your server, and start streaming host metrics.'} action={!agents.length ? <button className="primary-button" onClick={() => { setModal('agent'); setCreatedAgent(null) }}><Plus size={15}/> Create enrollment token</button> : undefined}/>}</section></section>}
      {view === 'incidents' && <section className="panel incident-page"><PanelHeading icon={<AlertTriangle size={16}/>} title="Incident timeline" subtitle={`${openIncidents.length} currently open · ${incidents.length} events loaded`}/><IncidentList incidents={incidents} nodes={nodes} expanded/></section>}
      <footer className="app-footer"><span><span className="live-dot"/> NetPulse monitoring core</span><span>API <code>{API}</code></span><span>© 2026 NETPULSE</span></footer>
      </div></main>
    {modal === 'node' && <Modal title="Add endpoint monitor" eyebrow="NEW MONITOR" onClose={() => setModal(null)}><form className="modal-form" onSubmit={createNode}><p className="modal-description">Configure an endpoint and NetPulse will start checking availability on your schedule.</p><label className="field-label">Monitor name<input placeholder="Public API" value={nodeName} onChange={e => setNodeName(e.target.value)} required maxLength={120}/></label><label className="field-label">Target address<input placeholder={probeType === 'http' ? 'https://api.example.com/health' : 'db.example.com:5432'} value={nodeTarget} onChange={e => setNodeTarget(e.target.value)} required maxLength={2048}/></label><div className="field-row"><label className="field-label">Probe protocol<select value={probeType} onChange={e => setProbeType(e.target.value as 'http' | 'tcp')}><option value="http">HTTP / HTTPS</option><option value="tcp">TCP connection</option></select></label><label className="field-label">Check interval<select value={interval} onChange={e => setInterval(e.target.value)}><option value="10">10 seconds</option><option value="30">30 seconds</option><option value="60">1 minute</option><option value="300">5 minutes</option></select></label></div>{actionError && <div className="form-error"><AlertTriangle size={15}/>{actionError}</div>}<div className="modal-actions"><button type="button" className="secondary-button" onClick={() => setModal(null)}>Cancel</button><button className="primary-button"><Plus size={15}/> Create monitor</button></div></form></Modal>}
    {modal === 'agent' && <Modal title={createdAgent ? 'Enrollment token created' : 'Connect an infrastructure agent'} eyebrow="AGENT ENROLLMENT" onClose={() => { setModal(null); setCreatedAgent(null) }} wide>
      <div className="agent-enroll-layout">
        <div className="enroll-copy">
          <p className="modal-description">Register a Linux server to stream CPU, memory, disk, and network metrics into your workspace.</p>
          {!createdAgent ? <form className="modal-form" onSubmit={createAgent}>
            <label className="field-label">Agent display name<input value={agentName} onChange={e => setAgentName(e.target.value)} placeholder="Production / EU-West" required maxLength={120}/></label>
            {actionError && <div className="form-error"><AlertTriangle size={15}/>{actionError}</div>}
            <button className="primary-button"><KeyRound size={15}/> Generate one-time token</button>
          </form> : <>
            <div className="token-warning"><AlertTriangle size={16}/><span>This token is shown only once. It expires at {dateLabel(createdAgent.expires_at)}.</span></div>
            <div className="token-box"><code>{createdAgent.enrollment_token}</code><button className="secondary-button" onClick={() => void copyToken()}>{copied ? <Check size={14}/> : <Copy size={14}/>} {copied ? 'Copied' : 'Copy token'}</button></div>
            <div className="enroll-steps"><b>Next steps</b><span>1. Build or download the NetPulse agent on your Linux server.</span><span>2. Exchange this token with <code>POST /api/v1/agents/enroll</code>.</span><span>3. Save the returned agent secret and start the agent with the API URL.</span></div>
            <button className="secondary-button" onClick={() => { setCreatedAgent(null); setActionError('') }}>Create another token</button>
          </>}
        </div>
        <div className="terminal-card"><div className="terminal-top"><span><i/><i/><i/></span><small>TERMINAL — BASH</small><Copy size={13}/></div>
          <pre><span className="terminal-muted"># 1. Exchange your enrollment token</span>{'\n'}<span className="terminal-green">curl</span> -X POST {API}/api/v1/agents/enroll \{'\n'}  -H <span className="terminal-blue">'Content-Type: application/json'</span> \{'\n'}  -d <span className="terminal-blue">'{'{'}"enrollment_token":"YOUR_TOKEN","hostname":"$(hostname)"{' }'}</span>{'\n\n'}<span className="terminal-muted"># 2. Keep the returned agent_secret private</span>{'\n'}<span className="terminal-green">chmod</span> 600 ./agent-secret</pre>
          <div className="terminal-note"><ShieldCheck size={14}/> Credentials are hashed at rest on the API.</div>
        </div>
      </div>
    </Modal>}
    {selectedNode && <Modal title={selectedNode.name} eyebrow="MONITOR DETAILS" onClose={() => setSelectedNode(null)}><div className="detail-target"><span className="target-icon"><Globe2 size={16}/></span><code>{selectedNode.target}</code><button className="quiet-icon" onClick={() => void navigator.clipboard.writeText(selectedNode.target)}><Copy size={14}/></button></div><div className="detail-stat-grid"><div><small>Current status</small><StatusPill status={selectedNode.status}/></div><div><small>Last latency</small><b>{selectedNode.last_latency_ms == null ? '—' : `${selectedNode.last_latency_ms.toFixed(1)} ms`}</b></div><div><small>Protocol</small><b>{selectedNode.probe_type.toUpperCase()}</b></div><div><small>Check interval</small><b>{selectedNode.interval_seconds}s</b></div></div><div className="detail-chart-heading"><b>Recent latency</b><span>{nodeSamples[selectedNode.id]?.length || 0} samples</span></div>{nodeSamples[selectedNode.id]?.length ? <><Sparkline values={nodeSamples[selectedNode.id].map(s => s.latency_ms || 0)} color="#6ee7c1" height={100}/><div className="detail-chart-axis"><span>{dateLabel(nodeSamples[selectedNode.id][0]?.checked_at)}</span><span>{dateLabel(nodeSamples[selectedNode.id].at(-1)?.checked_at)}</span></div></> : <p className="muted-paragraph">No samples have been recorded for this endpoint yet.</p>}{selectedNode.last_error && <div className="detail-error"><AlertTriangle size={15}/>{selectedNode.last_error}</div>}<div className="modal-actions modal-actions-split"><button className="danger-button" onClick={() => void deleteNode(selectedNode)}><Trash2 size={14}/> Delete monitor</button><button className="secondary-button" onClick={() => setSelectedNode(null)}>Close</button></div></Modal>}
    {selectedAgent && <Modal title={selectedAgent.name} eyebrow="AGENT TELEMETRY" onClose={() => setSelectedAgent(null)}><div className="agent-detail-head"><span className={`agent-icon-large ${selectedAgent.status}`}><Server size={21}/></span><div><StatusPill status={selectedAgent.status}/><small>{selectedAgent.hostname || 'Hostname pending'} · last seen {timeAgo(selectedAgent.last_seen_at)}</small></div></div>{agentMetrics[selectedAgent.id]?.length ? <><div className="agent-detail-metrics"><div><small>CPU</small><b>{agentMetrics[selectedAgent.id].at(-1)!.cpu_percent.toFixed(1)}%</b></div><div><small>MEMORY</small><b>{pct(agentMetrics[selectedAgent.id].at(-1)!.memory_used_bytes, agentMetrics[selectedAgent.id].at(-1)!.memory_total_bytes).toFixed(1)}%</b></div><div><small>DISK</small><b>{pct(agentMetrics[selectedAgent.id].at(-1)!.disk_used_bytes, agentMetrics[selectedAgent.id].at(-1)!.disk_total_bytes).toFixed(1)}%</b></div></div><div className="detail-chart-heading"><b>CPU history</b><span>{agentMetrics[selectedAgent.id].length} samples</span></div><Sparkline values={agentMetrics[selectedAgent.id].map(m => m.cpu_percent)} color="#ad92ff" height={100}/><div className="detail-chart-heading"><b>Memory history</b></div><Sparkline values={agentMetrics[selectedAgent.id].map(m => pct(m.memory_used_bytes, m.memory_total_bytes))} color="#6ee7c1" height={85}/></> : <EmptyState icon={<Activity size={20}/>} title="Waiting for telemetry" body="The agent is registered. Metrics will appear after it connects to the WebSocket endpoint."/>}<div className="modal-actions modal-actions-split"><button className="danger-button" onClick={() => void revokeAgent(selectedAgent)}><Trash2 size={14}/> Revoke agent</button><button className="secondary-button" onClick={() => setSelectedAgent(null)}>Close</button></div></Modal>}
  </div>
}

function PanelHeading({ icon, title, subtitle, action }: { icon: ReactNode; title: string; subtitle: string; action?: ReactNode }) { return <div className="panel-heading"><div className="panel-title-icon">{icon}</div><div className="panel-heading-copy"><h2>{title}</h2><p>{subtitle}</p></div><div className="panel-heading-action">{action}</div></div> }
function EmptyState({ icon, title, body, action }: { icon: ReactNode; title: string; body: string; action?: ReactNode }) { return <div className="empty-state"><div className="empty-state-icon">{icon}</div><b>{title}</b><p>{body}</p>{action}</div> }
function Topology({ nodes, agents, onNode, onAgent, expanded = false }: { nodes: NodeInfo[]; agents: Agent[]; onNode: (n: NodeInfo) => void; onAgent: (a: Agent) => void; expanded?: boolean }) {
  const entities = [...nodes.map(n => ({ id: n.id, name: n.name, kind: 'monitor' as const, status: n.status, detail: n.target, node: n })), ...agents.map(a => ({ id: a.id, name: a.name, kind: 'agent' as const, status: a.status, detail: a.hostname || 'Linux host', agent: a }))].slice(0, expanded ? 16 : 8)
  const positions = expanded ? [{x:50,y:17},{x:21,y:27},{x:79,y:27},{x:11,y:53},{x:35,y:49},{x:65,y:49},{x:89,y:53},{x:22,y:77},{x:50,y:81},{x:78,y:77},{x:39,y:23},{x:61,y:23},{x:14,y:68},{x:86,y:68},{x:34,y:65},{x:66,y:65}] : [{x:50,y:22},{x:23,y:42},{x:77,y:42},{x:15,y:75},{x:39,y:76},{x:62,y:76},{x:86,y:75},{x:50,y:55}]
  const width = 1000, height = expanded ? 480 : 310
  return <div className={`topology-canvas ${expanded ? 'topology-expanded' : ''}`}><div className="topology-grid"/><svg className="topology-links" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none"><defs><linearGradient id="topologyGradient"><stop stopColor="#6ee7c1" stopOpacity=".8"/><stop offset="1" stopColor="#8b83ff" stopOpacity=".7"/></linearGradient></defs>{entities.map((entity, i) => { const p = positions[i]; const x = p.x / 100 * width; const y = p.y / 100 * height; const centerX = width / 2; const centerY = height * .52; return <g key={entity.id}><path d={`M ${centerX} ${centerY} Q ${(centerX+x)/2 + (i%2 ? 18 : -18)} ${(centerY+y)/2} ${x} ${y}`} fill="none" stroke="url(#topologyGradient)" strokeWidth="1.3" strokeOpacity=".46" strokeDasharray={entity.status === 'offline' || entity.status === 'unhealthy' ? '3 7' : '5 7'} className="link-path"/><circle r="2.8" fill={entity.status === 'unhealthy' || entity.status === 'offline' ? '#ff7e8b' : '#6ee7c1'}><animateMotion dur={`${4 + i % 4}s`} repeatCount="indefinite" path={`M ${centerX} ${centerY} Q ${(centerX+x)/2 + (i%2 ? 18 : -18)} ${(centerY+y)/2} ${x} ${y}`}/></circle></g> })}<circle cx={width/2} cy={height*.52} r="38" fill="none" stroke="#6ee7c1" strokeOpacity=".16"><animate attributeName="r" values="32;48;32" dur="4s" repeatCount="indefinite"/></circle></svg><div className="topology-core" style={{ left: '50%', top: '52%' }}><div className="core-orb"><Activity size={expanded ? 24 : 20}/></div><b>NetPulse core</b><small>API · PostgreSQL</small></div>{entities.map((entity, i) => { const p = positions[i]; return <button key={entity.id} className={`graph-node ${entity.kind} ${entity.status}`} style={{ left: `${p.x}%`, top: `${p.y}%` }} onClick={() => entity.kind === 'monitor' ? onNode(entity.node) : onAgent(entity.agent)}><span className="graph-node-icon">{entity.kind === 'monitor' ? <Globe2 size={16}/> : <Server size={16}/>}<i/></span><b>{entity.name}</b><small>{entity.kind === 'monitor' ? entity.node.last_latency_ms != null ? `${entity.node.last_latency_ms.toFixed(0)} ms` : statusLabel(entity.status) : entity.agent.hostname || statusLabel(entity.status)}</small></button>})}{entities.length === 0 && <div className="topology-empty-overlay"><div className="empty-orb"><Network size={24}/></div><b>Your network map starts here</b><span>Add monitors and connect agents to populate this topology.</span></div>}</div>
}
function NodeTable({ nodes, onSelect, onDelete, expanded = false }: { nodes: NodeInfo[]; onSelect: (n: NodeInfo) => void; onDelete?: (n: NodeInfo) => void; expanded?: boolean }) { return <div className="table-wrap"><table><thead><tr><th>ENDPOINT</th><th>STATUS</th><th>LATENCY</th><th>PROTOCOL</th><th>LAST CHECK</th><th>INTERVAL</th>{expanded && <th/>}</tr></thead><tbody>{nodes.map(n => <tr key={n.id} onClick={() => onSelect(n)} className="clickable-row"><td><div className="endpoint-cell"><span className="endpoint-icon"><Globe2 size={16}/></span><span><b>{n.name}</b><small>{n.target}</small></span></div></td><td><StatusPill status={n.status}/></td><td><span className={n.last_latency_ms && n.last_latency_ms > 500 ? 'latency-high' : ''}>{n.last_latency_ms == null ? '—' : `${n.last_latency_ms.toFixed(1)} ms`}</span></td><td><span className="protocol-pill">{n.probe_type.toUpperCase()}</span></td><td className="time-cell">{timeAgo(n.last_checked_at)}</td><td>{n.interval_seconds}s</td>{expanded && <td><button className="quiet-icon" onClick={e => { e.stopPropagation(); onDelete?.(n) }} title="Delete monitor"><Trash2 size={14}/></button></td>}</tr>)}{!nodes.length && <tr><td colSpan={expanded ? 7 : 6} className="empty-row"><div className="table-empty"><Globe2 size={19}/><b>No monitors yet</b><span>Add an HTTP or TCP endpoint to begin tracking uptime and latency.</span></div></td></tr>}</tbody></table></div> }
function IncidentList({ incidents, nodes, expanded = false }: { incidents: Incident[]; nodes: NodeInfo[]; expanded?: boolean }) { return <div className={`events-list ${expanded ? 'events-expanded' : ''}`}>{incidents.map(i => { const n = nodes.find(node => node.id === i.node_id); return <div className="event-row" key={i.id}><span className={`event-icon ${i.status === 'open' ? i.severity : 'resolved'}`}>{i.status === 'open' ? <AlertTriangle size={16}/> : <CheckCircle2 size={16}/>}</span><div className="event-copy"><b>{i.title}</b><span>{n?.name || i.details || 'Endpoint health changed'} · {i.details}</span></div><div className="event-meta"><StatusPill status={i.status === 'open' ? (i.severity === 'critical' ? 'unhealthy' : 'degraded') : 'healthy'}/><time>{dateLabel(i.opened_at)}</time></div></div> })}{!incidents.length && <div className="all-clear"><span><ShieldCheck size={19}/></span><div><b>No incidents to report</b><small>New incidents will appear here when a monitor changes state.</small></div></div>}</div> }
function ChartLine({ values, color }: { values: number[]; color: string }) { const width = 700, height = 180; const points = values.map((v, i) => `${(i / Math.max(1, values.length - 1)) * width},${height - 8 - Math.min(100, Math.max(0, v)) / 100 * (height - 18)}`).join(' '); return <svg className="chart-line" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none"><polyline points={points} fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" vectorEffect="non-scaling-stroke"/><circle cx={values.length ? width : 0} cy={height - 8 - Math.min(100, Math.max(0, values.at(-1) || 0)) / 100 * (height - 18)} r="4" fill={color}/></svg> }
