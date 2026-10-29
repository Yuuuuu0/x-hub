<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { AlertTriangle, Link2, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { api, type ExternalItem, type InboundItem, type ProfileItem, type Subscription } from '../lib/api'
import { ago, bytes, percent } from '../lib/format'
import ExternalFormDrawer from '../components/ExternalFormDrawer.vue'

const items = ref<ExternalItem[]>([])
const inbounds = ref<InboundItem[]>([])
const subs = ref<Subscription[]>([])
const profiles = ref<ProfileItem[]>([])
const loaded = ref(false)
const busy = ref<Record<number, boolean>>({})
const expanded = ref<Record<number, boolean>>({})

const showForm = ref(false)
const editing = ref<ExternalItem | null>(null)

async function load() {
  try {
    const [a, b, c, d] = await Promise.all([
      api.get<{ sources: ExternalItem[] }>('external'),
      api.get<{ inbounds: InboundItem[] }>('inbounds'),
      api.get<{ subscriptions: Subscription[] }>('subscriptions'),
      api.get<{ profiles: ProfileItem[] }>('profiles'),
    ])
    items.value = a.sources
    inbounds.value = b.inbounds
    subs.value = c.subscriptions
    profiles.value = d.profiles
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loaded.value = true
  }
}
onMounted(load)

/** 各方案（含内置方案）中的分组名称，供前置分组选择。 */
const groupNames = computed(() => [...new Set(['节点选择', '自动选择', ...profiles.value.flatMap((p) => p.spec.groups.map((g) => g.name))])])

function openForm(it: ExternalItem | null) {
  editing.value = it
  showForm.value = true
}

/** 前置的可读描述。 */
function chainText(chain: string) {
  const [kind, ref] = chain.split(/:(.*)/s)
  if (kind === 'inbound') {
    const i = inbounds.value.find((x) => x.id === Number(ref))
    return i ? `经由 ${i.nodeName} · ${i.remark || '未命名'} :${i.port}` : '经由已删除的入站'
  }
  if (kind === 'group') return `经由分组 ${ref}`
  return ''
}

function scopeText(it: ExternalItem) {
  if (it.source.allSubs) return '全部订阅'
  const names = it.source.subIds.map((id) => subs.value.find((s) => s.id === id)?.name).filter(Boolean)
  return names.length ? names.join('、') : '无订阅可见'
}

/** 解析 subscription-userinfo 响应头。 */
function usage(info: string) {
  if (!info) return null
  const v: Record<string, number> = {}
  for (const part of info.split(';')) {
    const [k, n] = part.split('=').map((x) => x.trim())
    if (k && n) v[k] = Number(n)
  }
  const used = (v.upload || 0) + (v.download || 0)
  return { used, total: v.total || 0, expire: v.expire || 0 }
}

const usages = computed(() => Object.fromEntries(items.value.map((it) => [it.source.id, usage(it.source.userInfo)])))

function pct(it: ExternalItem) {
  const u = usages.value[it.source.id]
  return u ? percent(u.used, u.total) : 0
}

function expireText(ts: number) {
  const days = Math.floor((ts * 1000 - Date.now()) / 86400000)
  return days < 0 ? '已到期' : `${new Date(ts * 1000).toLocaleDateString('zh-CN')} 到期（剩 ${days} 天）`
}

async function save(it: ExternalItem, patch: Partial<ExternalItem['source']>) {
  const s = { ...it.source, ...patch }
  busy.value[s.id] = true
  try {
    const r = await api.put<ExternalItem>(`external/${s.id}`, s)
    Object.assign(it, r)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    busy.value[s.id] = false
  }
}

async function refresh(it: ExternalItem) {
  busy.value[it.source.id] = true
  try {
    const r = await api.post<ExternalItem>(`external/${it.source.id}/refresh`)
    Object.assign(it, r)
    if (r.source.fetchError) message.error(r.source.fetchError)
    else message.success(`已更新，共 ${r.nodes.length} 个节点`)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    busy.value[it.source.id] = false
  }
}

function remove(it: ExternalItem) {
  Modal.confirm({
    title: `删除「${it.source.name}」？`,
    content: '订阅中的这些节点会在客户端下次更新时消失。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`external/${it.source.id}`)
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
        <p class="eyebrow mono">EXTERNAL</p>
        <h1>外部节点</h1>
      </div>
      <a-button type="primary" size="large" class="btn" @click="openForm(null)"><Plus :size="17" />添加外部节点</a-button>
    </header>
    <p class="lead muted">不由 x-hub 管理的节点：自购的服务器、住宅代理或机场订阅。它们会和自有节点一起出现在订阅里，可以在分流方案中按名称或来源分组。</p>

    <a-skeleton v-if="!loaded" active />
    <a-empty v-else-if="!items.length" description="还没有外部节点" class="empty" />
    <div v-else class="list">
      <article v-for="it in items" :key="it.source.id" class="card" :class="{ off: !it.source.enabled }">
        <div class="top">
          <div class="title">
            <span class="chip mono">{{ it.source.kind === 'remote' ? 'SUB' : 'MANUAL' }}</span>
            <h3>{{ it.source.name }}</h3>
          </div>
          <a-switch :checked="it.source.enabled" :loading="busy[it.source.id]" size="small" @change="(v: any) => save(it, { enabled: v })" />
        </div>

        <div class="facts">
          <span>
            <strong class="mono">{{ it.nodes.filter((n) => !n.excluded).length }}</strong> 个节点
            <template v-if="it.nodes.some((n) => n.excluded)">（筛掉 {{ it.nodes.filter((n) => n.excluded).length }} 个）</template>
          </span>
          <span v-if="it.source.kind === 'remote'" class="muted">更新于 {{ ago(it.source.fetchedAt) }}</span>
          <span v-if="it.source.chain" class="chain">
            <Link2 :size="13" />{{ chainText(it.source.chain) }}
            <a-tooltip title="通用订阅（Shadowrocket、v2rayN 等）无法表达链式代理，这些节点只出现在 Clash 系与 sing-box 客户端中">
              <span class="only">仅 Clash / sing-box</span>
            </a-tooltip>
          </span>
          <span class="muted">可见：{{ scopeText(it) }}</span>
        </div>

        <div v-if="usages[it.source.id]" class="usage">
          <div class="usage-line mono">
            <span>{{ bytes(usages[it.source.id]!.used) }}</span>
            <span v-if="usages[it.source.id]!.total" class="muted">/ {{ bytes(usages[it.source.id]!.total) }}</span>
            <span v-if="usages[it.source.id]!.expire" class="muted expire">{{ expireText(usages[it.source.id]!.expire) }}</span>
          </div>
          <div v-if="usages[it.source.id]!.total" class="bar">
            <i :class="{ warn: pct(it) >= 80 }" :style="{ width: `${pct(it)}%` }" />
          </div>
        </div>

        <p v-if="it.source.fetchError" class="error"><AlertTriangle :size="14" />{{ it.source.fetchError }}</p>

        <div v-if="expanded[it.source.id]" class="tags">
          <a-tag v-for="(n, i) in it.nodes" :key="i" :bordered="false" :class="{ excluded: n.excluded }" :title="`${n.type} · ${n.server}`">{{ n.name }}</a-tag>
        </div>

        <footer class="acts">
          <a-button size="small" type="link" class="toggle" @click="expanded[it.source.id] = !expanded[it.source.id]">
            {{ expanded[it.source.id] ? '收起节点' : '查看节点' }}
          </a-button>
          <div class="row-acts">
            <a-tooltip v-if="it.source.kind === 'remote'" title="立即更新">
              <button type="button" class="ghost" :disabled="busy[it.source.id]" @click="refresh(it)"><RefreshCw :size="15" /></button>
            </a-tooltip>
            <a-tooltip title="编辑"><button type="button" class="ghost" @click="openForm(it)"><Pencil :size="15" /></button></a-tooltip>
            <a-tooltip title="删除"><button type="button" class="ghost danger" @click="remove(it)"><Trash2 :size="15" /></button></a-tooltip>
          </div>
        </footer>
      </article>
    </div>

    <ExternalFormDrawer v-model:open="showForm" :item="editing" :inbounds="inbounds" :subs="subs" :group-names="groupNames" @saved="load" />
  </section>
</template>

<style scoped>
.hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
  flex-wrap: wrap;
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
.lead {
  margin: 0 0 22px;
  max-width: 760px;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.empty {
  margin-top: 60px;
}
.list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
  gap: 16px;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px 20px 12px;
  border-radius: var(--radius);
  border: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--shadow);
  transition: opacity 0.2s;
}
.card.off {
  opacity: 0.6;
}
.top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.title h3 {
  margin: 0;
  font-family: var(--font-display);
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.02em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chip {
  flex: none;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--ink);
  color: var(--lime);
  font-size: 10.5px;
  letter-spacing: 0.08em;
}
:root[data-theme='dark'] .chip {
  background: var(--lime);
  color: var(--ink);
}
.facts {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}
.chain {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-2);
}
.only {
  margin-left: 4px;
  padding: 0 6px;
  border-radius: 999px;
  border: 1px solid color-mix(in srgb, var(--warn) 45%, transparent);
  color: var(--warn);
  font-size: 11px;
  cursor: help;
}
.usage-line {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 12.5px;
}
.expire {
  margin-left: auto;
  font-family: var(--font-body);
}
.bar {
  height: 4px;
  margin-top: 6px;
  border-radius: 999px;
  background: var(--bar-track);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  background: var(--ok);
}
.bar i.warn {
  background: var(--warn);
}
.error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin: 0;
  font-size: 12.5px;
  color: var(--bad);
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-height: 220px;
  overflow: auto;
}
.tags :deep(.ant-tag) {
  margin: 0;
}
.tags :deep(.ant-tag.excluded) {
  text-decoration: line-through;
  opacity: 0.45;
}
.acts {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px dashed var(--border);
}
.toggle {
  padding: 0;
}
.row-acts {
  display: inline-flex;
  gap: 2px;
}
.ghost {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
}
.ghost:hover {
  background: var(--surface-2);
  color: var(--text);
}
.ghost.danger:hover {
  color: var(--bad);
}
.ghost:disabled {
  opacity: 0.4;
  cursor: wait;
}
</style>
