// 入站协议组合预设：表单按组合显示对应参数区块，列表据此生成标签。
import type { InboundItem } from './api'

export type Protocol = 'vless' | 'vmess' | 'trojan' | 'shadowsocks'
export type Network = 'tcp' | 'ws' | 'xhttp'
export type Security = 'reality' | 'tls' | 'none'

export interface Preset {
  key: string
  label: string
  hint: string
  protocol: Protocol
  network: Network
  security: Security
  vision?: boolean
}

export const PRESETS: Preset[] = [
  { key: 'vless-reality-vision', label: 'VLESS + Reality + Vision', hint: '推荐。无需域名与证书，直连延迟低', protocol: 'vless', network: 'tcp', security: 'reality', vision: true },
  { key: 'vless-xhttp-reality', label: 'VLESS + XHTTP + Reality', hint: '无需证书，HTTP 分包传输；sing-box 客户端不支持', protocol: 'vless', network: 'xhttp', security: 'reality' },
  { key: 'vless-ws-tls', label: 'VLESS + WS + TLS', hint: '需要域名与证书，可套 Cloudflare 等 CDN', protocol: 'vless', network: 'ws', security: 'tls' },
  { key: 'vmess-ws-tls', label: 'VMess + WS + TLS', hint: '兼容老旧客户端，可套 CDN。Xray 已将 VMess 标记为弃用，新建建议用 VLESS', protocol: 'vmess', network: 'ws', security: 'tls' },
  { key: 'trojan-tls', label: 'Trojan + TLS', hint: '需要域名与证书', protocol: 'trojan', network: 'tcp', security: 'tls' },
  { key: 'ss2022', label: 'Shadowsocks 2022', hint: '无 TLS 伪装，适合中转或内网互联', protocol: 'shadowsocks', network: 'tcp', security: 'none' },
]

/** 根据入站的协议组合找出对应预设。 */
export function presetOf(i: Pick<InboundItem, 'protocol' | 'spec'>): Preset | undefined {
  return PRESETS.find(
    (p) =>
      p.protocol === i.protocol &&
      p.network === i.spec.network &&
      p.security === i.spec.security &&
      (p.protocol !== 'vless' || p.network !== 'tcp' || !!p.vision === !!i.spec.vless?.flow),
  ) ?? PRESETS.find((p) => p.protocol === i.protocol && p.network === i.spec.network && p.security === i.spec.security)
}

/** 入站列表中的协议标签。 */
export function tagsOf(i: Pick<InboundItem, 'protocol' | 'spec'>): string[] {
  const proto = { vless: 'VLESS', vmess: 'VMess', trojan: 'Trojan', shadowsocks: 'SS2022' }[i.protocol]
  const tags = [proto]
  if (i.spec.network === 'ws') tags.push('WS')
  if (i.spec.network === 'xhttp') tags.push('XHTTP')
  if (i.spec.security === 'reality') tags.push('Reality')
  if (i.spec.security === 'tls') tags.push('TLS')
  if (i.spec.vless?.flow) tags.push('Vision')
  return tags
}

/** 入站列表中显示的伪装 / 证书域名。 */
export function hostOf(i: Pick<InboundItem, 'spec'>): string {
  return i.spec.reality?.serverNames[0] ?? i.spec.tls?.serverName ?? i.spec.ss?.method ?? ''
}
