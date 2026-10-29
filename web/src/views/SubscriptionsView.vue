<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { BarChart3, ChevronDown, KeyRound, Link2, MonitorSmartphone, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { api, type InboundItem, type Subscription } from '../lib/api'
import { datetime } from '../lib/format'
import SubLinkModal from '../components/SubLinkModal.vue'
import SubDetailDrawer from '../components/SubDetailDrawer.vue'
import UsageMeter from '../components/UsageMeter.vue'

const GB = 1024 ** 3
const RESET_DAYS = [{ value: 0, label: '不自动重置' }, ...Array.from({ length: 28 }, (_, i) => ({ value: i + 1, label: `每月 ${i + 1} 日` }))]

const subs = ref<Subscription[]>([])
const loaded = ref(false)
const busy = ref<Record<number, boolean>>({})

const inbounds = ref<InboundItem[]>([])
const showForm = ref(false)
const editing = ref<Subscription | null>(null)
const form = reactive({
  name: '',
  note: '',
  allInbounds: true,
  inboundIds: [] as number[],
  limitGB: 0,
  expire: undefined as string | undefined, // Unix 秒字符串（date-picker valueFormat=X）
  resetDay: 0,
})
const detailSub = ref<Subscription | null>(null)
const showDetail = ref(false)
let timer: number | undefined
const saving = ref(false)
const linkSub = ref<Subscription | null>(null)
const showLink = ref(false)

/** 入站按服务器分组，供范围选择。 */
const inboundOptions = computed(() => {
  const groups = new Map<string, { label: string; options: { value: number; label: string }[] }>()
  for (const i of inbounds.value) {
    if (!groups.has(i.nodeName)) groups.set(i.nodeName, { label: i.nodeName, options: [] })
    groups.get(i.nodeName)!.options.push({ value: i.id, label: `${i.nodeName} · ${i.remark || '未命名'} :${i.port}` })
  }
  return [...groups.values()]
})

async function load() {
  try {
    const [a, b] = await Promise.all([
      api.get<{ subscriptions: Subscription[] }>('subscriptions'),
      api.get<{ inbounds: InboundItem[] }>('inbounds'),
    ])
    subs.value = a.subscriptions
    inbounds.value = b.inbounds
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loaded.value = true
  }
}

onMounted(() => {
  load()
  timer = window.setInterval(() => !document.hidden && !showForm.value && load(), 10000)
})
onBeforeUnmount(() => clearInterval(timer))

function openDetail(s: Subscription) {
  detailSub.value = s
  showDetail.value = true
}

function openForm(s: Subscription | null) {
  editing.value = s
  form.name = s?.name ?? ''
  form.note = s?.note ?? ''
  form.allInbounds = s?.allInbounds ?? true
  form.inboundIds = [...(s?.inboundIds ?? [])]
  form.limitGB = s ? +(s.limitBytes / GB).toFixed(2) : 0
  form.expire = s?.expireAt ? String(s.expireAt) : undefined
  form.resetDay = s?.resetDay ?? 0
  showForm.value = true
}

function openLink(s: Subscription) {
  linkSub.value = s
  showLink.value = true
}

function scopeText(s: Subscription) {
  return s.allInbounds ? '全部入站' : `${s.inboundIds.length} 个入站`
}

async function save() {
  if (!form.allInbounds && !form.inboundIds.length) {
    message.error('请至少选择一个入站，或选择「全部入站」')
    return
  }
  saving.value = true
  const body = {
    name: form.name,
    note: form.note,
    allInbounds: form.allInbounds,
    inboundIds: form.inboundIds,
    limitBytes: Math.round((form.limitGB || 0) * GB),
    expireAt: form.expire ? Number(form.expire) : 0,
    resetDay: form.resetDay,
  }
  try {
    if (editing.value) await api.put(`subscriptions/${editing.value.id}`, { ...body, enabled: editing.value.enabled })
    else await api.post('subscriptions', body)
    showForm.value = false
    message.success('已保存，正在同步到各服务器')
    load()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}

async function toggle(s: Subscription, enabled: boolean) {
  busy.value[s.id] = true
  try {
    await api.put(`subscriptions/${s.id}`, { name: s.name, note: s.note, enabled })
    s.enabled = enabled
    message.success(enabled ? `已启用「${s.name}」` : `已停用「${s.name}」，相关客户端将无法连接`)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    busy.value[s.id] = false
  }
}

function onReset(s: Subscription, key: string) {
  if (key === 'token') resetToken(s)
  else resetCredentials(s)
}

function resetToken(s: Subscription) {
  Modal.confirm({
    title: `重置「${s.name}」的订阅链接？`,
    content: '旧订阅链接立即失效，需要把新链接重新发给使用者；已导入的节点仍可继续使用。',
    okText: '重置链接',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.post(`subscriptions/${s.id}/reset-token`)
      message.success('订阅链接已重置')
      load()
    },
  })
}

function resetCredentials(s: Subscription) {
  Modal.confirm({
    title: `重置「${s.name}」的凭据？`,
    content: '将生成新的 UUID，已导入旧配置的客户端会立即失效，需要重新导入。',
    okText: '重置',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.post(`subscriptions/${s.id}/reset-credentials`)
      message.success('凭据已重置')
      load()
    },
  })
}

function remove(s: Subscription) {
  Modal.confirm({
    title: `删除订阅「${s.name}」？`,
    content: '该订阅的凭据会从所有服务器移除，使用它的客户端将无法连接。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`subscriptions/${s.id}`)
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
        <p class="eyebrow mono">SUBSCRIPTIONS / {{ String(subs.length).padStart(2, '0') }}</p>
        <h1>订阅</h1>
      </div>
      <a-button type="primary" size="large" class="add" @click="openForm(null)">
        <Plus :size="17" />
        新建订阅
      </a-button>
    </header>
    <p class="lead muted">每个订阅代表一位使用者，拥有独立凭据与订阅链接，凭据自动下发到其范围内的所有入站。</p>

    <a-skeleton v-if="!loaded" active />
    <a-empty v-else-if="!subs.length" description="还没有订阅" />
    <div v-else class="grid">
      <article v-for="s in subs" :key="s.id" :class="['card', { off: !s.enabled }]">
        <header class="top">
          <div class="title">
            <h3>{{ s.name }}</h3>
            <p v-if="s.note" class="muted note">{{ s.note }}</p>
          </div>
          <a-switch :checked="s.enabled" :loading="busy[s.id]" @change="(v: any) => toggle(s, !!v)" />
        </header>
        <UsageMeter :sub="s" />
        <dl class="meta">
          <dt>在线</dt>
          <dd>
            <a-tooltip v-if="s.onlineIps.length" :title="s.onlineIps.join('、')">
              <span class="online"><MonitorSmartphone :size="13" />{{ s.onlineIps.length }} 个设备</span>
            </a-tooltip>
            <span v-else class="muted">无</span>
          </dd>
          <dt>到期</dt>
          <dd class="mono">{{ s.expireAt ? datetime(s.expireAt) : '永不' }}</dd>
          <dt>UUID</dt>
          <dd class="mono">
            <a-typography-text :copyable="{ text: s.uuid }">{{ s.uuid }}</a-typography-text>
          </dd>
          <dt>范围</dt>
          <dd>{{ scopeText(s) }}</dd>
        </dl>
        <footer class="acts">
          <a-button size="small" type="primary" @click="openLink(s)"><Link2 :size="13" class="ico" />订阅链接</a-button>
          <a-button size="small" @click="openDetail(s)"><BarChart3 :size="13" class="ico" />流量</a-button>
          <a-button size="small" @click="openForm(s)"><Pencil :size="13" class="ico" />编辑</a-button>
          <a-dropdown trigger="click">
            <a-button size="small">重置<ChevronDown :size="13" class="ico-r" /></a-button>
            <template #overlay>
              <a-menu @click="({ key }: any) => onReset(s, key)">
                <a-menu-item key="token"><span class="mi"><RefreshCw :size="13" />重置订阅链接</span></a-menu-item>
                <a-menu-item key="credentials"><span class="mi"><KeyRound :size="13" />重置连接凭据</span></a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
          <a-button size="small" danger class="del" @click="remove(s)"><Trash2 :size="13" /></a-button>
        </footer>
      </article>
    </div>

    <a-modal v-model:open="showForm" :title="editing ? '编辑订阅' : '新建订阅'" :confirm-loading="saving" ok-text="保存" @ok="save">
      <a-form layout="vertical" class="form" @submit.prevent="save">
        <a-form-item label="名称" required>
          <a-input v-model:value="form.name" :maxlength="32" placeholder="如：朋友A" />
        </a-form-item>
        <a-form-item label="备注">
          <a-textarea v-model:value="form.note" :maxlength="200" :auto-size="{ minRows: 2, maxRows: 4 }" />
        </a-form-item>
        <a-form-item label="入站范围" extra="「全部入站」会自动包含之后新建的入站。">
          <a-radio-group v-model:value="form.allInbounds" button-style="solid">
            <a-radio-button :value="true">全部入站</a-radio-button>
            <a-radio-button :value="false">指定入站</a-radio-button>
          </a-radio-group>
        </a-form-item>
        <a-form-item v-if="!form.allInbounds" label="可使用的入站">
          <a-select v-model:value="form.inboundIds" mode="multiple" :options="inboundOptions" placeholder="选择入站" not-found-content="还没有入站" />
        </a-form-item>
        <div class="row">
          <a-form-item label="流量上限（GB）" extra="0 表示不限；上下行合计，超出后自动停用。">
            <a-input-number v-model:value="form.limitGB" :min="0" :step="10" class="full" />
          </a-form-item>
          <a-form-item label="流量重置">
            <a-select v-model:value="form.resetDay" :options="RESET_DAYS" />
          </a-form-item>
        </div>
        <a-form-item label="到期时间" extra="留空表示永不到期；到期后自动停用。">
          <a-date-picker v-model:value="form.expire" show-time value-format="X" format="YYYY-MM-DD HH:mm" class="full" placeholder="永不到期" />
        </a-form-item>
      </a-form>
    </a-modal>
    <SubLinkModal v-model:open="showLink" :sub="linkSub" />
    <SubDetailDrawer v-model:open="showDetail" :sub="detailSub && (subs.find((x) => x.id === detailSub!.id) ?? detailSub)" @changed="load" />
  </section>
</template>

<style scoped>
.page {
  max-width: 1200px;
}
.hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
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
  margin: 0 0 24px;
}
.add {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
  gap: 16px;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 20px;
  border-radius: var(--radius);
  border: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--shadow);
  transition: opacity 0.2s;
}
.card.off {
  background: var(--surface-2);
}
.card.off .title,
.card.off .meta {
  opacity: 0.5;
}
.top {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.title {
  flex: 1;
  min-width: 0;
}
h3 {
  margin: 0;
  font-family: var(--font-display);
  font-size: 24px;
  font-weight: 600;
  letter-spacing: -0.03em;
}
.note {
  margin: 4px 0 0;
  font-size: 13px;
}
.meta {
  display: grid;
  grid-template-columns: 56px 1fr;
  gap: 6px 10px;
  margin: 0;
  font-size: 12.5px;
}
.meta dt {
  color: var(--text-3);
}
.meta dd.mono :deep(.ant-typography) {
  font-family: var(--font-mono);
}
.meta dd {
  margin: 0;
  min-width: 0;
  word-break: break-all;
}
.acts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: auto;
  padding-top: 14px;
  border-top: 1px dashed var(--border);
}
.acts :deep(.ant-btn) {
  display: inline-flex;
  align-items: center;
}
.del {
  margin-left: auto;
}
.ico {
  margin-right: 5px;
}
.ico-r {
  margin-left: 4px;
}
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 14px;
}
.full {
  width: 100%;
}
.online {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--ok);
  cursor: help;
}
.mi {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.form {
  margin-top: 16px;
}
</style>
