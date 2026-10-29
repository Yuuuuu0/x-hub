<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type NodeItem } from '../lib/api'

// 新建或编辑服务器。
const props = defineProps<{ node: NodeItem | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [node: NodeItem, created: boolean] }>()

const GB = 1024 ** 3
const DAYS = Array.from({ length: 28 }, (_, i) => ({ value: i + 1, label: `每月 ${i + 1} 日` }))
const form = reactive({ name: '', publicHost: '', remark: '', netResetDay: 1, quotaGB: 0 })
const saving = ref(false)

watch(open, (v) => {
  if (!v) return
  form.name = props.node?.name ?? ''
  form.publicHost = props.node?.publicHost ?? ''
  form.remark = props.node?.remark ?? ''
  form.netResetDay = props.node?.netResetDay || 1
  form.quotaGB = props.node ? +(props.node.netQuotaBytes / GB).toFixed(2) : 0
})

async function save() {
  saving.value = true
  try {
    const created = !props.node
    const body = { name: form.name, publicHost: form.publicHost, remark: form.remark, netResetDay: form.netResetDay, netQuotaBytes: Math.round((form.quotaGB || 0) * GB) }
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
    :width="480"
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
        <a-form-item label="流量周期重置日" extra="与 VPS 商家的计费周期一致。">
          <a-select v-model:value="form.netResetDay" :options="DAYS" />
        </a-form-item>
        <a-form-item label="每周期流量配额（GB）" extra="按收发合计统计；0 表示不显示配额。">
          <a-input-number v-model:value="form.quotaGB" :min="0" :step="100" class="full" />
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
</style>
