<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import type { RouteGroup } from '../lib/api'
import { BUILTINS, GROUP_TYPES, matchNodes } from '../lib/routing'

// 编辑分组：类型、固定成员与节点筛选。保存时把结果交还给方案编辑器，由其处理改名引用。
const props = defineProps<{ group: RouteGroup | null; groupNames: string[]; nodeNames: string[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ save: [g: RouteGroup] }>()

const form = reactive({
  name: '',
  type: 'select' as RouteGroup['type'],
  proxies: [] as string[],
  withNodes: false,
  include: '',
  exclude: '',
  source: 'all' as 'all' | 'own' | 'external',
  url: '',
  interval: 300,
  tolerance: 50,
  strategy: 'consistent-hashing',
})
const original = ref('')

watch(open, (v) => {
  if (!v) return
  const g = props.group
  original.value = g?.name ?? ''
  Object.assign(form, {
    name: g?.name ?? '',
    type: g?.type ?? 'select',
    proxies: [...(g?.proxies ?? [])],
    withNodes: g ? !!g.nodes : true,
    include: g?.nodes?.include ?? '',
    exclude: g?.nodes?.exclude ?? '',
    source: g?.nodes?.source ?? 'all',
    url: g?.url || 'https://www.gstatic.com/generate_204',
    interval: g?.interval || 300,
    tolerance: g?.tolerance ?? 50,
    strategy: g?.strategy || 'consistent-hashing',
  })
})

const memberOptions = computed(() => [
  { label: '分组', options: props.groupNames.filter((n) => n !== original.value).map((n) => ({ value: n, label: n })) },
  { label: '内置', options: BUILTINS.map((n) => ({ value: n, label: n })) },
])

/** 按当前筛选条件匹配预览订阅中的节点；正则无效时为 null。 */
const matched = computed(() =>
  form.withNodes ? matchNodes({ include: form.include, exclude: form.exclude, source: form.source }, props.nodeNames) : [],
)

function save() {
  const name = form.name.trim()
  if (!name) return message.error('请填写分组名称')
  if (BUILTINS.includes(name)) return message.error('不能使用内置出站名作为分组名称')
  if (name !== original.value && props.groupNames.includes(name)) return message.error('已有同名分组')
  if (!form.proxies.length && !form.withNodes) return message.error('分组至少需要一个成员或包含节点')
  const g: RouteGroup = { name, type: form.type, proxies: form.proxies }
  if (form.withNodes) g.nodes = { include: form.include.trim(), exclude: form.exclude.trim(), source: form.source }
  if (form.type !== 'select') {
    g.url = form.url.trim()
    g.interval = form.interval
    if (form.type === 'url-test') g.tolerance = form.tolerance
    if (form.type === 'load-balance') g.strategy = form.strategy
  }
  emit('save', g)
  open.value = false
}
</script>

<template>
  <a-drawer v-model:open="open" :title="group ? '编辑分组' : '新建分组'" :width="520" class="group-drawer">
    <a-form layout="vertical">
      <a-form-item label="名称" required>
        <a-input v-model:value="form.name" :maxlength="64" placeholder="如：美国节点" />
      </a-form-item>
      <a-form-item label="类型">
        <a-radio-group v-model:value="form.type" button-style="solid" class="types">
          <a-radio-button v-for="t in GROUP_TYPES" :key="t.value" :value="t.value">{{ t.label }}</a-radio-button>
        </a-radio-group>
      </a-form-item>
      <template v-if="form.type !== 'select'">
        <a-form-item label="测速地址">
          <a-input v-model:value="form.url" class="mono" />
        </a-form-item>
        <div class="row">
          <a-form-item label="测速间隔（秒）">
            <a-input-number v-model:value="form.interval" :min="30" :max="86400" class="full" />
          </a-form-item>
          <a-form-item v-if="form.type === 'url-test'" label="容差（毫秒）" extra="延迟差小于容差时不切换。">
            <a-input-number v-model:value="form.tolerance" :min="0" :max="5000" class="full" />
          </a-form-item>
          <a-form-item v-if="form.type === 'load-balance'" label="策略">
            <a-select
              v-model:value="form.strategy"
              :options="[
                { value: 'consistent-hashing', label: '一致性哈希' },
                { value: 'round-robin', label: '轮询' },
                { value: 'sticky-sessions', label: '粘性会话' },
              ]"
            />
          </a-form-item>
        </div>
      </template>

      <a-form-item label="固定成员" extra="其他分组或内置出站，按选择顺序排在节点之前。">
        <a-select v-model:value="form.proxies" mode="multiple" :options="memberOptions" placeholder="不添加固定成员" />
      </a-form-item>

      <section class="nodes">
        <div class="nodes-head">
          <div>
            <strong>包含节点</strong>
            <p class="muted">按名称筛选订阅中的节点，节点增减后自动生效。</p>
          </div>
          <a-switch v-model:checked="form.withNodes" />
        </div>
        <template v-if="form.withNodes">
          <a-form-item label="节点来源">
            <a-radio-group v-model:value="form.source">
              <a-radio value="all">全部</a-radio>
              <a-radio value="own">自有服务器</a-radio>
              <a-radio value="external">外部节点</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-form-item label="名称包含（正则）" extra="留空表示全部；忽略大小写可在开头加 (?i)。">
            <a-input v-model:value="form.include" class="mono" placeholder="如 美国|US|🇺🇸" />
          </a-form-item>
          <a-form-item label="名称排除（正则）">
            <a-input v-model:value="form.exclude" class="mono" placeholder="如 家宽|到期|剩余" />
          </a-form-item>
          <div class="match">
            <template v-if="matched === null"><span class="bad">正则无效</span></template>
            <template v-else-if="!nodeNames.length"><span class="muted">在「预览」中选择一个订阅后，这里会显示匹配到的节点。</span></template>
            <template v-else>
              <span class="muted">在预览订阅的 {{ nodeNames.length }} 个节点中匹配到 {{ matched.length }} 个</span>
              <div class="tags">
                <a-tag v-for="n in matched" :key="n" :bordered="false">{{ n }}</a-tag>
              </div>
            </template>
          </div>
        </template>
      </section>
    </a-form>
    <template #footer>
      <div class="foot">
        <a-button @click="open = false">取消</a-button>
        <a-button type="primary" @click="save">确定</a-button>
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
.full {
  width: 100%;
}
.types {
  display: flex;
}
.types :deep(.ant-radio-button-wrapper) {
  flex: 1;
  text-align: center;
}
.nodes {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface-2);
}
.nodes-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
.nodes-head p {
  margin: 4px 0 0;
  font-size: 12.5px;
}
.match {
  font-size: 12.5px;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 8px;
  max-height: 180px;
  overflow: auto;
}
.bad {
  color: var(--bad);
}
.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
