// 面板 API 客户端与数据类型。
import { reactive } from 'vue'

/** 面板根路径，由主控改写 <base href> 注入，如 /a8f3k2/。 */
export const BASE = new URL(document.baseURI).pathname

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

let onUnauthorized: () => void = () => {}

/** 注册会话失效回调（跳转登录页）。 */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}api/${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  let data: any = null
  try {
    data = await res.json()
  } catch {
    // 非 JSON 响应，下面按状态码处理
  }
  if (!res.ok) {
    if (res.status === 401 && path !== 'login') onUnauthorized()
    throw new ApiError(res.status, data?.error ?? `请求失败（${res.status}）`)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}

export interface XrayStatus {
  installed: boolean
  version: string
  enabled: boolean
  running: boolean
  startedAt: number
  lastError?: string
}

export interface Heartbeat {
  cpu: number
  memUsed: number
  memTotal: number
  swapUsed: number
  swapTotal: number
  diskUsed: number
  diskTotal: number
  load1: number
  load5: number
  load15: number
  uptime: number
  netRx: number
  netTx: number
}

export interface HostInfo {
  hostname: string
  os: string
  platform: string
  platformVersion: string
  kernel: string
  arch: string
  cpuModel: string
  cpuCores: number
}

export interface NodeItem {
  id: number
  name: string
  remark: string
  publicHost: string
  enrolled: boolean
  agentVersion: string
  xrayVersion: string
  caps: string[]
  host: HostInfo
  lastIp: string
  lastSeenAt: number
  createdAt: number
  appliedRev: number
  syncError: string
  syncState: SyncState
  runtime: {
    online: boolean
    connectedAt: number
    ip: string
    heartbeat: Heartbeat | null
    rxRate: number
    txRate: number
    xray: XrayStatus
    caps: string[] | null
  }
}

export type TLSMode = 'proxy' | 'file' | 'selfsigned'

export interface AccessSettings {
  listen: string
  panelPath: string
  publicUrl: string
  tlsMode: TLSMode
  certFile: string
  keyFile: string
  trustedProxies: string[] | null
}

export interface XraySettings {
  defaultVersion: string
  maxVersion: string
}

export interface SecuritySettings {
  sessionHours: number
}

export interface AllSettings {
  access: AccessSettings
  xray: XraySettings
  security: SecuritySettings
  subscription: SubscriptionSettings
  selfSignedFingerprint?: string
}

export interface Release {
  tag: string
  prerelease: boolean
  publishedAt: string
  allowed: boolean
}

/** 当前登录会话，全局共享。 */
export const session = reactive({
  loaded: false,
  authenticated: false,
  username: '',
  version: '',
  publicUrlSet: true,
})

export async function loadSession() {
  const s = await api.get<{ authenticated: boolean; username?: string; version: string; publicUrlSet?: boolean }>('session')
  Object.assign(session, {
    loaded: true,
    authenticated: s.authenticated,
    username: s.username ?? '',
    version: s.version,
    publicUrlSet: s.publicUrlSet ?? true,
  })
}

export type SyncState = 'synced' | 'pending' | 'failed'

export interface RealitySpec {
  target: string
  serverNames: string[]
  privateKey: string
  publicKey: string
  shortIds: string[]
  fingerprint: string
  spiderX: string
  minClientVer?: string
}

export interface InboundSpec {
  network: 'tcp'
  security: 'reality'
  vless: { flow: string }
  reality: RealitySpec
  sniffing: boolean
}

export interface InboundItem {
  id: number
  nodeId: number
  nodeName: string
  remark: string
  enabled: boolean
  listen: string
  port: number
  protocol: 'vless'
  spec: InboundSpec
  linkHost: string
  linkPort: number
  createdAt: number
  updatedAt: number
}

export interface Subscription {
  id: number
  name: string
  note: string
  token: string
  uuid: string
  enabled: boolean
  state: 'active' | 'exceeded' | 'expired'
  allInbounds: boolean
  inboundIds: number[]
  url: string
  limitBytes: number
  expireAt: number
  usedUp: number
  usedDown: number
  createdAt: number
}

export interface SubscriptionSettings {
  path: string
  updateHours: number
  nameTemplate: string
}

export interface ShareLink {
  subscriptionId: number
  subscriptionName: string
  link: string
}

export interface ProbeTargetResult {
  ok: boolean
  tls13: boolean
  h2: boolean
  latencyMs: number
  error?: string
}
