<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { bytes } from '../lib/format'

// 按天堆叠的流量柱状图：下行在下、上行在上，悬停显示当日明细，可切换为表格。
export interface DayTraffic {
  day: string
  up: number
  down: number
}

const props = defineProps<{ data: DayTraffic[] }>()

const HEIGHT = 200
const PAD = { top: 12, right: 8, bottom: 26, left: 56 }
const GAP = 2 // 堆叠段之间的表面间隙

const root = ref<HTMLElement>()
const width = ref(640)
const hover = ref<number | null>(null)
const showTable = ref(false)
let ro: ResizeObserver | undefined

onMounted(() => {
  ro = new ResizeObserver(([e]) => (width.value = Math.max(280, e.contentRect.width)))
  if (root.value) ro.observe(root.value)
})
onBeforeUnmount(() => ro?.disconnect())

/** 四段刻度，步长取不小于 max/4 的「整」值（0.1/0.25/0.5/1/2/2.5/5 × 10^k × 1024^n）。 */
const scale = computed(() => {
  const max = Math.max(1, ...props.data.map((d) => d.up + d.down))
  const unit = 1024 ** Math.max(0, Math.floor(Math.log(max) / Math.log(1024)))
  const v = max / unit
  const nice = [0.1, 0.2, 0.25, 0.5, 1, 2, 2.5, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000]
  const step = nice.find((s) => s * 4 >= v) ?? 1000
  return { top: step * 4 * unit, ticks: [0, 1, 2, 3, 4].map((i) => i * step * unit) }
})

const plotW = computed(() => width.value - PAD.left - PAD.right)
const plotH = HEIGHT - PAD.top - PAD.bottom
const slot = computed(() => plotW.value / Math.max(1, props.data.length))
const barW = computed(() => Math.max(3, Math.min(22, slot.value * 0.62)))
const y = (v: number) => PAD.top + plotH - (v / scale.value.top) * plotH

/** 顶段带 4px 圆角，底部贴住基线。 */
function topRounded(x: number, y0: number, w: number, h: number) {
  const r = Math.min(4, w / 2, h)
  return `M${x},${y0 + h}V${y0 + r}Q${x},${y0} ${x + r},${y0}H${x + w - r}Q${x + w},${y0} ${x + w},${y0 + r}V${y0 + h}Z`
}

const bars = computed(() =>
  props.data.map((d, i) => {
    const x = PAD.left + i * slot.value + (slot.value - barW.value) / 2
    const downH = (d.down / scale.value.top) * plotH
    const upH = (d.up / scale.value.top) * plotH
    const base = PAD.top + plotH
    const hasUp = upH >= 0.5
    const downTop = base - downH
    return {
      i,
      x,
      d,
      // 有上行段时下行段不需要圆角；两段之间留出间隙
      down: downH < 0.5 ? '' : hasUp ? `M${x},${base}V${downTop}H${x + barW.value}V${base}Z` : topRounded(x, downTop, barW.value, downH),
      up: hasUp ? topRounded(x, downTop - upH - (downH >= 0.5 ? GAP : 0), barW.value, upH) : '',
    }
  }),
)

const labelEvery = computed(() => Math.ceil(props.data.length / Math.max(2, Math.floor(plotW.value / 56))))
const tip = computed(() => {
  if (hover.value === null) return null
  const b = bars.value[hover.value]
  const left = Math.min(Math.max(b.x + barW.value / 2, 90), width.value - 90)
  return { ...b.d, left, total: b.d.up + b.d.down }
})
const total = computed(() => props.data.reduce((s, d) => ({ up: s.up + d.up, down: s.down + d.down }), { up: 0, down: 0 }))
</script>

<template>
  <div class="viz-root">
    <div class="viz-head">
      <div class="legend">
        <span><i class="sw down" />下行 <b class="mono">{{ bytes(total.down) }}</b></span>
        <span><i class="sw up" />上行 <b class="mono">{{ bytes(total.up) }}</b></span>
      </div>
      <a-segmented v-model:value="showTable" size="small" :options="[{ value: false, label: '图表' }, { value: true, label: '表格' }]" />
    </div>

    <div v-show="!showTable" ref="root" class="plot" @mouseleave="hover = null">
      <svg :width="width" :height="HEIGHT" role="img" :aria-label="`最近 ${data.length} 天流量，下行 ${bytes(total.down)}，上行 ${bytes(total.up)}`">
        <g class="grid">
          <template v-for="t in scale.ticks" :key="t">
            <line :x1="PAD.left" :x2="width - PAD.right" :y1="y(t)" :y2="y(t)" :class="{ base: t === 0 }" />
            <text :x="PAD.left - 8" :y="y(t) + 4" text-anchor="end" class="mono">{{ bytes(t) }}</text>
          </template>
        </g>
        <g>
          <template v-for="b in bars" :key="b.d.day">
            <rect
              :x="PAD.left + b.i * slot"
              :y="PAD.top"
              :width="slot"
              :height="plotH"
              :class="['hit', { on: hover === b.i }]"
              @mouseenter="hover = b.i"
            />
            <path v-if="b.down" :d="b.down" class="seg down" :class="{ dim: hover !== null && hover !== b.i }" />
            <path v-if="b.up" :d="b.up" class="seg up" :class="{ dim: hover !== null && hover !== b.i }" />
          </template>
        </g>
        <g class="xaxis">
          <template v-for="b in bars" :key="b.d.day">
            <text v-if="b.i % labelEvery === 0 || b.i === bars.length - 1" :x="b.x + barW / 2" :y="HEIGHT - 8" text-anchor="middle" class="mono">
              {{ b.d.day.slice(5) }}
            </text>
          </template>
        </g>
      </svg>
      <div v-if="tip" class="tip" :style="{ left: `${tip.left}px` }">
        <p class="mono">{{ tip.day }}</p>
        <p><i class="sw down" />下行 <b class="mono">{{ bytes(tip.down) }}</b></p>
        <p><i class="sw up" />上行 <b class="mono">{{ bytes(tip.up) }}</b></p>
        <p class="sum">合计 <b class="mono">{{ bytes(tip.total) }}</b></p>
      </div>
    </div>

    <a-table
      v-if="showTable"
      size="small"
      :pagination="false"
      :scroll="{ y: 240 }"
      row-key="day"
      :data-source="[...data].reverse()"
      :columns="[
        { title: '日期', dataIndex: 'day' },
        { title: '下行', dataIndex: 'down', align: 'right', customRender: ({ value }: any) => bytes(value) },
        { title: '上行', dataIndex: 'up', align: 'right', customRender: ({ value }: any) => bytes(value) },
      ]"
    />
  </div>
</template>

<style scoped>
.viz-root {
  --series-down: #5b8a00;
  --series-up: #2a78d6;
  --grid: var(--border);
}
:root[data-theme='dark'] .viz-root {
  --series-down: #6fa300;
  --series-up: #3987e5;
}
.viz-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  font-size: 12.5px;
  color: var(--text-2);
}
.legend span,
.tip p {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.legend b,
.tip b {
  color: var(--text);
  font-weight: 500;
}
.sw {
  width: 10px;
  height: 10px;
  border-radius: 3px;
}
.sw.down {
  background: var(--series-down);
}
.sw.up {
  background: var(--series-up);
}
.plot {
  position: relative;
  width: 100%;
}
svg {
  display: block;
  overflow: visible;
}
.grid line {
  stroke: var(--grid);
  stroke-dasharray: 2 4;
}
.grid line.base {
  stroke: var(--border-strong);
  stroke-dasharray: none;
}
.grid text,
.xaxis text {
  font-size: 10.5px;
  fill: var(--text-3);
}
.hit {
  fill: transparent;
}
.hit.on {
  fill: var(--surface-2);
}
.seg {
  pointer-events: none; /* 悬停由覆盖整列的命中区处理 */
  transition: opacity 0.15s;
}
.seg.down {
  fill: var(--series-down);
}
.seg.up {
  fill: var(--series-up);
}
.seg.dim {
  opacity: 0.45;
}
.tip {
  position: absolute;
  top: 0;
  transform: translateX(-50%);
  pointer-events: none;
  min-width: 150px;
  padding: 8px 10px;
  border-radius: 10px;
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow);
  font-size: 12px;
  color: var(--text-2);
}
.tip p {
  display: flex;
  margin: 0;
  line-height: 1.7;
}
.tip p b {
  margin-left: auto;
  padding-left: 12px;
}
.tip p:first-child {
  color: var(--text);
}
.tip .sum {
  border-top: 1px dashed var(--border);
  margin-top: 4px;
  padding-top: 4px;
}
</style>
