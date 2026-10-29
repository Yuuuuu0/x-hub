// 分流方案编辑用到的常量与工具。
import type { NodeFilter } from './api'

export const BUILTINS = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS']

export const GROUP_TYPES = [
  { value: 'select', label: '手动选择' },
  { value: 'url-test', label: '自动测速' },
  { value: 'fallback', label: '故障转移' },
  { value: 'load-balance', label: '负载均衡' },
] as const

export const GROUP_TYPE_LABEL: Record<string, string> = Object.fromEntries(GROUP_TYPES.map((t) => [t.value, t.label]))

export const RULE_TYPES = [
  { value: 'RULE-SET', hint: '规则集' },
  { value: 'DOMAIN', hint: '完整域名' },
  { value: 'DOMAIN-SUFFIX', hint: '域名后缀' },
  { value: 'DOMAIN-KEYWORD', hint: '域名关键字' },
  { value: 'DOMAIN-REGEX', hint: '域名正则' },
  { value: 'GEOSITE', hint: '域名分类' },
  { value: 'GEOIP', hint: 'IP 归属地' },
  { value: 'IP-CIDR', hint: 'IPv4 段' },
  { value: 'IP-CIDR6', hint: 'IPv6 段' },
  { value: 'IP-ASN', hint: 'ASN' },
  { value: 'SRC-IP-CIDR', hint: '来源 IP 段' },
  { value: 'DST-PORT', hint: '目标端口' },
  { value: 'SRC-PORT', hint: '来源端口' },
  { value: 'PROCESS-NAME', hint: '进程名' },
  { value: 'PROCESS-PATH', hint: '进程路径' },
  { value: 'NETWORK', hint: 'tcp / udp' },
  { value: 'MATCH', hint: '兜底' },
  { value: 'RAW', hint: '原文（逻辑规则等）' },
]

/** 支持 no-resolve 的规则类型。 */
export const NO_RESOLVE = ['RULE-SET', 'GEOIP', 'IP-CIDR', 'IP-CIDR6', 'IP-ASN']

/** 把 Go 正则转换为 JS 正则（仅用于预览），开头的 (?i) 转为 i 标志。 */
function toJS(re: string) {
  return re.startsWith('(?i)') ? new RegExp(re.slice(4), 'i') : new RegExp(re)
}

/** 按筛选条件匹配节点名，与主控的展开逻辑一致；正则无效时返回 null。目前节点均为自有服务器。 */
export function matchNodes(f: NodeFilter, names: string[]): string[] | null {
  if (f.source === 'external') return []
  try {
    const inc = f.include ? toJS(f.include) : null
    const exc = f.exclude ? toJS(f.exclude) : null
    return names.filter((n) => (!inc || inc.test(n)) && (!exc || !exc.test(n)))
  } catch {
    return null
  }
}
