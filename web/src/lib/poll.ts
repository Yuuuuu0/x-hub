// 页面数据的定时刷新。
import { onBeforeUnmount, onMounted } from 'vue'

/**
 * 挂载后立即执行 fn，之后按 interval() 毫秒定时执行；页面不可见或 enabled() 为假时跳过。
 * 页面重新可见或窗口获得焦点时立即刷新：浏览器会大幅放慢后台页面的定时器
 * （如 Windows 上被其他窗口完全遮挡时），只靠定时器会让状态长时间不更新。
 * 返回的 refresh 用于操作后立即刷新，见下方说明。
 */
export function usePolling(fn: () => unknown, interval: () => number, enabled: () => boolean = () => true) {
  let timer: number | undefined
  let running = false
  let pending = false
  let stopped = false

  function schedule() {
    clearTimeout(timer)
    if (!stopped) timer = window.setTimeout(tick, interval())
  }

  /** 串行执行 fn；执行期间有新的刷新请求时，结束后再执行一次。 */
  async function run() {
    running = true
    try {
      do {
        pending = false
        await fn()
      } while (pending && !stopped)
    } finally {
      running = false
    }
  }

  async function tick() {
    clearTimeout(timer)
    if (!running && !document.hidden && enabled()) await run()
    schedule()
  }
  const wake = () => !document.hidden && tick()

  /**
   * 操作后立即刷新：不受页面可见性与 enabled() 限制；若正在加载，则等当前请求结束后再加载一次，
   * 保证拿到操作后的数据，且不会被操作前发出的旧请求覆盖。
   */
  async function refresh() {
    if (running) {
      pending = true
      return
    }
    clearTimeout(timer)
    await run()
    schedule()
  }

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
  return { refresh }
}
