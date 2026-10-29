<script setup lang="ts">
import { computed, ref } from 'vue'
import { message } from 'ant-design-vue'
import { RotateCw } from 'lucide-vue-next'
import { api, type NodeItem } from '../lib/api'

// 服务器配置同步状态；失败时可点击重试。
const props = defineProps<{ node: NodeItem }>()
const emit = defineEmits<{ retried: [] }>()
const retrying = ref(false)

const view = computed(() => {
  const n = props.node
  if (!n.enrolled) return null
  if (n.syncState === 'synced') return { tone: 'ok', text: '配置已同步' }
  if (n.syncState === 'failed') return { tone: 'bad', text: '同步失败' }
  return { tone: 'warn', text: n.runtime.online ? '同步中' : '待同步（离线）' }
})

async function retry() {
  retrying.value = true
  try {
    await api.post(`nodes/${props.node.id}/sync`)
    message.info('已重新触发同步')
    emit('retried')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    retrying.value = false
  }
}
</script>

<template>
  <a-tooltip v-if="view" :title="node.syncState === 'failed' ? node.syncError : `配置版本 #${node.appliedRev}`">
    <span :class="['sync', view.tone]">
      <i />{{ view.text }}
      <button v-if="node.syncState === 'failed' && node.runtime.online" type="button" class="retry" :disabled="retrying" aria-label="重试同步" @click.stop="retry">
        <RotateCw :size="12" :class="{ spin: retrying }" />
      </button>
    </span>
  </a-tooltip>
</template>

<style scoped>
.sync {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-2);
  white-space: nowrap;
}
.sync i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--idle);
}
.sync.ok i {
  background: var(--ok);
}
.sync.warn i {
  background: var(--warn);
  animation: blink 1.2s ease-in-out infinite;
}
.sync.bad {
  color: var(--bad);
}
.sync.bad i {
  background: var(--bad);
}
.retry {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.retry:hover {
  background: var(--surface-2);
}
.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes blink {
  50% {
    opacity: 0.3;
  }
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
