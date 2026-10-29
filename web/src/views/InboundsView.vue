<script setup lang="ts">
import { computed, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { Pencil, Plus, QrCode, Trash2 } from 'lucide-vue-next'
import { api, type InboundItem, type NodeItem } from '../lib/api'
import { usePolling } from '../lib/poll'
import InboundFormDrawer from '../components/InboundFormDrawer.vue'
import ShareModal from '../components/ShareModal.vue'
import SyncChip from '../components/SyncChip.vue'
import { hostOf, tagsOf } from '../lib/presets'

const POLL_MS = 5000

const inbounds = ref<InboundItem[]>([])
const nodes = ref<NodeItem[]>([])
const loaded = ref(false)
const toggling = ref<Record<number, boolean>>({})

const current = ref<InboundItem | null>(null)
const presetNode = ref<number>()
const showForm = ref(false)
const showShare = ref(false)

async function load() {
  try {
    const [a, b] = await Promise.all([
      api.get<{ inbounds: InboundItem[] }>('inbounds'),
      api.get<{ nodes: NodeItem[] }>('nodes'),
    ])
    inbounds.value = a.inbounds
    nodes.value = b.nodes
  } catch (e: any) {
    if (!loaded.value) message.error(e.message)
  } finally {
    loaded.value = true
  }
}

usePolling(load, () => POLL_MS)

/** 按服务器分组，只展示有入站的服务器。 */
const groups = computed(() =>
  nodes.value
    .map((node) => ({ node, items: inbounds.value.filter((i) => i.nodeId === node.id) }))
    .filter((g) => g.items.length),
)

function create(nodeId?: number) {
  if (!nodes.value.length) {
    message.info('请先在「服务器」中添加服务器')
    return
  }
  current.value = null
  presetNode.value = nodeId
  showForm.value = true
}

function edit(i: InboundItem) {
  current.value = i
  showForm.value = true
}

function share(i: InboundItem) {
  current.value = i
  showShare.value = true
}

async function toggle(i: InboundItem, enabled: boolean) {
  toggling.value[i.id] = true
  try {
    await api.put(`inbounds/${i.id}`, { ...i, enabled })
    i.enabled = enabled
    load()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    toggling.value[i.id] = false
  }
}

function remove(i: InboundItem) {
  Modal.confirm({
    title: `删除入站「${i.remark || `端口 ${i.port}`}」？`,
    content: '该入站将从服务器配置中移除，使用它的客户端会立即断开。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`inbounds/${i.id}`)
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
        <p class="eyebrow mono">INBOUNDS / {{ String(inbounds.length).padStart(2, '0') }}</p>
        <h1>入站</h1>
      </div>
      <a-button type="primary" size="large" class="add" @click="create()">
        <Plus :size="17" />
        新建入站
      </a-button>
    </header>

    <a-skeleton v-if="!loaded" active />

    <div v-else-if="!groups.length" class="empty">
      <p class="empty-big">表单，<br />不是 JSON。</p>
      <p class="muted">选择协议组合与服务器，密钥、ShortID 与证书一键生成，保存后主控自动组装配置并下发。</p>
      <a-button type="primary" size="large" class="add" @click="create()">
        <Plus :size="17" />
        新建第一个入站
      </a-button>
    </div>

    <div v-else class="groups">
      <section v-for="g in groups" :key="g.node.id" class="group">
        <header class="g-head">
          <span :class="['dot', { on: g.node.runtime.online }]" />
          <h2>{{ g.node.name }}</h2>
          <span class="mono muted host">{{ g.node.publicHost }}</span>
          <SyncChip :node="g.node" @retried="load" />
          <a-button size="small" class="g-add" @click="create(g.node.id)">
            <Plus :size="14" />
            添加
          </a-button>
        </header>

        <TransitionGroup name="list" tag="ul" class="rows">
          <li v-for="i in g.items" :key="i.id" :class="['row', { off: !i.enabled }]">
            <a-switch size="small" :checked="i.enabled" :loading="toggling[i.id]" @change="(v: any) => toggle(i, !!v)" />
            <div class="main">
              <strong>{{ i.remark || '未命名' }}</strong>
              <span class="tags">
                <span v-for="t in tagsOf(i)" :key="t" class="tag">{{ t }}</span>
              </span>
            </div>
            <span class="port mono">:{{ i.port }}</span>
            <span class="sni mono muted">{{ hostOf(i) }}</span>
            <div class="acts">
              <a-tooltip title="分享链接 / 二维码">
                <button type="button" class="ghost" @click="share(i)"><QrCode :size="16" /></button>
              </a-tooltip>
              <a-tooltip title="编辑">
                <button type="button" class="ghost" @click="edit(i)"><Pencil :size="16" /></button>
              </a-tooltip>
              <a-tooltip title="删除">
                <button type="button" class="ghost danger" @click="remove(i)"><Trash2 :size="16" /></button>
              </a-tooltip>
            </div>
          </li>
        </TransitionGroup>
      </section>
    </div>

    <InboundFormDrawer v-model:open="showForm" :inbound="current" :nodes="nodes" :preset-node-id="presetNode" @saved="load" />
    <ShareModal v-model:open="showShare" :inbound="current" />
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
.add {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 440px), 1fr));
  align-items: start;
  gap: 18px;
}
.group {
  container-type: inline-size;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  box-shadow: var(--shadow);
  overflow: hidden;
}
.g-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--surface-2);
}
.g-head h2 {
  margin: 0;
  font-family: var(--font-display);
  font-size: 17px;
  font-weight: 600;
  letter-spacing: -0.02em;
}
.host {
  font-size: 12px;
  margin-right: auto;
}
.g-add {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--idle);
}
.dot.on {
  background: var(--ok);
}
.rows {
  list-style: none;
  margin: 0;
  padding: 0;
}
.row {
  display: grid;
  grid-template-columns: 44px minmax(0, 1.4fr) 90px minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding: 12px 18px;
  transition: background 0.15s, opacity 0.2s;
}
.row + .row {
  border-top: 1px dashed var(--border);
}
/* 卡片较窄时（宽屏多列）把伪装目标 / SNI 移到第二行，避免名称与标签被挤压。 */
@container (max-width: 640px) {
  .row {
    grid-template-columns: 44px minmax(0, 1fr) auto auto;
    row-gap: 2px;
  }
  .sni {
    grid-column: 2 / 4;
    grid-row: 2;
  }
  .acts {
    grid-column: 4;
    grid-row: 1 / 3;
  }
}
.row:hover {
  background: var(--surface-2);
}
.row.off .main,
.row.off .port,
.row.off .sni {
  opacity: 0.45;
}
.main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.main strong {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tags {
  display: inline-flex;
  gap: 4px;
}
.tag {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.04em;
  padding: 1px 6px;
  border-radius: 5px;
  border: 1px solid var(--border);
  color: var(--text-2);
}
.port {
  font-size: 15px;
}
.sni {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.acts {
  display: flex;
  gap: 2px;
}
.ghost {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
}
.ghost:hover {
  background: var(--bg);
  color: var(--text);
}
.ghost.danger:hover {
  color: var(--bad);
}
.empty {
  padding: 56px 0;
  max-width: 560px;
}
.empty-big {
  margin: 0 0 16px;
  font-family: var(--font-display);
  font-size: clamp(56px, 9vw, 104px);
  font-weight: 700;
  letter-spacing: -0.06em;
  line-height: 0.92;
  color: transparent;
  -webkit-text-stroke: 1.5px var(--text-3);
}
.empty .muted {
  margin: 0 0 24px;
}
.list-enter-active,
.list-leave-active {
  transition: all 0.3s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.list-enter-from,
.list-leave-to {
  opacity: 0;
  transform: translateX(-8px);
}
@media (max-width: 760px) {
  .row {
    grid-template-columns: 44px minmax(0, 1fr) auto;
  }
  .sni {
    display: none;
  }
  .port {
    grid-column: 2;
    grid-row: 2;
    font-size: 12px;
  }
  .acts {
    grid-column: 3;
    grid-row: 1 / 3;
  }
  .host {
    display: none;
  }
  .g-head {
    flex-wrap: wrap;
  }
}
</style>
