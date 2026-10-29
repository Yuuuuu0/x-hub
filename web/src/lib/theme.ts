// 明暗主题：跟随系统或手动切换，选择保存在本地。
import { computed, ref } from 'vue'

export type ThemeMode = 'system' | 'light' | 'dark'

const KEY = 'xhub-theme'

function readMode(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    // 存储不可用时跟随系统
  }
  return 'system'
}

const media = window.matchMedia('(prefers-color-scheme: dark)')
const systemDark = ref(media.matches)
media.addEventListener('change', (e) => (systemDark.value = e.matches))

export const themeMode = ref<ThemeMode>(readMode())

export const isDark = computed(() => (themeMode.value === 'system' ? systemDark.value : themeMode.value === 'dark'))

/** 依次切换 系统 → 浅色 → 深色。 */
export function cycleTheme() {
  const order: ThemeMode[] = ['system', 'light', 'dark']
  themeMode.value = order[(order.indexOf(themeMode.value) + 1) % order.length]
  try {
    localStorage.setItem(KEY, themeMode.value)
  } catch {
    // 忽略
  }
}
