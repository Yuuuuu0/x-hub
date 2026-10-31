<script setup lang="ts">
import { computed, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { GripVertical, Pencil, Plus, QrCode, Trash2 } from 'lucide-vue-next'
import { api, type InboundItem, type NodeItem } from '../lib/api'
import { useDragSort } from '../lib/dragSort'
import { usePolling } from '../lib/poll'
import InboundFormDrawer from '../components/InboundFormDrawer.vue'
import ShareModal from '../components/ShareModal.vue'
import SyncChip from '../components/SyncChip.vue'
import { hostOf, tagsOf } from '../lib/presets'

const POLL_MS = 5000

// 窄屏隐藏次要列，服务器与端口收进名称下方。
const COLUMNS = [
  { key: 'grip', width: 44, responsive: ['md'] },
  { title: '启用', key: 'enabled', width: 72 },
  { title: '名称', key: 'name', ellipsis: true },
  { title: '协议', key: 'protocol', responsive: ['xl'] },
  { title: '服务器', key: 'node', responsive: ['sm'] },
  { title: '端口', key: 'port', width: 96, responsive: ['sm'] },
  { title: '伪装 / 域名', key: 'host', ellipsis: true, responsive: ['xl'] },
  { key: 'actions', width: 128, align: 'right' },
]

const inbounds = ref<InboundItem[]>([])
const nodes = ref<NodeItem[]>([])
const loaded = ref(false)
const toggling = ref<Record<number, boolean>>({})

const current = ref<InboundItem | null>(null)
const showForm = ref(false)
const showShare = ref(false)

const sort = useDragSort(inbounds, 'inbounds/order', load)

async function load() {
  try {
    const [a, b] = await Promise.all([
      api.get<{ inbounds: InboundItem[] }>('inbounds'),
      api.get<{ nodes: NodeItem[] }>('nodes'),
    ])
    if (!sort.busy.value) inbounds.value = a.inbounds
    nodes.value = b.nodes
  } catch (e: any) {
    if (!loaded.value) message.error(e.message)
  } finally {
    loaded.value = true
  }
}

usePolling(load, () => POLL_MS)

const nodeOf = computed(() => new Map(nodes.value.map((n) => [n.id, n])))

function create() {
  if (!nodes.value.length) {
    message.info('请先在「服务器」中添加服务器')
    return
  }
  current.value = null
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

    <div v-else-if="!inbounds.length" class="empty">
      <p class="empty-big">表单，<br />不是 JSON。</p>
      <p class="muted">选择协议组合与服务器，密钥、ShortID 与证书一键生成，保存后主控自动组装配置并下发。</p>
      <a-button type="primary" size="large" class="add" @click="create()">
        <Plus :size="17" />
        新建第一个入站
      </a-button>
    </div>

    <template v-else>
      <p class="muted hint">拖动左侧手柄调整顺序，订阅中的节点按此顺序输出。</p>
      <div class="table" @dragenter.prevent @dragover.prevent @drop.prevent="sort.drop">
        <a-table
          :data-source="inbounds"
          :columns="COLUMNS"
          :custom-row="(r: InboundItem) => sort.itemProps(r.id)"
          :row-class-name="(r: InboundItem) => (r.enabled ? '' : 'off')"
          row-key="id"
          size="middle"
          :pagination="false"
        >
          <template #bodyCell="{ column, record }">
            <span v-if="column.key === 'grip'" v-bind="sort.gripProps(record.id)"><GripVertical :size="16" /></span>
            <a-switch
              v-else-if="column.key === 'enabled'"
              size="small"
              :checked="record.enabled"
              :loading="toggling[record.id]"
              @change="(v: any) => toggle(record, !!v)"
            />
            <template v-else-if="column.key === 'name'">
              <strong class="dim">{{ record.remark || '未命名' }}</strong>
              <span class="brief mono muted dim">{{ record.nodeName }} · {{ record.port }}</span>
            </template>
            <span v-else-if="column.key === 'protocol'" class="tags dim">
              <span v-for="t in tagsOf(record)" :key="t" class="tag">{{ t }}</span>
            </span>
            <span v-else-if="column.key === 'node'" class="server">
              <span :class="['dot', { on: nodeOf.get(record.nodeId)?.runtime.online }]" />
              <span class="server-name">{{ record.nodeName }}</span>
              <SyncChip v-if="nodeOf.get(record.nodeId) && nodeOf.get(record.nodeId)!.syncState !== 'synced'" :node="nodeOf.get(record.nodeId)!" @retried="load" />
            </span>
            <span v-else-if="column.key === 'port'" class="mono dim">{{ record.port }}</span>
            <span v-else-if="column.key === 'host'" class="mono muted sni dim">{{ hostOf(record) }}</span>
            <div v-else-if="column.key === 'actions'" class="acts">
              <a-tooltip title="分享链接 / 二维码">
                <button type="button" class="ghost" @click="share(record)"><QrCode :size="16" /></button>
              </a-tooltip>
              <a-tooltip title="编辑">
                <button type="button" class="ghost" @click="edit(record)"><Pencil :size="16" /></button>
              </a-tooltip>
              <a-tooltip title="删除">
                <button type="button" class="ghost danger" @click="remove(record)"><Trash2 :size="16" /></button>
              </a-tooltip>
            </div>
          </template>
        </a-table>
      </div>
    </template>

    <InboundFormDrawer v-model:open="showForm" :inbound="current" :nodes="nodes" @saved="load" />
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
.hint {
  margin: -12px 0 14px;
  font-size: 13px;
}
.table {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  box-shadow: var(--shadow);
  overflow: hidden;
}
.table :deep(.ant-table) {
  background: transparent;
}
.table :deep(tr.off) .dim {
  opacity: 0.45;
}
.brief {
  display: none;
  font-size: 12px;
}
.server {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 8px;
}
.server-name {
  white-space: nowrap;
}
.dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--idle);
}
.dot.on {
  background: var(--ok);
}
.tags {
  display: inline-flex;
  flex-wrap: wrap;
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
.sni {
  font-size: 12px;
}
.acts {
  display: flex;
  justify-content: flex-end;
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
@media (max-width: 767px) {
  .hint {
    display: none;
  }
}
@media (max-width: 575px) {
  .brief {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}
</style>
