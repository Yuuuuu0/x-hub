<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { Modal, message } from 'ant-design-vue'
import { ArrowLeft, Copy, GripVertical, Pencil, Plus, RefreshCw, Save, Trash2 } from 'lucide-vue-next'
import { api, ApiError, type ProfileItem, type ProfileSpec, type RouteGroup, type RouteRule, type PreviewNode, type RuleSetItem, type Subscription } from '../lib/api'
import { BUILTINS, GROUP_TYPE_LABEL, NO_RESOLVE, RULE_TYPES, matchNodes } from '../lib/routing'
import { copyText } from '../lib/clipboard'
import CodeEditor from '../components/CodeEditor.vue'
import GroupDrawer from '../components/GroupDrawer.vue'
import RuleSetModal from '../components/RuleSetModal.vue'

const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)

const loaded = ref(false)
const tab = ref('groups')
const name = ref('')
const note = ref('')
const spec = reactive<ProfileSpec>({ groups: [], rules: [], mihomo: { base: '' }, singbox: { base: '' }, shadowrocket: { base: '' } })
const client = ref<'mihomo' | 'singbox' | 'shadowrocket'>('mihomo')
const CLIENTS = [
  { value: 'mihomo', label: 'Mihomo' },
  { value: 'singbox', label: 'sing-box' },
  { value: 'shadowrocket', label: 'Shadowrocket' },
]
const snapshot = ref('')
const saving = ref(false)
const issues = ref<string[]>([])

const ruleSets = ref<RuleSetItem[]>([])
const subs = ref<Subscription[]>([])

const preview = reactive({ subId: undefined as number | undefined, loading: false, config: '', warnings: [] as string[], issues: [] as string[], nodes: [] as PreviewNode[] })

const current = () => JSON.stringify({ name: name.value, note: note.value, spec })
const dirty = computed(() => loaded.value && current() !== snapshot.value)

async function load() {
  try {
    const [p, a, b] = await Promise.all([
      api.get<ProfileItem>(`profiles/${id}`),
      api.get<{ ruleSets: RuleSetItem[] }>('rulesets'),
      api.get<{ subscriptions: Subscription[] }>('subscriptions'),
    ])
    name.value = p.name
    note.value = p.note
    Object.assign(spec, {
      groups: p.spec.groups ?? [],
      rules: p.spec.rules ?? [],
      mihomo: { base: p.spec.mihomo?.base ?? '' },
      singbox: { base: p.spec.singbox?.base ?? '' },
      shadowrocket: { base: p.spec.shadowrocket?.base ?? '' },
    })
    ruleSets.value = a.ruleSets
    subs.value = b.subscriptions
    preview.subId = (subs.value.find((s) => s.profileId === id) ?? subs.value[0])?.id
    snapshot.value = current()
    loaded.value = true
    runPreview()
  } catch (e: any) {
    message.error(e.message)
    router.replace({ name: 'routing' })
  }
}

async function reloadRuleSets() {
  ruleSets.value = (await api.get<{ ruleSets: RuleSetItem[] }>('rulesets')).ruleSets
}

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    await api.put(`profiles/${id}`, { name: name.value, note: note.value, spec })
    snapshot.value = current()
    issues.value = []
    message.success('已保存，客户端下次更新订阅时生效')
  } catch (e: any) {
    if (e instanceof ApiError && e.data?.issues) issues.value = e.data.issues
    message.error(e.message)
  } finally {
    saving.value = false
  }
}

/** 以编辑器中的内容（含未保存的修改）渲染预览订阅的配置。 */
async function runPreview() {
  if (!preview.subId) return
  preview.loading = true
  try {
    const r = await api.post<{ config: string; warnings: string[]; issues: string[]; nodes?: PreviewNode[] }>('profiles/preview', {
      subscriptionId: preview.subId,
      client: client.value,
      spec,
    })
    preview.config = r.config
    preview.warnings = r.warnings
    preview.issues = r.issues
    if (r.nodes) preview.nodes = r.nodes
  } catch (e: any) {
    preview.config = ''
    preview.warnings = []
    preview.issues = [e.message]
  } finally {
    preview.loading = false
  }
}

watch(tab, (t) => t === 'preview' && runPreview())
watch(client, () => tab.value === 'preview' && runPreview())

/** 以默认模板填充当前客户端（sing-box / Shadowrocket）的基础配置。 */
async function fillDefault() {
  const d = await api.get<{ singbox: string; shadowrocket: string }>('profiles/defaults')
  if (client.value === 'singbox') spec.singbox.base = d.singbox
  else if (client.value === 'shadowrocket') spec.shadowrocket.base = d.shadowrocket
}

const LANG = { mihomo: 'yaml', singbox: 'json', shadowrocket: 'text' } as const

function onKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 's') {
    e.preventDefault()
    save()
  }
}
function onUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}
onMounted(() => {
  load()
  window.addEventListener('keydown', onKey)
  window.addEventListener('beforeunload', onUnload)
  window.addEventListener('mouseup', disarm)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('beforeunload', onUnload)
  window.removeEventListener('mouseup', disarm)
})
onBeforeRouteLeave(() => {
  if (!dirty.value) return true
  return new Promise<boolean>((resolve) =>
    Modal.confirm({ title: '有未保存的修改', content: '离开后修改将丢失。', okText: '离开', okType: 'danger', cancelText: '留下', onOk: () => resolve(true), onCancel: () => resolve(false) }),
  )
})

// ---- 拖动排序：按住手柄拖动行 ----
const drag = reactive({ armed: '', list: '' as '' | 'groups' | 'rules', from: -1, over: -1 })
function arm(list: 'groups' | 'rules', i: number) {
  drag.armed = `${list}:${i}`
}
function disarm() {
  drag.armed = ''
}
function onDragStart(list: 'groups' | 'rules', i: number, e: DragEvent) {
  drag.list = list
  drag.from = i
  e.dataTransfer!.effectAllowed = 'move'
}
function onDragOver(list: 'groups' | 'rules', i: number) {
  if (drag.list === list) drag.over = i
}
function onDrop(list: 'groups' | 'rules') {
  if (drag.list !== list || drag.from < 0 || drag.over < 0 || drag.from === drag.over) return endDrag()
  const arr = spec[list] as unknown[]
  const [item] = arr.splice(drag.from, 1)
  arr.splice(drag.over, 0, item)
  endDrag()
}
function endDrag() {
  Object.assign(drag, { armed: '', list: '', from: -1, over: -1 })
}

// ---- 分组 ----
const groupNames = computed(() => spec.groups.map((g) => g.name))
const showGroup = ref(false)
const editingGroup = ref(-1)

function openGroup(i: number) {
  editingGroup.value = i
  showGroup.value = true
}

/** 节点筛选匹配到的节点数，未包含节点时为 -1，正则无效时为 null。 */
function nodeCount(g: RouteGroup) {
  if (!g.nodes) return -1
  return matchNodes(g.nodes, preview.nodes)?.length ?? null
}

function ruleRefs(n: string) {
  return spec.rules.filter((r) => r.target === n).length
}

/** 保存分组；改名时同步更新其他分组的成员与规则目标。 */
function saveGroup(g: RouteGroup) {
  const i = editingGroup.value
  if (i < 0) {
    spec.groups.push(g)
    return
  }
  const old = spec.groups[i].name
  spec.groups[i] = g
  if (old === g.name) return
  for (const other of spec.groups) other.proxies = other.proxies.map((m) => (m === old ? g.name : m))
  for (const r of spec.rules) {
    if (r.target === old) r.target = g.name
    if (r.type === 'RAW' && r.raw) r.raw = r.raw.split(',').map((p) => (p.trim() === old ? g.name : p)).join(',')
  }
}

function removeGroup(i: number) {
  const g = spec.groups[i]
  const refs = ruleRefs(g.name)
  Modal.confirm({
    title: `删除分组「${g.name}」？`,
    content: refs ? `有 ${refs} 条规则以它为目标，删除后这些规则将改为 DIRECT。` : undefined,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk() {
      spec.groups.splice(i, 1)
      for (const other of spec.groups) other.proxies = other.proxies.filter((m) => m !== g.name)
      for (const r of spec.rules) if (r.target === g.name) r.target = 'DIRECT'
    },
  })
}

// ---- 规则 ----
const ruleFilter = ref('')
const typeOptions = RULE_TYPES.map((t) => ({ value: t.value, label: t.value, hint: t.hint }))
const targetOptions = computed(() => [
  { label: '分组', options: spec.groups.map((g) => ({ value: g.name, label: g.name })) },
  { label: '内置', options: BUILTINS.map((n) => ({ value: n, label: n })) },
])
const setOptions = computed(() => ruleSets.value.map((s) => ({ value: s.name, label: s.name })))
const setBehavior = computed(() => Object.fromEntries(ruleSets.value.map((s) => [s.name, s.behavior])))

/** 当前显示的规则及其在方案中的位置；有筛选时不可拖动。 */
const shownRules = computed(() => {
  const q = ruleFilter.value.trim().toLowerCase()
  return spec.rules
    .map((r, i) => ({ r, i }))
    .filter(({ r }) => !q || [r.type, r.value, r.target, r.raw].some((v) => v?.toLowerCase().includes(q)))
})

function addRule() {
  const rule: RouteRule = { type: 'DOMAIN-SUFFIX', value: '', target: spec.groups[0]?.name ?? 'DIRECT' }
  const last = spec.rules.length - 1
  if (last >= 0 && spec.rules[last].type === 'MATCH') spec.rules.splice(last, 0, rule)
  else spec.rules.push(rule)
}

function changeType(r: RouteRule, t: string) {
  const prev = r.type
  if (t === 'RAW' && prev !== 'RAW') {
    r.raw = prev === 'MATCH' ? `MATCH,${r.target}` : [prev, r.value, r.target].join(',') + (r.noResolve ? ',no-resolve' : '')
  }
  r.type = t
  if (t === 'MATCH') r.value = ''
  if (prev === 'RULE-SET' || t === 'RULE-SET') r.value = ''
  if (!NO_RESOLVE.includes(t)) r.noResolve = false
}

const VALUE_HINT: Record<string, string> = {
  DOMAIN: 'www.example.com',
  'DOMAIN-SUFFIX': 'example.com',
  'DOMAIN-KEYWORD': 'example',
  'DOMAIN-REGEX': '^.*\\.example\\.com$',
  GEOSITE: 'cn',
  GEOIP: 'CN',
  'IP-CIDR': '1.2.3.0/24',
  'IP-CIDR6': '2001:db8::/32',
  'IP-ASN': '13335',
  'SRC-IP-CIDR': '192.168.1.0/24',
  'DST-PORT': '443',
  'SRC-PORT': '7777',
  'PROCESS-NAME': 'Telegram',
  'PROCESS-PATH': '/usr/bin/curl',
  NETWORK: 'udp',
}

const showSet = ref(false)

async function copyPreview() {
  if (await copyText(preview.config)) message.success('已复制')
}
</script>

<template>
  <section class="page">
    <a-skeleton v-if="!loaded" active />
    <template v-else>
      <header class="hero">
        <div class="title">
          <button type="button" class="back" @click="router.push({ name: 'routing' })"><ArrowLeft :size="15" />分流</button>
          <p class="eyebrow mono">PROFILE · {{ String(id).padStart(3, '0') }}</p>
          <h1>{{ name || '未命名方案' }}</h1>
        </div>
        <div class="acts">
          <span v-if="dirty" class="dirty mono">UNSAVED</span>
          <a-button type="primary" size="large" class="btn" :loading="saving" :disabled="!dirty" @click="save"><Save :size="17" />保存</a-button>
        </div>
      </header>

      <div class="meta">
        <a-input v-model:value="name" :maxlength="32" placeholder="方案名称" class="meta-name" />
        <a-input v-model:value="note" :maxlength="200" placeholder="备注（可选）" />
      </div>

      <a-alert v-if="issues.length" type="error" show-icon closable class="issues" @close="issues = []">
        <template #message>保存失败，方案有 {{ issues.length }} 处问题</template>
        <template #description>
          <ul class="issue-list">
            <li v-for="(x, i) in issues" :key="i">{{ x }}</li>
          </ul>
        </template>
      </a-alert>

      <a-tabs v-model:activeKey="tab">
        <a-tab-pane key="groups" :tab="`分组 · ${spec.groups.length}`">
          <div class="toolbar">
            <p class="muted hint">
              分组在客户端中按此顺序显示，拖动左侧手柄调整。
              <template v-if="preview.nodes.length">节点匹配数按预览订阅（{{ subs.find((s) => s.id === preview.subId)?.name }}）计算。</template>
            </p>
            <a-button class="btn" @click="openGroup(-1)"><Plus :size="15" />添加分组</a-button>
          </div>
          <ol class="list" @dragover.prevent @drop.prevent="onDrop('groups')">
            <li
              v-for="(g, i) in spec.groups"
              :key="g.name + i"
              class="group"
              :class="{ over: drag.list === 'groups' && drag.over === i, lifted: drag.list === 'groups' && drag.from === i }"
              :draggable="drag.armed === `groups:${i}`"
              @dragstart="onDragStart('groups', i, $event)"
              @dragenter="onDragOver('groups', i)"
              @dragend="endDrag"
            >
              <span class="grip" @mousedown="arm('groups', i)"><GripVertical :size="16" /></span>
              <span class="idx mono">{{ String(i + 1).padStart(2, '0') }}</span>
              <div class="g-main">
                <div class="g-head">
                  <strong class="g-name">{{ g.name }}</strong>
                  <span class="chip mono">{{ GROUP_TYPE_LABEL[g.type] }}</span>
                </div>
                <div class="g-members">
                  <a-tag v-for="m in g.proxies" :key="m" :bordered="false" :class="{ builtin: BUILTINS.includes(m) }">{{ m }}</a-tag>
                  <span v-if="g.nodes" class="filter mono">
                    <template v-if="g.nodes.include">/{{ g.nodes.include }}/</template>
                    <template v-else>全部节点</template>
                    <template v-if="g.nodes.exclude"> 排除 /{{ g.nodes.exclude }}/</template>
                    <template v-if="g.nodes.source !== 'all'"> · {{ g.nodes.source === 'own' ? '自有' : '外部' }}</template>
                    <template v-if="preview.nodes.length">
                      <span v-if="nodeCount(g) === null" class="bad"> · 正则无效</span>
                      <span v-else :class="{ warn: nodeCount(g) === 0 }"> · 匹配 {{ nodeCount(g) }}</span>
                    </template>
                  </span>
                </div>
              </div>
              <span class="refs muted">{{ ruleRefs(g.name) ? `${ruleRefs(g.name)} 条规则` : '' }}</span>
              <div class="row-acts">
                <a-tooltip title="编辑"><button type="button" class="ghost" @click="openGroup(i)"><Pencil :size="15" /></button></a-tooltip>
                <a-tooltip title="删除"><button type="button" class="ghost danger" @click="removeGroup(i)"><Trash2 :size="15" /></button></a-tooltip>
              </div>
            </li>
          </ol>
          <a-empty v-if="!spec.groups.length" description="还没有分组" />
        </a-tab-pane>

        <a-tab-pane key="rules" :tab="`规则 · ${spec.rules.length}`">
          <div class="toolbar">
            <a-input v-model:value="ruleFilter" allow-clear placeholder="筛选规则" class="search" />
            <div class="tool-acts">
              <a-button class="btn" @click="showSet = true"><Plus :size="15" />新建规则集</a-button>
              <a-button type="primary" class="btn" @click="addRule"><Plus :size="15" />添加规则</a-button>
            </div>
          </div>
          <p class="muted hint">自上而下匹配，命中即停止；MATCH 为兜底，必须位于最后。{{ ruleFilter ? '筛选时不可拖动排序。' : '' }}</p>
          <ol class="list rules" @dragover.prevent @drop.prevent="onDrop('rules')">
            <li
              v-for="{ r, i } in shownRules"
              :key="i"
              class="rule"
              :class="{ over: drag.list === 'rules' && drag.over === i, lifted: drag.list === 'rules' && drag.from === i, match: r.type === 'MATCH' }"
              :draggable="drag.armed === `rules:${i}`"
              @dragstart="onDragStart('rules', i, $event)"
              @dragenter="onDragOver('rules', i)"
              @dragend="endDrag"
            >
              <span class="grip" :class="{ off: ruleFilter }" @mousedown="!ruleFilter && arm('rules', i)"><GripVertical :size="16" /></span>
              <span class="idx mono">{{ String(i + 1).padStart(2, '0') }}</span>
              <a-select
                :value="r.type"
                :options="typeOptions"
                show-search
                :dropdown-match-select-width="false"
                class="r-type mono"
                @change="(v: any) => changeType(r, v)"
              >
                <template #option="{ value, hint }">
                  <span class="opt mono">{{ value }}</span><span class="opt-hint">{{ hint }}</span>
                </template>
              </a-select>
              <template v-if="r.type === 'RAW'">
                <a-input v-model:value="r.raw" class="r-raw mono" placeholder="AND,((DOMAIN,example.com),(NETWORK,udp)),REJECT" />
              </template>
              <template v-else>
                <a-select
                  v-if="r.type === 'RULE-SET'"
                  v-model:value="r.value"
                  :options="setOptions"
                  show-search
                  placeholder="选择规则集"
                  class="r-value"
                >
                  <template #option="{ value }">
                    <span class="opt">{{ value }}</span><span class="opt-hint">{{ setBehavior[value] }}</span>
                  </template>
                </a-select>
                <span v-else-if="r.type === 'MATCH'" class="r-value muted match-label">其余所有流量</span>
                <a-input v-else v-model:value="r.value" class="r-value mono" :placeholder="VALUE_HINT[r.type]" />
                <span class="arrow" aria-hidden="true">→</span>
                <a-select v-model:value="r.target" :options="targetOptions" show-search class="r-target" placeholder="目标" />
                <a-tooltip :title="NO_RESOLVE.includes(r.type) ? '匹配时不解析域名为 IP' : ''">
                  <a-checkbox v-model:checked="r.noResolve" :disabled="!NO_RESOLVE.includes(r.type)" class="r-nr mono">no-resolve</a-checkbox>
                </a-tooltip>
              </template>
              <button type="button" class="ghost danger" aria-label="删除规则" @click="spec.rules.splice(i, 1)"><Trash2 :size="15" /></button>
            </li>
          </ol>
          <a-empty v-if="!shownRules.length" :description="ruleFilter ? '没有匹配的规则' : '还没有规则'" />
        </a-tab-pane>

        <a-tab-pane key="base" tab="基础配置">
          <div class="toolbar">
            <a-segmented v-model:value="client" :options="CLIENTS" />
            <a-button v-if="client !== 'mihomo'" class="btn" @click="fillDefault">填入默认模板</a-button>
          </div>
          <template v-if="client === 'mihomo'">
            <p class="muted hint">端口、DNS、TUN、嗅探等设置。proxies、proxy-groups、rule-providers 与 rules 由方案生成，不要写在这里。</p>
            <CodeEditor v-model="spec.mihomo.base" min-height="560px" />
          </template>
          <template v-else-if="client === 'shadowrocket'">
            <p class="muted hint">
              [General] 等段落（DNS、跳过代理、URL 重写、MITM 等）。[Proxy Group] 与 [Rule] 由方案生成；节点不写入配置，由同一订阅的通用链接导入。留空时使用默认模板。
            </p>
            <CodeEditor v-model="spec.shadowrocket.base" lang="text" min-height="560px" />
          </template>
          <template v-else>
            <p class="muted hint">
              JSON 格式（1.12+）。outbounds、route.rule_set 与 route.final 由方案生成；route.rules 中的规则会排在方案规则之前，适合放 sniff、hijack-dns。留空时使用默认模板。
            </p>
            <CodeEditor v-model="spec.singbox.base" lang="json" min-height="560px" />
          </template>
        </a-tab-pane>

        <a-tab-pane key="preview" tab="预览">
          <div class="toolbar">
            <div class="tool-acts">
              <a-select
                v-model:value="preview.subId"
                :options="subs.map((s) => ({ value: s.id, label: s.name }))"
                placeholder="选择订阅"
                class="sub-pick"
                not-found-content="还没有订阅"
                @change="runPreview"
              />
              <a-segmented v-model:value="client" :options="CLIENTS" />
              <a-button class="btn" :loading="preview.loading" @click="runPreview"><RefreshCw :size="15" />刷新</a-button>
            </div>
            <a-button class="btn" :disabled="!preview.config" @click="copyPreview"><Copy :size="15" />复制</a-button>
          </div>
          <p class="muted hint">按所选订阅可用的节点生成，包含尚未保存的修改。</p>
          <a-alert v-if="preview.issues.length" type="error" show-icon class="issues">
            <template #message>
              <ul class="issue-list">
                <li v-for="(x, i) in preview.issues" :key="i">{{ x }}</li>
              </ul>
            </template>
          </a-alert>
          <a-alert v-if="preview.warnings.length" type="warning" show-icon class="issues">
            <template #message>
              <ul class="issue-list">
                <li v-for="(x, i) in preview.warnings" :key="i">{{ x }}</li>
              </ul>
            </template>
          </a-alert>
          <a-empty v-if="!subs.length" description="先创建一个订阅才能预览" />
          <CodeEditor v-else-if="preview.config" :model-value="preview.config" :lang="LANG[client]" readonly min-height="560px" />
        </a-tab-pane>
      </a-tabs>
    </template>

    <GroupDrawer
      v-model:open="showGroup"
      :group="editingGroup >= 0 ? spec.groups[editingGroup] : null"
      :group-names="groupNames"
      :nodes="preview.nodes"
      @save="saveGroup"
    />
    <RuleSetModal v-model:open="showSet" :rule-set="null" @saved="reloadRuleSets" />
  </section>
</template>

<style scoped>
.hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
  flex-wrap: wrap;
}
.title {
  min-width: 0;
}
.back {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 14px;
  padding: 4px 10px 4px 6px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text-2);
  font-size: 12.5px;
  cursor: pointer;
}
.back:hover {
  color: var(--text);
  border-color: var(--border-strong);
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
  font-size: clamp(36px, 5vw, 64px);
  font-weight: 700;
  letter-spacing: -0.05em;
  line-height: 0.98;
  overflow-wrap: anywhere;
}
.acts {
  display: flex;
  align-items: center;
  gap: 12px;
}
.dirty {
  font-size: 11px;
  letter-spacing: 0.16em;
  color: var(--warn);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}
.meta {
  display: grid;
  grid-template-columns: minmax(0, 280px) minmax(0, 1fr);
  gap: 10px;
  margin-bottom: 8px;
}
.issues {
  margin: 10px 0 14px;
}
.issue-list {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
  line-height: 1.7;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.tool-acts {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.hint {
  margin: 0 0 12px;
  font-size: 12.5px;
  line-height: 1.6;
}
.toolbar .hint {
  margin: 0;
}
.search {
  max-width: 280px;
}
.sub-pick {
  width: 220px;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.group,
.rule {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px 10px 6px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  transition:
    border-color 0.15s,
    box-shadow 0.15s,
    opacity 0.15s;
}
.group {
  padding-top: 14px;
  padding-bottom: 14px;
}
.rules {
  gap: 4px;
}
.rule {
  padding: 5px 8px 5px 4px;
  border-radius: 10px;
}
.over {
  border-color: var(--text);
  box-shadow: 0 -3px 0 0 var(--lime) inset;
}
.lifted {
  opacity: 0.45;
}
.grip {
  display: grid;
  place-items: center;
  width: 22px;
  height: 30px;
  color: var(--text-3);
  cursor: grab;
  flex: none;
}
.grip:hover {
  color: var(--text);
}
.grip.off {
  cursor: not-allowed;
  opacity: 0.35;
}
.idx {
  flex: none;
  width: 24px;
  font-size: 11.5px;
  color: var(--text-3);
}
.g-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.g-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.g-name {
  font-family: var(--font-display);
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.chip {
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--ink);
  color: var(--lime);
  font-size: 11px;
}
:root[data-theme='dark'] .chip {
  background: var(--lime);
  color: var(--ink);
}
.g-members {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
}
.g-members :deep(.ant-tag) {
  margin: 0;
}
.g-members :deep(.ant-tag.builtin) {
  font-family: var(--font-mono);
  font-size: 11.5px;
}
.filter {
  font-size: 12px;
  color: var(--text-2);
  padding: 1px 8px;
  border: 1px dashed var(--border-strong);
  border-radius: 6px;
}
.warn {
  color: var(--warn);
}
.bad {
  color: var(--bad);
}
.refs {
  flex: none;
  font-size: 12px;
}
.row-acts {
  display: inline-flex;
  gap: 2px;
  flex: none;
}
.ghost {
  flex: none;
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
.rule.match {
  background: var(--surface-2);
  border-style: dashed;
}
.r-type {
  width: 168px;
  flex: none;
}
.r-value {
  flex: 1;
  min-width: 0;
}
.r-raw {
  flex: 1;
  min-width: 0;
}
.match-label {
  font-size: 13px;
  padding-left: 4px;
}
.arrow {
  flex: none;
  color: var(--text-3);
}
.r-target {
  width: 190px;
  flex: none;
}
.r-nr {
  flex: none;
  font-size: 12px;
}
.opt {
  font-size: 12.5px;
}
.opt-hint {
  margin-left: 10px;
  font-size: 12px;
  color: var(--text-3);
}
@media (max-width: 860px) {
  .meta {
    grid-template-columns: 1fr;
  }
  .rule {
    flex-wrap: wrap;
  }
  .r-type,
  .r-target {
    width: calc(50% - 40px);
  }
  .refs {
    display: none;
  }
}
</style>
