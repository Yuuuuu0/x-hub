<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Modal, message } from 'ant-design-vue'
import { Copy, FileDown, Pencil, Plus, Trash2 } from 'lucide-vue-next'
import { api, type ProfileItem, type RuleSetItem } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { datetime } from '../lib/format'
import RuleSetModal from '../components/RuleSetModal.vue'

const router = useRouter()
const tab = ref('profiles')
const profiles = ref<ProfileItem[]>([])
const ruleSets = ref<RuleSetItem[]>([])
const loaded = ref(false)

const showCreate = ref(false)
const createName = ref('')
const creating = ref(false)

const showImport = ref(false)
const importForm = reactive({ name: '', yaml: '' })
const importing = ref(false)

const showSet = ref(false)
const editingSet = ref<RuleSetItem | null>(null)

async function load() {
  try {
    const [a, b] = await Promise.all([
      api.get<{ profiles: ProfileItem[] }>('profiles'),
      api.get<{ ruleSets: RuleSetItem[] }>('rulesets'),
    ])
    profiles.value = a.profiles
    ruleSets.value = b.ruleSets
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loaded.value = true
  }
}
onMounted(load)

async function create() {
  creating.value = true
  try {
    const p = await api.post<ProfileItem>('profiles', { name: createName.value })
    showCreate.value = false
    router.push({ name: 'profile', params: { id: p.id } })
  } catch (e: any) {
    message.error(e.message)
  } finally {
    creating.value = false
  }
}

function pickFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  f.text().then((t) => {
    importForm.yaml = t
    if (!importForm.name) importForm.name = f.name.replace(/\.(ya?ml)$/i, '').slice(0, 32)
  })
}

async function doImport() {
  importing.value = true
  try {
    const r = await api.post<{ id: number; notes: string[]; issues: string[] }>('profiles/import', importForm)
    showImport.value = false
    const lines = [...r.notes, ...r.issues.map((i) => '需要修正：' + i)]
    Modal.success({
      title: '导入完成',
      width: 560,
      content: lines.length ? lines.join('\n') : '全部内容已导入。',
      okText: '打开方案',
      onOk: () => router.push({ name: 'profile', params: { id: r.id } }),
    })
  } catch (e: any) {
    message.error(e.message)
  } finally {
    importing.value = false
  }
}

function removeProfile(p: ProfileItem) {
  Modal.confirm({
    title: `删除方案「${p.name}」？`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`profiles/${p.id}`)
      message.success('已删除')
      load()
    },
  })
}

function editSet(rs: RuleSetItem | null) {
  editingSet.value = rs
  showSet.value = true
}

function removeSet(rs: RuleSetItem) {
  Modal.confirm({
    title: `删除规则集「${rs.name}」？`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      await api.del(`rulesets/${rs.id}`)
      message.success('已删除')
      load()
    },
  })
}

async function copyURL(rs: RuleSetItem) {
  if (!rs.hostedUrl) {
    message.warning('请先在「设置 → 访问」中填写公开访问地址')
    return
  }
  if (await copyText(rs.hostedUrl)) message.success('已复制规则集地址')
}

const BEHAVIOR: Record<string, string> = { domain: '域名', ipcidr: 'IP 段', classical: '混合' }
</script>

<template>
  <section class="page">
    <header class="hero">
      <div>
        <p class="eyebrow mono">ROUTING</p>
        <h1>分流</h1>
      </div>
      <div class="acts">
        <a-button v-if="tab === 'profiles'" size="large" class="btn" @click="(importForm.name = ''), (importForm.yaml = ''), (showImport = true)">
          <FileDown :size="17" />导入 Mihomo 配置
        </a-button>
        <a-button v-if="tab === 'profiles'" type="primary" size="large" class="btn" @click="(createName = ''), (showCreate = true)">
          <Plus :size="17" />新建方案
        </a-button>
        <a-button v-else type="primary" size="large" class="btn" @click="editSet(null)"><Plus :size="17" />新建规则集</a-button>
      </div>
    </header>
    <p class="lead muted">分流方案决定 Clash 系、sing-box 与 Shadowrocket 客户端拿到的分组与规则，每个订阅可以选用不同的方案；规则集可在多个方案间复用。</p>

    <a-tabs v-model:activeKey="tab">
      <a-tab-pane key="profiles" tab="方案">
        <a-skeleton v-if="!loaded" active />
        <div v-else class="grid">
          <article class="card builtin">
            <h3>内置精简方案</h3>
            <p class="muted desc">未选择方案的订阅使用它：节点选择、自动选择两个分组，局域网与国内直连。</p>
          </article>
          <article v-for="p in profiles" :key="p.id" class="card">
            <h3>{{ p.name }}</h3>
            <p class="muted desc">{{ p.spec.groups.length }} 个分组 · {{ p.spec.rules.length }} 条规则 · 更新于 {{ datetime(p.updatedAt) }}</p>
            <p class="users">
              <span v-if="!p.users?.length" class="muted">尚未被订阅使用</span>
              <a-tag v-for="u in p.users" :key="u" :bordered="false">{{ u }}</a-tag>
            </p>
            <footer class="card-acts">
              <a-button size="small" type="primary" @click="router.push({ name: 'profile', params: { id: p.id } })"><Pencil :size="13" class="ico" />编辑</a-button>
              <a-button size="small" danger class="del" @click="removeProfile(p)"><Trash2 :size="13" /></a-button>
            </footer>
          </article>
        </div>
      </a-tab-pane>

      <a-tab-pane key="sets" tab="规则集">
        <a-table
          :data-source="ruleSets"
          row-key="id"
          size="middle"
          :pagination="false"
          :loading="!loaded"
          :columns="[
            { title: '名称', key: 'name' },
            { title: '行为', key: 'behavior', width: 90 },
            { title: '来源', key: 'source' },
            { title: '被引用', key: 'profiles', width: 180 },
            { title: '', key: 'acts', width: 120, align: 'right' },
          ]"
          :locale="{ emptyText: '还没有规则集，导入 Mihomo 配置时会自动创建' }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'name'">
              <span class="mono strong">{{ record.name }}</span>
              <span v-if="record.note" class="muted note">{{ record.note }}</span>
            </template>
            <template v-else-if="column.key === 'behavior'">
              <a-tag :bordered="false">{{ BEHAVIOR[record.behavior] }}</a-tag>
            </template>
            <template v-else-if="column.key === 'source'">
              <span v-if="record.source === 'hosted'">托管 · {{ record.rules }} 条</span>
              <span v-else class="mono url" :title="record.url">{{ record.url }}</span>
            </template>
            <template v-else-if="column.key === 'profiles'">
              <span v-if="!record.profiles.length" class="muted">—</span>
              <span v-else>{{ record.profiles.join('、') }}</span>
            </template>
            <template v-else-if="column.key === 'acts'">
              <div class="row-acts">
                <a-tooltip v-if="record.source === 'hosted'" title="复制规则集地址">
                  <button type="button" class="ghost" @click="copyURL(record)"><Copy :size="15" /></button>
                </a-tooltip>
                <a-tooltip title="编辑"><button type="button" class="ghost" @click="editSet(record)"><Pencil :size="15" /></button></a-tooltip>
                <a-tooltip title="删除"><button type="button" class="ghost danger" @click="removeSet(record)"><Trash2 :size="15" /></button></a-tooltip>
              </div>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:open="showCreate" title="新建方案" ok-text="创建" :confirm-loading="creating" @ok="create">
      <p class="muted modal-desc">以默认的「节点选择 / 自动选择」为起点，创建后可在编辑器中调整。</p>
      <a-input v-model:value="createName" :maxlength="32" placeholder="方案名称" @press-enter="create" />
    </a-modal>

    <a-modal v-model:open="showImport" title="导入 Mihomo 配置" ok-text="导入" :confirm-loading="importing" :width="680" @ok="doImport">
      <p class="muted modal-desc">
        分组、规则集与规则会被拆分导入，其余部分（DNS、TUN 等）作为基础配置保留。引用 proxy-providers 的分组会转换为按名称筛选节点；规则集会加入规则集库。
      </p>
      <a-form layout="vertical">
        <a-form-item label="方案名称" required>
          <a-input v-model:value="importForm.name" :maxlength="32" placeholder="如：我的方案" />
        </a-form-item>
        <a-form-item label="配置内容">
          <a-textarea v-model:value="importForm.yaml" class="mono" :auto-size="{ minRows: 10, maxRows: 18 }" placeholder="粘贴 Mihomo 配置 YAML" />
          <label class="file">或选择文件 <input type="file" accept=".yaml,.yml,.txt" @change="pickFile" /></label>
        </a-form-item>
      </a-form>
    </a-modal>

    <RuleSetModal v-model:open="showSet" :rule-set="editingSet" @saved="load" />
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
  margin: 0 0 12px;
}
.acts {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
  gap: 16px;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 20px;
  border-radius: var(--radius);
  border: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--shadow);
}
.card.builtin {
  background: var(--surface-2);
  border-style: dashed;
  box-shadow: none;
}
.card h3 {
  margin: 0;
  font-family: var(--font-display);
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.02em;
}
.desc {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.6;
}
.users {
  margin: 4px 0 0;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  font-size: 12.5px;
}
.card-acts {
  display: flex;
  gap: 6px;
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px dashed var(--border);
}
.card-acts :deep(.ant-btn) {
  display: inline-flex;
  align-items: center;
}
.del {
  margin-left: auto;
}
.ico {
  margin-right: 5px;
}
.strong {
  font-weight: 600;
}
.note {
  margin-left: 8px;
  font-size: 12px;
}
.url {
  display: inline-block;
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
  font-size: 12px;
  color: var(--text-2);
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
.modal-desc {
  margin: 8px 0 14px;
  font-size: 13px;
  line-height: 1.7;
}
.file {
  display: inline-block;
  margin-top: 8px;
  font-size: 12.5px;
  color: var(--text-2);
}
.file input {
  margin-left: 6px;
  font-size: 12px;
}
</style>
