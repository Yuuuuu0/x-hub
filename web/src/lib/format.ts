// 数值与时间的展示格式化。

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

/** 字节数格式化为 1.23 GB。 */
export function bytes(n: number, digits = 1): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), UNITS.length - 1)
  const v = n / 1024 ** i
  return `${v.toFixed(i === 0 ? 0 : v >= 100 ? 0 : digits)} ${UNITS[i]}`
}

/** 拆分为数值与单位，便于大号数字与小号单位分开排版。 */
export function bytesParts(n: number): [string, string] {
  const [v, u] = bytes(n).split(' ')
  return [v, u]
}

/** 时长（秒）格式化为 3天 4小时 / 5小时 12分。 */
export function duration(sec: number): string {
  if (!sec || sec < 60) return `${Math.max(0, Math.floor(sec || 0))}秒`
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}天 ${h}小时`
  if (h > 0) return `${h}小时 ${m}分`
  return `${m}分钟`
}

/** Unix 秒时间戳格式化为相对时间。 */
export function ago(ts: number): string {
  if (!ts) return '从未'
  const s = Math.floor(Date.now() / 1000) - ts
  if (s < 60) return '刚刚'
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  return `${Math.floor(s / 86400)} 天前`
}

/** Unix 秒时间戳格式化为本地日期时间。 */
export function datetime(ts: number): string {
  return new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false })
}

export function percent(used: number, total: number): number {
  return total > 0 ? Math.min(100, (used / total) * 100) : 0
}
