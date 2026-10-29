<script setup lang="ts">
import { computed } from 'vue'
import {
  ArrowDown,
  ArrowUp,
  MoreHorizontal,
  Package,
  Pencil,
  Play,
  RotateCw,
  ScrollText,
  Square,
  Terminal,
  Trash2,
} from 'lucide-vue-next'
import type { NodeItem } from '../lib/api'
import SyncChip from './SyncChip.vue'
import { ago, bytes, bytesParts, duration, percent } from '../lib/format'

const props = defineProps<{ node: NodeItem; masterVersion: string; busy: boolean }>()
const emit = defineEmits<{
  synced: []
  edit: []
  install: []
  remove: []
  logs: []
  versions: []
  xray: [action: 'start' | 'stop' | 'restart']
}>()

const rt = computed(() => props.node.runtime)
const hb = computed(() => rt.value.heartbeat)
const online = computed(() => rt.value.online)
const status = computed(() => (!props.node.enrolled ? 'pending' : online.value ? 'online' : 'offline'))
const xray = computed(() => rt.value.xray)

const sys = computed(() => {
  const h = props.node.host
  return [h.platform && `${h.platform} ${h.platformVersion}`.trim(), h.arch].filter(Boolean).join(' · ')
})

const meters = computed(() => {
  const h = hb.value
  if (!h) return []
  return [
    { label: 'CPU', pct: h.cpu, detail: `${props.node.host.cpuCores} 核 · 负载 ${h.load1.toFixed(2)}` },
    { label: 'MEM', pct: percent(h.memUsed, h.memTotal), detail: `${bytes(h.memUsed)} / ${bytes(h.memTotal)}` },
    { label: 'DISK', pct: percent(h.diskUsed, h.diskTotal), detail: `${bytes(h.diskUsed)} / ${bytes(h.diskTotal)}` },
  ]
})

const period = computed(() => {
  const p = props.node.netPeriod
  const used = p.rx + p.tx
  const quota = props.node.netQuotaBytes
  const pct = quota ? percent(used, quota) : 0
  return { ...p, used, quota, pct, tone: meterTone(pct) }
})

const rx = computed(() => bytesParts(rt.value.rxRate))
const tx = computed(() => bytesParts(rt.value.txRate))

const xrayState = computed(() => {
  const x = xray.value
  if (!online.value) return { text: props.node.xrayVersion ? `Xray ${props.node.xrayVersion}` : 'Xray', tone: 'idle' }
  if (!x.installed) return { text: '安装中…', tone: 'warn' }
  if (x.running) return { text: `Xray ${x.version}`, tone: 'ok' }
  if (x.enabled) return { text: `Xray ${x.version} · 启动失败`, tone: 'bad' }
  return { text: `Xray ${x.version} · 已停止`, tone: 'idle' }
})

const agentOutdated = computed(() => online.value && props.node.agentVersion && props.node.agentVersion !== props.masterVersion)

function meterTone(p: number) {
  return p >= 90 ? 'bad' : p >= 75 ? 'warn' : ''
}

function onMenu({ key }: { key: string | number }) {
  if (key === 'edit') emit('edit')
  else if (key === 'install') emit('install')
  else if (key === 'remove') emit('remove')
}
</script>

<template>
  <article :class="['card', status]">
    <header class="top">
      <span :class="['dot', status]" />
      <div class="title">
        <h3>{{ node.name }}</h3>
        <p class="sub mono">
          {{ node.publicHost || '—' }}<template v-if="sys"> · {{ sys }}</template>
        </p>
      </div>
      <a-dropdown trigger="click" placement="bottomRight">
        <button class="ghost" type="button" aria-label="更多操作"><MoreHorizontal :size="18" /></button>
        <template #overlay>
          <a-menu @click="onMenu">
            <a-menu-item key="edit"><span class="mi"><Pencil :size="14" />编辑</span></a-menu-item>
            <a-menu-item key="install"><span class="mi"><Terminal :size="14" />{{ node.enrolled ? '重装命令' : '安装命令' }}</span></a-menu-item>
            <a-menu-divider />
            <a-menu-item key="remove" danger><span class="mi"><Trash2 :size="14" />删除</span></a-menu-item>
          </a-menu>
        </template>
      </a-dropdown>
    </header>

    <div v-if="status === 'pending'" class="await">
      <p class="await-title">等待接入</p>
      <p class="muted">在目标 VPS 上执行安装命令后，这里会实时显示状态。</p>
      <a-button type="primary" @click="emit('install')">
        <Terminal :size="15" />
        查看安装命令
      </a-button>
    </div>

    <template v-else>
      <div class="rates">
        <div class="rate">
          <span class="rate-label"><ArrowDown :size="13" />下行</span>
          <span class="rate-val mono">{{ online ? rx[0] : '—' }}<small v-if="online">{{ rx[1] }}/s</small></span>
        </div>
        <div class="rate">
          <span class="rate-label"><ArrowUp :size="13" />上行</span>
          <span class="rate-val mono">{{ online ? tx[0] : '—' }}<small v-if="online">{{ tx[1] }}/s</small></span>
        </div>
      </div>

      <div v-if="online && meters.length" class="meters">
        <div v-for="m in meters" :key="m.label" class="meter">
          <span class="m-label mono">{{ m.label }}</span>
          <div class="bar"><i :class="meterTone(m.pct)" :style="{ width: `${m.pct}%` }" /></div>
          <span class="m-pct mono">{{ m.pct.toFixed(0) }}%</span>
          <span class="m-detail mono">{{ m.detail }}</span>
        </div>
      </div>
      <div v-else class="offline-note">
        <p>离线</p>
        <span class="muted">最后在线 {{ ago(node.lastSeenAt) }}</span>
      </div>

      <div class="period">
        <div class="p-line">
          <span class="m-label mono">本期</span>
          <span class="mono p-val">↓{{ bytes(period.rx) }} ↑{{ bytes(period.tx) }}</span>
          <span v-if="period.quota" class="mono muted p-quota">{{ bytes(period.used) }} / {{ bytes(period.quota) }}</span>
        </div>
        <div v-if="period.quota" class="bar"><i :class="period.tone" :style="{ width: `${period.pct}%` }" /></div>
      </div>

      <footer class="foot">
        <div class="xray">
          <a-tooltip :title="xray.lastError" :open="xray.lastError && online ? undefined : false">
            <span :class="['chip', 'mono', xrayState.tone]"><i />{{ xrayState.text }}</span>
          </a-tooltip>
          <div class="tools">
            <a-tooltip title="重启 Xray">
              <button class="ghost" type="button" :disabled="!online || !xray.installed || busy" @click="emit('xray', 'restart')">
                <RotateCw :size="15" :class="{ spin: busy }" />
              </button>
            </a-tooltip>
            <a-tooltip :title="xray.enabled ? '停止 Xray' : '启动 Xray'">
              <button class="ghost" type="button" :disabled="!online || !xray.installed || busy" @click="emit('xray', xray.enabled ? 'stop' : 'start')">
                <component :is="xray.enabled ? Square : Play" :size="15" />
              </button>
            </a-tooltip>
            <a-tooltip title="版本">
              <button class="ghost" type="button" :disabled="!online || busy" @click="emit('versions')">
                <Package :size="15" />
              </button>
            </a-tooltip>
            <a-tooltip title="日志">
              <button class="ghost" type="button" :disabled="!online" @click="emit('logs')">
                <ScrollText :size="15" />
              </button>
            </a-tooltip>
          </div>
        </div>
        <p class="meta mono">
          <SyncChip :node="node" class="sync" @retried="emit('synced')" />
          <span v-if="online && hb">已运行 {{ duration(hb.uptime) }}</span>
          <span v-if="online && hb">累计 ↓{{ bytes(hb.netRx) }} ↑{{ bytes(hb.netTx) }}</span>
          <a-tooltip v-if="agentOutdated" :title="`主控版本为 ${masterVersion}，可通过重装命令更新被控端`">
            <span class="outdated">Agent {{ node.agentVersion }}</span>
          </a-tooltip>
        </p>
      </footer>
    </template>
  </article>
</template>

<style scoped>
.card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 20px 20px 16px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow);
  transition:
    transform 0.25s cubic-bezier(0.2, 0.8, 0.2, 1),
    border-color 0.2s;
}

.card:hover {
  transform: translateY(-2px);
  border-color: var(--border-strong);
}

.card.offline {
  background: var(--surface-2);
}

.top {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.title {
  flex: 1;
  min-width: 0;
}

h3 {
  margin: 0;
  font-family: var(--font-display);
  font-size: 19px;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.2;
}

.sub {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dot {
  position: relative;
  flex: none;
  width: 10px;
  height: 10px;
  margin-top: 7px;
  border-radius: 50%;
  background: var(--idle);
}

.dot.online {
  background: var(--ok);
}

.dot.online::after {
  content: '';
  position: absolute;
  inset: -4px;
  border-radius: 50%;
  border: 2px solid var(--ok);
  animation: pulse 2s ease-out infinite;
}

.dot.pending {
  background: var(--warn);
}

.ghost {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--text-2);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.ghost:hover:not(:disabled) {
  background: var(--surface-2);
  color: var(--text);
}

.ghost:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.mi {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.rates {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.rate {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.rate-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-3);
}

.rate-val {
  font-size: 30px;
  font-weight: 500;
  letter-spacing: -0.04em;
  line-height: 1.1;
}

.rate-val small {
  font-size: 13px;
  margin-left: 4px;
  letter-spacing: 0;
  color: var(--text-3);
}

.meters {
  display: flex;
  flex-direction: column;
  gap: 9px;
}

.meter {
  display: grid;
  grid-template-columns: 40px 1fr 40px;
  grid-template-rows: auto auto;
  align-items: center;
  column-gap: 10px;
}

.m-label {
  font-size: 11px;
  color: var(--text-3);
  letter-spacing: 0.06em;
}

.bar {
  height: 6px;
  border-radius: 99px;
  background: var(--bar-track);
  overflow: hidden;
}

.bar i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--text);
  transition: width 0.8s cubic-bezier(0.2, 0.8, 0.2, 1);
}

:root[data-theme='dark'] .bar i {
  background: var(--lime);
}

.bar i.warn {
  background: var(--warn);
}

.bar i.bad {
  background: var(--bad);
}

.m-pct {
  font-size: 12px;
  text-align: right;
}

.m-detail {
  grid-column: 2 / 4;
  font-size: 11px;
  color: var(--text-3);
  margin-top: 2px;
}

.period {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.p-line {
  display: flex;
  align-items: baseline;
  gap: 10px;
  font-size: 12px;
}

.p-quota {
  margin-left: auto;
  font-size: 11px;
}

.offline-note {
  padding: 18px 0 8px;
}

.offline-note p {
  margin: 0;
  font-family: var(--font-display);
  font-size: 28px;
  font-weight: 600;
  letter-spacing: -0.03em;
  color: var(--text-3);
}

.await {
  padding: 8px 0 4px;
}

.await-title {
  margin: 0 0 4px;
  font-family: var(--font-display);
  font-size: 28px;
  font-weight: 600;
  letter-spacing: -0.03em;
}

.await .muted {
  margin: 0 0 16px;
  font-size: 13px;
}

.await :deep(.ant-btn) {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.foot {
  margin-top: auto;
  padding-top: 14px;
  border-top: 1px dashed var(--border);
}

.xray {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 4px 10px;
  border-radius: 99px;
  font-size: 12px;
  border: 1px solid var(--border);
  background: var(--surface-2);
  white-space: nowrap;
}

.chip i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--idle);
}

.chip.ok i {
  background: var(--ok);
}

.chip.warn i {
  background: var(--warn);
}

.chip.bad {
  color: var(--bad);
  border-color: color-mix(in srgb, var(--bad) 35%, transparent);
}

.chip.bad i {
  background: var(--bad);
}

.tools {
  display: flex;
  gap: 2px;
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  margin: 10px 0 0;
  font-size: 11px;
  color: var(--text-3);
}

.meta .sync {
  font-family: var(--font-body);
  font-size: 11px;
}

.outdated {
  color: var(--warn);
  cursor: help;
}

.spin {
  animation: spin 0.9s linear infinite;
}

@keyframes pulse {
  from {
    opacity: 0.7;
    transform: scale(0.6);
  }
  to {
    opacity: 0;
    transform: scale(1.6);
  }
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
