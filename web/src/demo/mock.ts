// 演示模式的接口模拟：读取接口返回预置数据，修改类接口提示演示环境只读。
// 预置数据由真实主控生成（见 docs/development.md「演示环境」），仅在 VITE_DEMO 构建中打包。
import raw from './fixtures.json'
import { ApiError } from '../lib/api'

const fixtures = raw as Record<string, any>

/** 预置数据中的时间戳字段，按生成时间平移到当前时间，避免到期时间等随时间失真。 */
const TIME_KEYS = new Set(['createdAt', 'updatedAt', 'expireAt', 'periodStartAt', 'lastSeenAt', 'connectedAt', 'startedAt', 'fetchedAt', 'start'])
const shift = Math.floor(Date.now() / 1000) - fixtures.capturedAt

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v), (k, x) => (TIME_KEYS.has(k) && typeof x === 'number' && x > 0 ? x + shift : x))
}

/** 在基准值附近小幅波动，让服务器卡片的实时数据看起来在变化。 */
function jitter(v: number, ratio: number) {
  return Math.max(0, v * (1 + (Math.random() * 2 - 1) * ratio))
}

function nodes() {
  const r = clone(fixtures.nodes)
  for (const n of r.nodes) {
    const hb = n.runtime.heartbeat
    hb.cpu = Math.min(100, jitter(hb.cpu, 0.35))
    n.runtime.rxRate = Math.round(jitter(n.runtime.rxRate, 0.4))
    n.runtime.txRate = Math.round(jitter(n.runtime.txRate, 0.4))
  }
  return r
}

/** 按订阅 id 生成稳定的伪随机流量明细：最近 30 天合计与订阅的已用流量一致。 */
function traffic(id: number, days: number) {
  let seed = id * 9301 + 49297
  const rand = () => ((seed = (seed * 9301 + 49297) % 233280) / 233280)
  const sub = fixtures.subscriptions.subscriptions.find((s: any) => s.id === id)
  const n = Math.max(days, 30)
  const raw = Array.from({ length: n }, () => [0.3 + rand() * 1.4, 0.02 + rand() * 0.04])
  const recent = raw.slice(-30).reduce((a, [w]) => a + w, 0)
  const unit = sub ? sub.usedDown / recent : 1 << 30
  const daily = raw.slice(-days).map(([w, r], k) => {
    const d = new Date(Date.now() - (days - 1 - k) * 86400_000)
    const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    const down = Math.round(w * unit)
    return { day, up: Math.round(down * r), down }
  })
  const all = fixtures.inbounds.inbounds as any[]
  const ids: number[] = sub ? (sub.allInbounds ? all.map((i) => i.id).filter((x) => !sub.inboundIds.includes(x)) : sub.inboundIds) : []
  const weights = ids.map(() => rand() + 0.2)
  const sum = weights.reduce((a, b) => a + b, 0)
  const byInbound = ids.map((iid, k) => {
    const ib = all.find((i) => i.id === iid)
    const share = weights[k] / sum
    return { nodeId: ib.nodeId, inboundId: iid, nodeName: ib.nodeName, remark: ib.remark, deleted: false, up: Math.round(sub.usedUp * share), down: Math.round(sub.usedDown * share) }
  })
  return { daily, byInbound: byInbound.sort((a, b) => b.down - a.down) }
}

const LOGS = [
  'xhub-agent: connected to master',
  'xhub-agent: config rev applied, xray restarted in 182ms',
  'Xray 26.10.31 (Xray, Penetrates Everything.) Custom (go1.26.3 linux/amd64)',
  '[Info] infra/conf/serial: Reading config: &{Name:/etc/xhub/xray.json Format:json}',
  '[Warning] core: Xray 26.10.31 started',
  'xhub-agent: traffic report: 8 users, 3 online',
]

/** 只读或无副作用的 POST 接口。 */
function post(path: string, body: any): unknown {
  if (path === 'login' || path === 'logout') return {}
  if (path === 'tools/x25519') return { privateKey: 'aGVsbG8tZGVtby1wcml2YXRlLWtleS1vbmx5LXgtaHViLQ', publicKey: 'ZGVtby1wdWJsaWMta2V5LWZvci14LWh1Yi1zaG93Y2FzZQ' }
  if (path === 'profiles/preview') {
    const id = body?.spec && fixtures.profiles.profiles.find((p: any) => JSON.stringify(p.spec.groups) === JSON.stringify(body.spec.groups))?.id
    return clone(fixtures[`preview/${id ?? 1}/${body?.client ?? 'mihomo'}`])
  }
  if (/^nodes\/\d+\/install-command$/.test(path)) return { ...fixtures['install-command'], expireAt: Math.floor(Date.now() / 1000) + 3600 }
  if (/^nodes\/\d+\/probe$/.test(path)) {
    return body?.kind === 'reality_target'
      ? { ok: true, tls13: true, h2: true, latencyMs: 12 + Math.round(Math.random() * 20) }
      : { ok: true, names: ['demo.example.com'], notAfter: Math.floor(Date.now() / 1000) + 80 * 86400 }
  }
  if (path === 'rulesets/check') return { rules: 1280 }
  if (path === 'account/totp/setup') return { secret: 'JBSWY3DPEHPK3PXP', uri: 'otpauth://totp/x-hub:demo?secret=JBSWY3DPEHPK3PXP&issuer=x-hub' }
  throw new ApiError(403, '演示环境为只读，修改不会保存')
}

function get(path: string): unknown {
  const [p, q] = path.split('?')
  const params = new URLSearchParams(q)
  if (p === 'session') return { authenticated: true, username: 'demo', version: 'demo', publicUrlSet: true, totpEnabled: false }
  if (p === 'nodes') return nodes()
  if (p === 'tools/short-id') return { shortId: Math.random().toString(16).slice(2, 18).padEnd(16, '0') }
  if (p === 'tools/ss-key') return { key: 'ZGVtby1zczIwMjIta2V5LTE2Yg==' }
  let m = p.match(/^subscriptions\/(\d+)\/traffic$/)
  if (m) return traffic(+m[1], +(params.get('days') ?? 30))
  m = p.match(/^nodes\/\d+\/logs$/)
  if (m) return { lines: LOGS.map((l, i) => `${new Date(Date.now() - (LOGS.length - i) * 60_000).toISOString()} ${l}`) }
  if (p in fixtures) return clone(fixtures[p])
  throw new ApiError(404, '演示环境不支持该操作')
}

/** 演示模式下替代 fetch 的请求处理，带少量延迟模拟网络。 */
export async function demoRequest<T>(method: string, path: string, body?: unknown): Promise<T> {
  await new Promise((r) => setTimeout(r, 120))
  if (method === 'GET') return get(path) as T
  if (method === 'POST') return post(path, body) as T
  throw new ApiError(403, '演示环境为只读，修改不会保存')
}
