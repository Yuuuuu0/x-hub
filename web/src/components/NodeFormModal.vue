<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type CountryItem, type NodeItem } from '../lib/api'

// 新建或编辑服务器：基本信息、地区与 VPS 套餐（流量配额、计费方式、到期时间）。
const props = defineProps<{ node: NodeItem | null; countries: CountryItem[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [node: NodeItem, created: boolean] }>()

const GB = 1024 ** 3
const DAYS = Array.from({ length: 28 }, (_, i) => ({ value: i + 1, label: `每月 ${i + 1} 日` }))
const COUNT_MODES = [
  { value: 'both', label: '双向合计' },
  { value: 'tx', label: '仅出站' },
  { value: 'max', label: '取较大值' },
]
const form = reactive({
  name: '',
  publicHost: '',
  remark: '',
  country: '',
  region: '',
  netResetDay: 1,
  quotaGB: 0,
  netCountMode: 'both',
  expire: undefined as string | undefined, // Unix 秒字符串（date-picker valueFormat=X）
})
const saving = ref(false)

watch(open, (v) => {
  if (!v) return
  const n = props.node
  Object.assign(form, {
    name: n?.name ?? '',
    publicHost: n?.publicHost ?? '',
    remark: n?.remark ?? '',
    country: n?.country ?? '',
    region: n?.region ?? '',
    netResetDay: n?.netResetDay || 1,
    quotaGB: n ? +(n.netQuotaBytes / GB).toFixed(2) : 0,
    netCountMode: n?.netCountMode || 'both',
    expire: n?.expireAt ? String(n.expireAt) : undefined,
  })
})

const label = (c?: CountryItem) => (c ? `${c.name} ${c.code}` : '')
const countryOptions = computed(() => {
  const detected = props.node?.geo.detected
  const auto = detected ? `自动检测（${label(props.countries.find((c) => c.code === detected)) || detected}）` : '自动检测（接入后识别）'
  return [{ value: '', label: auto }, ...props.countries.map((c) => ({ value: c.code, label: label(c) }))]
})
/** 地区名称输入框的占位：当前选择对应的默认名称。 */
const regionPlaceholder = computed(() => {
  const code = form.country || props.node?.geo.detected
  return props.countries.find((c) => c.code === code)?.name ?? '如：东京'
})

async function save() {
  saving.value = true
  try {
    const created = !props.node
    const body = {
      name: form.name,
      publicHost: form.publicHost,
      remark: form.remark,
      country: form.country,
      region: form.region,
      netResetDay: form.netResetDay,
      netQuotaBytes: Math.round((form.quotaGB || 0) * GB),
      netCountMode: form.netCountMode,
      expireAt: form.expire ? Number(form.expire) : 0,
    }
    const n = created ? await api.post<NodeItem>('nodes', body) : await api.put<NodeItem>(`nodes/${props.node!.id}`, body)
    open.value = false
    emit('saved', n, created)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <a-modal
    v-model:open="open"
    :title="node ? '编辑服务器' : '添加服务器'"
    :confirm-loading="saving"
    :ok-text="node ? '保存' : '创建并生成安装命令'"
    :width="560"
    @ok="save"
  >
    <a-form layout="vertical" :model="form" class="form" @submit.prevent="save">
      <a-form-item label="名称" required>
        <a-input v-model:value="form.name" :maxlength="32" placeholder="如：东京-01" />
      </a-form-item>
      <a-form-item label="公网地址" extra="生成分享链接时使用的域名或 IP。留空则在接入后自动填入连接来源 IP。">
        <a-input v-model:value="form.publicHost" class="mono" placeholder="jp1.example.com" />
      </a-form-item>
      <div class="row">
        <a-form-item label="国家或地区" extra="被控端会按出口 IP 自动识别，也可以手动指定。">
          <a-select v-model:value="form.country" :options="countryOptions" show-search option-filter-prop="label" />
        </a-form-item>
        <a-form-item label="地区名称" extra="用于节点名称，留空使用默认名称。">
          <a-input v-model:value="form.region" :maxlength="16" :placeholder="regionPlaceholder" />
        </a-form-item>
      </div>

      <p class="section mono">VPS 套餐</p>
      <div class="row">
        <a-form-item label="流量周期重置日" extra="与 VPS 商家的计费周期一致。">
          <a-select v-model:value="form.netResetDay" :options="DAYS" />
        </a-form-item>
        <a-form-item label="每周期流量配额（GB）" extra="0 表示不限，节点名称中不显示剩余流量。">
          <a-input-number v-model:value="form.quotaGB" :min="0" :step="100" class="full" />
        </a-form-item>
        <a-form-item label="流量计费方式" extra="以商家的计算方式为准。">
          <a-select v-model:value="form.netCountMode" :options="COUNT_MODES" />
        </a-form-item>
        <a-form-item label="到期时间" extra="留空则节点名称中不显示剩余天数。">
          <a-date-picker v-model:value="form.expire" value-format="X" format="YYYY-MM-DD" class="full" placeholder="未设置" />
        </a-form-item>
      </div>
      <a-form-item label="备注">
        <a-textarea v-model:value="form.remark" :maxlength="200" :auto-size="{ minRows: 2, maxRows: 4 }" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<style scoped>
.form {
  margin-top: 16px;
}
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 14px;
}
.full {
  width: 100%;
}
.section {
  margin: 4px 0 12px;
  padding-top: 14px;
  border-top: 1px dashed var(--border);
  font-size: 11px;
  letter-spacing: 0.16em;
  color: var(--text-3);
}
</style>
