// 页面数据的定时刷新。
import { onBeforeUnmount, onMounted } from 'vue'

/**
 * 挂载后立即执行 fn，之后按 interval() 毫秒定时执行；页面不可见或 enabled() 为假时跳过。
 * 页面重新可见或窗口获得焦点时立即刷新：浏览器会大幅放慢后台页面的定时器
 * （如 Windows 上被其他窗口完全遮挡时），只靠定时器会让状态长时间不更新。
 */
export function usePolling(fn: () => unknown, interval: () => number, enabled: () => boolean = () => true) {
  let timer: number | undefined
  let running = false
  let stopped = false

  async function tick() {
    clearTimeout(timer)
    if (!running && !document.hidden && enabled()) {
      running = true
      try {
        await fn()
      } finally {
        running = false
      }
    }
    if (!stopped) timer = window.setTimeout(tick, interval())
  }
  const wake = () => !document.hidden && tick()

  onMounted(() => {
    tick()
    document.addEventListener('visibilitychange', wake)
    window.addEventListener('focus', wake)
  })
  onBeforeUnmount(() => {
    stopped = true
    clearTimeout(timer)
    document.removeEventListener('visibilitychange', wake)
    window.removeEventListener('focus', wake)
  })
  return { refresh: tick }
}
