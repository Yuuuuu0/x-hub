<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type NodeItem, type Release } from '../lib/api'

// 批量把在线服务器的 Xray 切换到同一版本；锁定版本的服务器切换到各自锁定的版本。
const props = defineProps<{ nodes: NodeItem[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ done: [] }>()

interface Result {
  nodeId: number
  name: string
  version: string
  status: 'done' | 'skipped' | 'failed' | 'offline'
  error?: string
}

const STATUS = { done: { text: '已切换', color: 'green' }, skipped: { text: '无需切换', color: 'default' }, failed: { text: '失败', color: 'red' }, offline: { text: '离线', color: 'default' } }

const releases = ref<Release[]>([])
const selected = ref<string>()
const running = ref(false)
const results = ref<Result[] | null>(null)

const options = computed(() =>
  releases.value.filter((r) => r.allowed).map((r) => ({ value: r.tag, label: r.prerelease ? `${r.tag}（预发布）` : r.tag })),
)
const preview = computed(() =>
  props.nodes.filter((n) => n.enrolled).map((n) => ({
    name: n.name,
    current: n.runtime.xray.version || n.xrayVersion || '—',
    target: n.xrayPinnedVersion || selected.value || '—',
    pinned: !!n.xrayPinnedVersion,
    online: n.runtime.online,
  })),
)

watch(open, async (v) => {
  if (!v) return
  results.value = null
  try {
    const r = await api.get<{ releases: Release[]; default: string }>('xray/releases')
    releases.value = r.releases
    selected.value = r.default || undefined
  } catch (e: any) {
    message.error(e.message)
  }
})

async function run() {
  running.value = true
  try {
    results.value = (await api.post<{ results: Result[] }>('xray/batch-install', { version: selected.value })).results
    emit('done')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    running.value = false
  }
}
</script>

<template>
  <a-modal v-model:open="open" title="批量切换 Xray 版本" :width="600" :mask-closable="!running" :closable="!running" :footer="null">
    <template v-if="!results">
      <p class="muted intro">安装包由主控中转下载，各服务器并行切换，失败的服务器会自动回滚到原版本。</p>
      <a-select v-model:value="selected" :options="options" show-search class="select mono" placeholder="选择目标版本" />
      <a-table
        size="small"
        :pagination="false"
        :data-source="preview"
        row-key="name"
        class="tbl"
        :columns="[
          { title: '服务器', dataIndex: 'name' },
          { title: '当前', dataIndex: 'current' },
          { title: '目标', key: 'target' },
        ]"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'target'">
            <span class="mono">{{ record.target }}</span>
            <a-tag v-if="record.pinned" :bordered="false" class="tag">已锁定</a-tag>
            <a-tag v-if="!record.online" :bordered="false" class="tag">离线</a-tag>
          </template>
        </template>
      </a-table>
      <div class="actions">
        <a-button :disabled="running" @click="open = false">取消</a-button>
        <a-button type="primary" :loading="running" :disabled="!selected" @click="run">{{ running ? '切换中…' : '开始切换' }}</a-button>
      </div>
    </template>
    <template v-else>
      <a-table
        size="small"
        :pagination="false"
        :data-source="results"
        row-key="nodeId"
        :columns="[
          { title: '服务器', dataIndex: 'name' },
          { title: '版本', dataIndex: 'version' },
          { title: '结果', key: 'status' },
        ]"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'status'">
            <a-tooltip :title="record.error">
              <a-tag :color="STATUS[record.status as Result['status']].color" :bordered="false">{{ STATUS[record.status as Result['status']].text }}</a-tag>
            </a-tooltip>
          </template>
        </template>
      </a-table>
      <div class="actions">
        <a-button type="primary" @click="open = false">完成</a-button>
      </div>
    </template>
  </a-modal>
</template>

<style scoped>
.intro {
  margin: 8px 0 14px;
  font-size: 13px;
}
.select {
  width: 100%;
  margin-bottom: 14px;
}
.tag {
  margin-left: 6px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>
