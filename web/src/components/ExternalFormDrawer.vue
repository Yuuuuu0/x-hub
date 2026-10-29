<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { ScanSearch } from 'lucide-vue-next'
import { api, type ExternalItem, type ExternalNode, type InboundItem, type Subscription } from '../lib/api'

// 新建或编辑外部节点来源：手动粘贴或外部订阅，附筛选、前置（链式代理）与可见范围。
const props = defineProps<{ item: ExternalItem | null; inbounds: InboundItem[]; subs: Subscription[]; groupNames: string[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [] }>()

const INTERVALS = [1, 3, 6, 12, 24].map((h) => ({ value: h * 3600, label: `每 ${h} 小时` }))

const form = reactive({
  kind: 'manual' as 'manual' | 'remote',
  name: '',
  content: '',
  userAgent: '',
  interval: 43200,
  prefix: '',
  include: '',
  exclude: '',
  chainKind: '' as '' | 'inbound' | 'group',
  chainInbound: undefined as number | undefined,
  chainGroup: '',
  allSubs: false,
  subIds: [] as number[],
})
const saving = ref(false)
const parsing = ref(false)
const parsed = ref<{ nodes: ExternalNode[]; skipped: string[]; userInfo: string } | null>(null)

watch(open, (v) => {
  if (!v) return
  const s = props.item?.source
  const [ck, cref] = (s?.chain ?? '').split(/:(.*)/s)
  Object.assign(form, {
    kind: s?.kind ?? 'manual',
    name: s?.name ?? '',
    content: s?.content ?? '',
    userAgent: s?.userAgent ?? '',
    interval: s?.interval || 43200,
    prefix: s?.prefix ?? '',
    include: s?.include ?? '',
    exclude: s?.exclude ?? '',
    chainKind: ck ?? '',
    chainInbound: ck === 'inbound' ? Number(cref) : undefined,
    chainGroup: ck === 'group' ? cref : '',
    allSubs: s?.allSubs ?? false,
    subIds: [...(s?.subIds ?? [])],
  })
  parsed.value = props.item ? { nodes: props.item.nodes, skipped: [], userInfo: s?.userInfo ?? '' } : null
})

const inboundOptions = computed(() => {
  const groups = new Map<string, { label: string; options: { value: number; label: string }[] }>()
  for (const i of props.inbounds) {
    if (!groups.has(i.nodeName)) groups.set(i.nodeName, { label: i.nodeName, options: [] })
    groups.get(i.nodeName)!.options.push({ value: i.id, label: `${i.nodeName} · ${i.remark || '未命名'} :${i.port}` })
  }
  return [...groups.values()]
})
const groupOptions = computed(() => props.groupNames.map((n) => ({ value: n })))
const subOptions = computed(() => props.subs.map((s) => ({ value: s.id, label: s.name })))

const kept = computed(() => parsed.value?.nodes.filter((n) => !n.excluded).length ?? 0)

const chain = computed(() => {
  if (form.chainKind === 'inbound' && form.chainInbound) return `inbound:${form.chainInbound}`
  if (form.chainKind === 'group' && form.chainGroup.trim()) return `group:${form.chainGroup.trim()}`
  return ''
})

/** 试解析内容或试拉取订阅，按当前筛选条件标记被排除的节点。 */
async function tryParse() {
  parsing.value = true
  try {
    parsed.value = await api.post('external/parse', {
      kind: form.kind,
      content: form.content,
      userAgent: form.userAgent,
      prefix: form.prefix,
      include: form.include,
      exclude: form.exclude,
    })
  } catch (e: any) {
    message.error(e.message)
  } finally {
    parsing.value = false
  }
}

async function save() {
  if (form.chainKind && !chain.value) return message.error(form.chainKind === 'inbound' ? '请选择前置入站' : '请填写前置分组')
  if (!form.allSubs && !form.subIds.length) return message.error('请选择可以使用这些节点的订阅')
  saving.value = true
  const body = {
    kind: form.kind,
    name: form.name,
    enabled: props.item?.source.enabled ?? true,
    content: form.content,
    userAgent: form.userAgent.trim(),
    interval: form.interval,
    prefix: form.prefix,
    include: form.include,
    exclude: form.exclude,
    chain: chain.value,
    allSubs: form.allSubs,
    subIds: form.subIds,
  }
  try {
    const r = props.item
      ? await api.put<ExternalItem>(`external/${props.item.source.id}`, body)
      : await api.post<ExternalItem>('external', body)
    if (r.source.fetchError) message.warning(`已保存，但订阅拉取失败：${r.source.fetchError}`)
    else message.success(`已保存，共 ${r.nodes.filter((n) => !n.excluded).length} 个节点`)
    open.value = false
    emit('saved')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <a-drawer v-model:open="open" :title="item ? '编辑外部节点' : '添加外部节点'" :width="600">
    <a-form layout="vertical">
      <a-form-item v-if="!item" label="来源">
        <a-radio-group v-model:value="form.kind" button-style="solid" class="split" @change="parsed = null">
          <a-radio-button value="manual">手动添加</a-radio-button>
          <a-radio-button value="remote">外部订阅</a-radio-button>
        </a-radio-group>
      </a-form-item>
      <a-form-item label="名称" required>
        <a-input v-model:value="form.name" :maxlength="64" :placeholder="form.kind === 'manual' ? '如：住宅代理' : '如：某机场'" />
      </a-form-item>

      <template v-if="form.kind === 'manual'">
        <a-form-item label="节点" required extra="每行一条分享链接（vless / vmess / trojan / ss / hysteria2 / tuic / socks5 / http），或粘贴 Mihomo 的节点定义。">
          <a-textarea
            v-model:value="form.content"
            class="mono"
            :auto-size="{ minRows: 5, maxRows: 14 }"
            placeholder="socks5://user:pass@1.2.3.4:1080#住宅-美国"
          />
        </a-form-item>
      </template>
      <template v-else>
        <a-form-item label="订阅地址" required>
          <a-input v-model:value="form.content" class="mono" placeholder="https://example.com/api/v1/client/subscribe?token=..." />
        </a-form-item>
        <div class="row">
          <a-form-item label="User-Agent" extra="多数机场据此返回完整的 Clash 配置。">
            <a-input v-model:value="form.userAgent" class="mono" placeholder="clash.meta" />
          </a-form-item>
          <a-form-item label="自动更新">
            <a-select v-model:value="form.interval" :options="INTERVALS" />
          </a-form-item>
        </div>
      </template>

      <div class="row">
        <a-form-item label="名称包含（正则）">
          <a-input v-model:value="form.include" class="mono" placeholder="留空表示全部" />
        </a-form-item>
        <a-form-item label="名称排除（正则）">
          <a-input v-model:value="form.exclude" class="mono" placeholder="如 剩余|到期|官网" />
        </a-form-item>
      </div>
      <a-form-item label="名称前缀" extra="加在每个节点名称前，便于在分组中按名称筛选。">
        <a-input v-model:value="form.prefix" :maxlength="32" placeholder="如：机场A-" />
      </a-form-item>

      <section class="preview">
        <div class="preview-head">
          <span v-if="!parsed" class="muted">{{ form.kind === 'manual' ? '解析内容，确认节点与筛选结果。' : '试拉取订阅，确认节点与筛选结果。' }}</span>
          <span v-else>共 {{ parsed.nodes.length }} 个节点，保留 <strong>{{ kept }}</strong> 个</span>
          <a-button size="small" class="btn" :loading="parsing" :disabled="!form.content.trim()" @click="tryParse">
            <ScanSearch :size="14" />{{ form.kind === 'manual' ? '解析' : '试拉取' }}
          </a-button>
        </div>
        <div v-if="parsed?.nodes.length" class="tags">
          <a-tag v-for="(n, i) in parsed.nodes" :key="i" :bordered="false" :class="{ excluded: n.excluded }" :title="`${n.type} · ${n.server}`">
            {{ n.name }}
          </a-tag>
        </div>
        <ul v-if="parsed?.skipped.length" class="skipped">
          <li v-for="(s, i) in parsed.skipped" :key="i">已跳过 {{ s }}</li>
        </ul>
      </section>

      <a-form-item label="前置（链式代理）" class="gap">
        <a-radio-group v-model:value="form.chainKind">
          <a-radio value="">不使用</a-radio>
          <a-radio value="inbound">经由自有服务器</a-radio>
          <a-radio value="group">经由分组</a-radio>
        </a-radio-group>
      </a-form-item>
      <a-form-item v-if="form.chainKind === 'inbound'" extra="客户端先连到这个入站，再由它连接外部节点；订阅不包含该入站时这些节点不会输出。">
        <a-select v-model:value="form.chainInbound" :options="inboundOptions" placeholder="选择入站" not-found-content="还没有入站" />
      </a-form-item>
      <a-form-item v-if="form.chainKind === 'group'" extra="经由分流方案中的分组（如「美国节点」）；方案中没有该分组时这些节点不会输出。">
        <a-auto-complete v-model:value="form.chainGroup" :options="groupOptions" placeholder="分组名称" />
      </a-form-item>
      <p v-if="form.chainKind" class="muted note">链式代理只在 Clash 系客户端中生效，通用订阅（Shadowrocket、v2rayN 等）不包含这些节点。</p>

      <a-form-item label="可见范围" extra="外部节点不经过 x-hub 的服务器，没有流量统计与限额。">
        <a-radio-group v-model:value="form.allSubs" button-style="solid">
          <a-radio-button :value="false">指定订阅</a-radio-button>
          <a-radio-button :value="true">全部订阅</a-radio-button>
        </a-radio-group>
      </a-form-item>
      <a-form-item v-if="!form.allSubs">
        <a-select v-model:value="form.subIds" mode="multiple" :options="subOptions" placeholder="选择订阅" />
      </a-form-item>
    </a-form>
    <template #footer>
      <div class="foot">
        <a-button @click="open = false">取消</a-button>
        <a-button type="primary" :loading="saving" @click="save">保存</a-button>
      </div>
    </template>
  </a-drawer>
</template>

<style scoped>
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 14px;
}
.split {
  display: flex;
}
.split :deep(.ant-radio-button-wrapper) {
  flex: 1;
  text-align: center;
}
.preview {
  padding: 14px 16px;
  border: 1px dashed var(--border-strong);
  border-radius: 12px;
  background: var(--surface-2);
  font-size: 13px;
}
.preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 10px;
  max-height: 200px;
  overflow: auto;
}
.tags :deep(.ant-tag) {
  margin: 0;
}
.tags :deep(.ant-tag.excluded) {
  text-decoration: line-through;
  opacity: 0.45;
}
.skipped {
  margin: 10px 0 0;
  padding-left: 18px;
  font-size: 12px;
  color: var(--warn);
}
.gap {
  margin-top: 24px;
}
.note {
  margin: -8px 0 18px;
  font-size: 12.5px;
}
.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
