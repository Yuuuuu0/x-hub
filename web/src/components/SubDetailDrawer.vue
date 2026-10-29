<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { RotateCcw } from 'lucide-vue-next'
import { api, type Subscription, type SubTraffic } from '../lib/api'
import { bytes, datetime } from '../lib/format'
import TrafficChart from './TrafficChart.vue'
import UsageMeter from './UsageMeter.vue'

// 订阅详情：当前周期用量、最近流量趋势与各入站明细。
const props = defineProps<{ sub: Subscription | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ changed: [] }>()

const days = ref(30)
const data = ref<SubTraffic | null>(null)
const loading = ref(false)

async function load() {
  if (!props.sub) return
  loading.value = true
  try {
    data.value = await api.get<SubTraffic>(`subscriptions/${props.sub.id}/traffic?days=${days.value}`)
  } catch (e: any) {
    message.error(e.message)
  } finally {
    loading.value = false
  }
}

watch(open, (v) => v && load())
watch(days, load)

const periodText = computed(() => {
  const s = props.sub
  if (!s) return ''
  return s.resetDay ? `每月 ${s.resetDay} 日重置，本周期自 ${datetime(s.periodStartAt)}` : `自 ${datetime(s.periodStartAt)} 起累计`
})

function resetTraffic() {
  const s = props.sub
  if (!s) return
  Modal.confirm({
    title: `清零「${s.name}」的已用流量？`,
    content: '当前周期用量归零并从现在开始新周期；因超额被停用的订阅会立即恢复。历史统计不受影响。',
    okText: '清零',
    cancelText: '取消',
    async onOk() {
      await api.post(`subscriptions/${s.id}/reset-traffic`)
      message.success('已清零')
      emit('changed')
      load()
    },
  })
}
</script>

<template>
  <a-drawer v-model:open="open" :title="`流量 · ${sub?.name ?? ''}`" :width="720">
    <template v-if="sub">
      <section class="sec">
        <UsageMeter :sub="sub" large />
        <p class="muted period">{{ periodText }}</p>
        <div class="facts">
          <div><span>上行</span><b class="mono">{{ bytes(sub.usedUp) }}</b></div>
          <div><span>下行</span><b class="mono">{{ bytes(sub.usedDown) }}</b></div>
          <div><span>到期</span><b class="mono">{{ sub.expireAt ? datetime(sub.expireAt) : '永不' }}</b></div>
          <div><span>在线设备</span><b class="mono">{{ sub.onlineIps.length }}</b></div>
        </div>
        <p v-if="sub.onlineIps.length" class="ips mono">{{ sub.onlineIps.join('  ') }}</p>
        <a-button size="small" @click="resetTraffic"><RotateCcw :size="13" class="ico" />清零已用流量</a-button>
      </section>

      <section class="sec">
        <header class="sec-head">
          <h4>每日流量</h4>
          <a-segmented v-model:value="days" size="small" :options="[{ value: 7, label: '7 天' }, { value: 30, label: '30 天' }, { value: 90, label: '90 天' }]" />
        </header>
        <a-skeleton v-if="loading && !data" active />
        <TrafficChart v-else-if="data" :data="data.daily" />
      </section>

      <section class="sec">
        <h4>本周期各入站</h4>
        <a-table
          size="small"
          :pagination="false"
          :data-source="data?.byInbound ?? []"
          :row-key="(r: any) => `${r.nodeId}-${r.inboundId}`"
          :locale="{ emptyText: '本周期暂无流量' }"
          :columns="[
            { title: '入站', key: 'name' },
            { title: '下行', dataIndex: 'down', align: 'right', customRender: ({ value }: any) => bytes(value) },
            { title: '上行', dataIndex: 'up', align: 'right', customRender: ({ value }: any) => bytes(value) },
            { title: '合计', key: 'total', align: 'right' },
          ]"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'name'">
              {{ record.nodeName || '已删除服务器' }} · {{ record.deleted ? '已删除入站' : record.remark || '未命名' }}
            </template>
            <template v-else-if="column.key === 'total'">
              <span class="mono">{{ bytes(record.up + record.down) }}</span>
            </template>
          </template>
        </a-table>
      </section>
    </template>
  </a-drawer>
</template>

<style scoped>
.sec {
  padding: 18px;
  margin-bottom: 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
}
.sec h4 {
  margin: 0 0 12px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.14em;
  color: var(--text-3);
}
.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.sec-head h4 {
  margin: 0;
}
.period {
  margin: 8px 0 14px;
  font-size: 12.5px;
}
.facts {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}
.facts div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.facts span {
  font-size: 12px;
  color: var(--text-3);
}
.facts b {
  font-weight: 500;
  font-size: 14px;
}
.ips {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--text-2);
  word-break: break-all;
}
.ico {
  margin-right: 5px;
  vertical-align: -2px;
}
@media (max-width: 640px) {
  .facts {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
