<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type NodeItem, type Release } from '../lib/api'

// 批量把在线服务器的 Xray 切换到同一版本；锁定版本的服务器切换到各自锁定的版本。
const props = defineProps<{ nodes: NodeItem[] }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ done: []; refresh: [] }>()

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
const loading = ref(false)
const running = ref(false)
const results = ref<Result[] | null>(null)
let releaseReq = 0
let runReq = 0

const options = computed(() =>
  releases.value.filter((r) => r.allowed).map((r) => ({ value: r.tag, label: r.prerelease ? `${r.tag}（预发布）` : r.tag })),
)
const preview = computed(() =>
  props.nodes
    .filter((n) => n.enrolled)
    .map((n) => {
      const online = n.runtime.online
      const current = (online ? n.runtime.xray.version : '') || n.xrayVersion || '—'
      const target = n.xrayPinnedVersion || selected.value || '—'
      const pinned = !!n.xrayPinnedVersion
      const same = online && target !== '—' && current === target
      return { id: n.id, name: n.name, online, current, target, pinned, same }
    }),
)
const switchable = computed(() => preview.value.filter((r) => r.online && r.target !== '—' && r.current !== r.target))

watch(open, async (v) => {
  if (!v) return
  results.value = null
  selected.value = undefined
  emit('refresh')
  const req = ++releaseReq
  loading.value = true
  try {
    const r = await api.get<{ releases: Release[]; default: string }>('xray/releases')
    if (req !== releaseReq) return
    releases.value = r.releases
    selected.value = r.default || undefined
  } catch (e: any) {
    if (req === releaseReq) message.error(e.message)
  } finally {
    if (req === releaseReq) loading.value = false
  }
})

async function run() {
  const req = ++runReq
  running.value = true
  try {
    const r = await api.post<{ results: Result[] }>('xray/batch-install', { version: selected.value })
    if (req !== runReq) return
    results.value = r.results
    emit('done')
  } catch (e: any) {
    if (req === runReq) message.error(e.message)
  } finally {
    if (req === runReq) running.value = false
  }
}
</script>

<template>
  <a-modal
    v-model:open="open"
    title="批量切换 Xray 版本"
    :width="600"
    :mask-closable="!running"
    :closable="!running"
    :keyboard="!running"
    :footer="null"
  >
    <template v-if="!results">
      <p class="muted intro">安装包由主控中转下载，各服务器并行切换，失败的服务器会自动回滚到原版本。</p>
      <a-select v-model:value="selected" :loading="loading" :disabled="running" :options="options" show-search class="select mono" placeholder="选择目标版本" />
      <a-table
        size="small"
        :pagination="false"
        :data-source="preview"
        row-key="id"
        class="tbl"
        :columns="[
          { title: '服务器', dataIndex: 'name' },
          { title: '当前', key: 'current' },
          { title: '目标', key: 'target' },
        ]"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'current'">
            <span class="mono">{{ record.current }}</span>
          </template>
          <template v-else-if="column.key === 'target'">
            <span class="mono">{{ record.target }}</span>
            <a-tag v-if="record.pinned" :bordered="false" class="tag">已锁定</a-tag>
            <a-tag v-if="!record.online" :bordered="false" class="tag">离线</a-tag>
            <a-tag v-if="record.online && record.same" :bordered="false" class="tag">无需切换</a-tag>
          </template>
        </template>
      </a-table>
      <div class="actions">
        <a-button :disabled="running" @click="open = false">取消</a-button>
        <a-button type="primary" :loading="running" :disabled="loading || !selected || !switchable.length" @click="run">
          {{ running ? '切换中…' : !loading && selected && !switchable.length && preview.some((r) => r.online) ? '无需切换' : '开始切换' }}
        </a-button>
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
          { title: '当前版本', key: 'version' },
          { title: '结果', key: 'status' },
        ]"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'version'">
            <span class="mono">{{ record.version || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'status'">
            <div class="res-status">
              <a-tag :color="STATUS[record.status as Result['status']].color" :bordered="false" class="res-tag">
                {{ STATUS[record.status as Result['status']].text }}
              </a-tag>
              <a-tooltip v-if="record.error" :title="record.error">
                <span class="muted res-err">{{ record.error }}</span>
              </a-tooltip>
            </div>
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
.res-status {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.res-tag {
  flex: none;
  margin-inline-end: 0;
}
.res-err {
  min-width: 0;
  max-width: 240px;
  font-size: 12px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>
