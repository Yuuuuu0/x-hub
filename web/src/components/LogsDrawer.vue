<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { api, type NodeItem } from '../lib/api'

// 查看被控端 Xray 最近输出。
const props = defineProps<{ node: NodeItem | null }>()
const open = defineModel<boolean>('open', { required: true })

const lines = ref<string[]>([])
const loading = ref(false)
const error = ref('')
const box = ref<HTMLElement>()

async function load() {
  if (!props.node) return
  loading.value = true
  error.value = ''
  try {
    const r = await api.get<{ lines: string[] }>(`nodes/${props.node.id}/logs?lines=500`)
    lines.value = r.lines ?? []
    await nextTick()
    box.value?.scrollTo({ top: box.value.scrollHeight })
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(open, (v) => v && load())

function level(line: string) {
  if (/\[(Error|Fatal)\]|failed|panic/i.test(line)) return 'err'
  if (/\[Warning\]/.test(line)) return 'warn'
  return ''
}
</script>

<template>
  <a-drawer v-model:open="open" :title="`Xray 日志 · ${node?.name ?? ''}`" :width="760" class="logs-drawer">
    <template #extra>
      <a-button size="small" :loading="loading" @click="load">
        <template #icon><RefreshCw :size="14" /></template>
      </a-button>
    </template>
    <a-alert v-if="error" type="warning" show-icon :message="error" />
    <div v-else ref="box" class="box mono">
      <div v-if="!lines.length && !loading" class="empty">暂无日志</div>
      <div v-for="(l, i) in lines" :key="i" :class="['ln', level(l)]">{{ l }}</div>
    </div>
  </a-drawer>
</template>

<style scoped>
.box {
  height: calc(100vh - 120px);
  overflow: auto;
  background: var(--ink);
  color: #c9ccd3;
  border-radius: 12px;
  padding: 14px 16px;
  font-size: 12px;
  line-height: 1.7;
}
.ln {
  white-space: pre-wrap;
  word-break: break-all;
}
.ln.warn {
  color: #fbbf24;
}
.ln.err {
  color: #ff6b6b;
}
.empty {
  color: #6b707c;
}
</style>
