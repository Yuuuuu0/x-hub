<script setup lang="ts">
import { Plus, X } from 'lucide-vue-next'

/** 一个对外地址；port 仅入站连接地址使用，0 表示使用入站端口。 */
export interface AddrRow {
  host: string
  port?: number
  label: string
}

// 对外地址列表的编辑：每行一个地址与标签，入站模式下另有端口列。标签用于区分同一入站生成的多个节点。
const props = withDefaults(
  defineProps<{ withPort?: boolean; hostPlaceholder?: string; max?: number }>(),
  { withPort: false, hostPlaceholder: '域名或 IP', max: 8 },
)
const rows = defineModel<AddrRow[]>({ required: true })

function add() {
  rows.value = [...rows.value, props.withPort ? { host: '', port: 0, label: '' } : { host: '', label: '' }]
}

function remove(i: number) {
  rows.value = rows.value.filter((_, j) => j !== i)
}
</script>

<template>
  <div class="addrs">
    <div v-for="(row, i) in rows" :key="i" class="line" :class="{ port: withPort }">
      <a-input v-model:value="row.host" class="mono" :placeholder="hostPlaceholder" />
      <a-input-number
        v-if="withPort"
        :value="row.port || null"
        :min="1"
        :max="65535"
        class="mono"
        placeholder="入站端口"
        @update:value="(v: number | null) => (row.port = v ?? 0)"
      />
      <a-input v-model:value="row.label" :maxlength="16" placeholder="标签，如 IPv6" />
      <a-button type="text" class="del" aria-label="删除" @click="remove(i)"><X :size="15" /></a-button>
    </div>
    <a-button v-if="rows.length < max" type="dashed" block class="add" @click="add"><Plus :size="15" />添加地址</a-button>
  </div>
</template>

<style scoped>
.addrs {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.line {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr) 32px;
  gap: 8px;
  align-items: center;
}
.line.port {
  grid-template-columns: minmax(0, 1.6fr) 96px minmax(0, 1fr) 32px;
}
.line :deep(.ant-input-number) {
  width: 100%;
}
.del {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  color: var(--text-3);
}
.del svg {
  flex-shrink: 0;
}
.add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
@media (max-width: 520px) {
  .line.port {
    grid-template-columns: minmax(0, 1fr) 80px 32px;
  }
  .line.port > :nth-child(3) {
    grid-column: 1 / 3;
  }
}
</style>
