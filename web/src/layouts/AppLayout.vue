<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Globe, LogOut, Monitor, Moon, Network, Server, Settings, Split, Sun, Users } from 'lucide-vue-next'
import { api, session } from '../lib/api'
import { cycleTheme, themeMode } from '../lib/theme'
import PublicUrlModal from '../components/PublicUrlModal.vue'

const router = useRouter()
const nav = [
  { name: 'nodes', label: '服务器', icon: Server },
  { name: 'inbounds', label: '入站', icon: Network },
  { name: 'external', label: '外部节点', icon: Globe },
  { name: 'subscriptions', label: '订阅', icon: Users },
  { name: 'routing', label: '分流', icon: Split },
  { name: 'settings', label: '设置', icon: Settings },
]

const themeIcon = computed(() => ({ system: Monitor, light: Sun, dark: Moon })[themeMode.value])
const themeLabel = computed(() => ({ system: '跟随系统', light: '浅色', dark: '深色' })[themeMode.value])
const showPublicUrl = ref(!session.publicUrlSet)

async function logout() {
  await api.post('logout').catch(() => {})
  session.authenticated = false
  router.replace({ name: 'login' })
}
</script>

<template>
  <div class="shell">
    <aside class="rail">
      <router-link to="/nodes" class="brand">
        <span class="mark" aria-hidden="true">
          <svg viewBox="0 0 32 32"><path d="M9 9l14 14M23 9L9 23" /></svg>
        </span>
        <span class="word">x<span class="dash">—</span>hub</span>
      </router-link>

      <nav class="nav">
        <router-link v-for="item in nav" :key="item.name" :to="{ name: item.name }" class="nav-item">
          <component :is="item.icon" :size="18" :stroke-width="1.75" />
          <span>{{ item.label }}</span>
        </router-link>
      </nav>

      <div class="rail-foot">
        <a-tooltip :title="`主题：${themeLabel}`" placement="right">
          <button class="icon-btn" type="button" :aria-label="`切换主题，当前${themeLabel}`" @click="cycleTheme">
            <component :is="themeIcon" :size="17" :stroke-width="1.75" />
          </button>
        </a-tooltip>
        <div class="who">
          <span class="who-name">{{ session.username }}</span>
        </div>
        <a-tooltip title="退出登录" placement="right">
          <button class="icon-btn" type="button" aria-label="退出登录" @click="logout">
            <LogOut :size="17" :stroke-width="1.75" />
          </button>
        </a-tooltip>
      </div>
    </aside>

    <main class="main">
      <router-view />
    </main>

    <PublicUrlModal v-model:open="showPublicUrl" />
  </div>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 232px minmax(0, 1fr);
}

.rail {
  position: sticky;
  top: 0;
  height: 100vh;
  display: flex;
  flex-direction: column;
  padding: 22px 14px 18px;
  border-right: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 72%, transparent);
  backdrop-filter: blur(12px);
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 8px 26px;
  color: var(--text);
  text-decoration: none;
}

.mark {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  background: var(--ink);
  display: grid;
  place-items: center;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.08);
}

.mark svg {
  width: 20px;
  height: 20px;
  fill: none;
  stroke: var(--lime);
  stroke-width: 3.2;
  stroke-linecap: round;
}

.word {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: 21px;
  letter-spacing: -0.04em;
}

.dash {
  color: var(--lime-ink);
}

.nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 10px;
  color: var(--text-2);
  text-decoration: none;
  font-size: 14px;
  transition:
    background 0.18s,
    color 0.18s;
}

.nav-item:hover {
  background: var(--surface-2);
  color: var(--text);
}

.nav-item.router-link-active {
  background: var(--ink);
  color: #fff;
}

:root[data-theme='dark'] .nav-item.router-link-active {
  background: var(--lime);
  color: var(--ink);
}

.rail-foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}

.who {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.who-name {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.icon-btn {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-2);
  cursor: pointer;
  transition:
    color 0.15s,
    border-color 0.15s;
}

.icon-btn:hover {
  color: var(--text);
  border-color: var(--border-strong);
}

.main {
  min-width: 0;
  padding: 36px clamp(16px, 4vw, 56px) 64px;
}

@media (max-width: 860px) {
  .shell {
    grid-template-columns: minmax(0, 1fr);
  }
  .rail {
    position: sticky;
    z-index: 10;
    height: auto;
    flex-direction: row;
    align-items: center;
    padding: 10px 16px;
    border-right: 0;
    border-bottom: 1px solid var(--border);
    gap: 8px;
  }
  .brand {
    padding: 0;
  }
  .word {
    display: none;
  }
  .nav {
    flex-direction: row;
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .nav-item {
    flex: none;
    gap: 8px;
    padding: 8px 10px;
    white-space: nowrap;
  }
  .rail-foot {
    margin: 0;
    padding: 0;
    border: 0;
  }
  .who {
    display: none;
  }
  .main {
    padding: 24px 16px 48px;
  }
}
</style>
