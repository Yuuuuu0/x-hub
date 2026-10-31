<script setup lang="ts">
import { computed, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { GripVertical, Layers, Plus } from 'lucide-vue-next'
import { api, type CountryItem, type NodeItem } from '../lib/api'
import { usePolling } from '../lib/poll'
import { useDragSort } from '../lib/dragSort'
import { bytesParts } from '../lib/format'
import NodeCard from '../components/NodeCard.vue'
import NodeFormModal from '../components/NodeFormModal.vue'
import InstallCommandModal from '../components/InstallCommandModal.vue'
import XrayInstallModal from '../components/XrayInstallModal.vue'
import LogsDrawer from '../components/LogsDrawer.vue'
import BatchUpgradeModal from '../components/BatchUpgradeModal.vue'

// 安装命令弹窗打开时加快刷新，接入后尽快看到上线。
const POLL_MS = 5000
const POLL_FAST_MS = 2000

const nodes = ref<NodeItem[]>([])
const countries = ref<CountryItem[]>([])
const agentVersion = ref('') // 主控分发的被控端版本
const loaded = ref(false)
const busy = ref<Record<number, boolean>>({})

const current = ref<NodeItem | null>(null)
const showForm = ref(false)
const showInstall = ref(false)
const showVersions = ref(false)
const showLogs = ref(false)
const showBatch = ref(false)

const sort = useDragSort(nodes, 'nodes/order', load)

async function load() {
  try {
    const r = await api.get<{ nodes: NodeItem[]; agentVersion: string; countries: CountryItem[] }>('nodes')
    if (!sort.busy.value) nodes.value = r.nodes
    agentVersion.value = r.agentVersion
    countries.value = r.countries
  } catch (e: any) {
    if (!loaded.value) message.error(e.message)
  } finally {
    loaded.value = true
  }
}

usePolling(load, () => (showInstall.value ? POLL_FAST_MS : POLL_MS))

/** 弹窗使用列表中的最新数据，以便实时显示接入状态。 */
const live = computed(() => nodes.value.find((n) => n.id === current.value?.id) ?? current.value)

const stats = computed(() => {
  const enrolled = nodes.value.filter((n) => n.enrolled)
  const online = enrolled.filter((n) => n.runtime.online)
  return {
    total: nodes.value.length,
    online: online.length,
    xray: online.filter((n) => n.runtime.xray.running).length,
    rx: bytesParts(online.reduce((s, n) => s + n.runtime.rxRate, 0)),
    tx: bytesParts(online.reduce((s, n) => s + n.runtime.txRate, 0)),
  }
})

function openForm(n: NodeItem | null) {
  current.value = n
  showForm.value = true
}

function open(n: NodeItem, which: 'install' | 'versions' | 'logs') {
  current.value = n
  ;({ install: showInstall, versions: showVersions, logs: showLogs })[which].value = true
}

function onSaved(n: NodeItem, created: boolean) {
  load()
  if (created) {
    current.value = n
    showInstall.value = true
  } else {
    message.success('已保存')
  }
}

const ACTION_TEXT = { start: '启动', stop: '停止', restart: '重启' } as const

async function xray(n: NodeItem, action: keyof typeof ACTION_TEXT) {
  busy.value[n.id] = true
  try {
    await api.post(`nodes/${n.id}/xray`, { action })
    message.success(`已${ACTION_TEXT[action]} ${n.name} 的 Xray`)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    busy.value[n.id] = false
    load()
  }
}

function remove(n: NodeItem) {
  Modal.confirm({
    title: `删除「${n.name}」？`,
    content: '服务器记录将被删除，被控端将无法再连接主控。被控端程序需在 VPS 上手动卸载。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`nodes/${n.id}`)
      message.success('已删除')
      load()
    },
  })
}
</script>

<template>
  <section class="page">
    <header class="hero">
      <div>
        <p class="eyebrow mono">NODES / {{ String(stats.total).padStart(2, '0') }}</p>
        <h1>服务器</h1>
      </div>
      <div class="hero-acts">
        <a-button v-if="nodes.some((n) => n.enrolled)" size="large" class="add" @click="showBatch = true">
          <Layers :size="17" />
          批量切换 Xray
        </a-button>
        <a-button type="primary" size="large" class="add" @click="openForm(null)">
          <Plus :size="17" />
          添加服务器
        </a-button>
      </div>
    </header>

    <div v-if="stats.total" class="strip">
      <div class="cell">
        <span class="k">在线</span>
        <span class="v mono">{{ stats.online }}<small>/ {{ stats.total }}</small></span>
      </div>
      <div class="cell">
        <span class="k">Xray 运行中</span>
        <span class="v mono">{{ stats.xray }}<small>/ {{ stats.online }}</small></span>
      </div>
      <div class="cell">
        <span class="k">总下行</span>
        <span class="v mono">{{ stats.rx[0] }}<small>{{ stats.rx[1] }}/s</small></span>
      </div>
      <div class="cell">
        <span class="k">总上行</span>
        <span class="v mono">{{ stats.tx[0] }}<small>{{ stats.tx[1] }}/s</small></span>
      </div>
    </div>

    <div v-if="!loaded" class="grid">
      <div v-for="i in 3" :key="i" class="skeleton"><a-skeleton active /></div>
    </div>

    <div v-else-if="!nodes.length" class="empty">
      <p class="empty-big">还没有<br />服务器。</p>
      <p class="muted">添加一台服务器，复制安装命令到 VPS 上执行，几秒后它就会出现在这里。</p>
      <a-button type="primary" size="large" class="add" @click="openForm(null)">
        <Plus :size="17" />
        添加第一台服务器
      </a-button>
    </div>

    <TransitionGroup v-else name="list" tag="div" class="grid" @dragenter.prevent @dragover.prevent @drop.prevent="sort.drop">
      <NodeCard
        v-for="n in nodes"
        v-bind="sort.itemProps(n.id)"
        :key="n.id"
        :node="n"
        :agent-version="agentVersion"
        :busy="!!busy[n.id]"
        @edit="openForm(n)"
        @install="open(n, 'install')"
        @versions="open(n, 'versions')"
        @logs="open(n, 'logs')"
        @remove="remove(n)"
        @xray="(a) => xray(n, a)"
        @synced="load"
      >
        <template #grip>
          <span v-bind="sort.gripProps(n.id)"><GripVertical :size="16" /></span>
        </template>
      </NodeCard>
    </TransitionGroup>

    <NodeFormModal v-model:open="showForm" :node="current" :countries="countries" @saved="onSaved" />
    <InstallCommandModal v-model:open="showInstall" :node="live" />
    <XrayInstallModal v-model:open="showVersions" :node="current" @done="load" />
    <LogsDrawer v-model:open="showLogs" :node="current" />
    <BatchUpgradeModal v-model:open="showBatch" :nodes="nodes" @done="load" />
  </section>
</template>

<style scoped>
.hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 28px;
}

.eyebrow {
  margin: 0 0 6px;
  font-size: 12px;
  letter-spacing: 0.18em;
  color: var(--text-3);
}

h1 {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(44px, 6vw, 76px);
  font-weight: 700;
  letter-spacing: -0.05em;
  line-height: 0.95;
}

.hero-acts {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.add {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  margin-bottom: 24px;
  overflow: hidden;
}

.cell {
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cell + .cell {
  border-left: 1px solid var(--border);
}

.k {
  font-size: 12px;
  color: var(--text-3);
}

.v {
  font-size: 26px;
  letter-spacing: -0.04em;
  line-height: 1.1;
}

.v small {
  font-size: 13px;
  margin-left: 5px;
  letter-spacing: 0;
  color: var(--text-3);
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
  gap: 16px;
}

.skeleton {
  padding: 20px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--border);
}

.empty {
  padding: 56px 0;
  max-width: 560px;
}

.empty-big {
  margin: 0 0 16px;
  font-family: var(--font-display);
  font-size: clamp(56px, 9vw, 112px);
  font-weight: 700;
  letter-spacing: -0.06em;
  line-height: 0.9;
  color: transparent;
  -webkit-text-stroke: 1.5px var(--text-3);
}

.empty .muted {
  margin: 0 0 24px;
}

.list-enter-active,
.list-leave-active,
.list-move {
  transition: all 0.35s cubic-bezier(0.2, 0.8, 0.2, 1);
}

.list-enter-from,
.list-leave-to {
  opacity: 0;
  transform: translateY(12px);
}

@media (max-width: 760px) {
  .strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .cell:nth-child(3) {
    border-left: 0;
  }
  .cell:nth-child(n + 3) {
    border-top: 1px solid var(--border);
  }
  .hero {
    flex-wrap: wrap;
  }
}
</style>
