<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle, CalendarX } from 'lucide-vue-next'
import type { Subscription } from '../lib/api'
import { bytesParts, percent } from '../lib/format'

// 订阅当前周期用量。设置了上限时显示进度条；超额 / 到期以图标 + 文字标注，不只依赖颜色。
const props = defineProps<{ sub: Subscription; large?: boolean }>()

const used = computed(() => props.sub.usedUp + props.sub.usedDown)
const usedParts = computed(() => bytesParts(used.value))
const limitParts = computed(() => bytesParts(props.sub.limitBytes))
const pct = computed(() => percent(used.value, props.sub.limitBytes))
const tone = computed(() => (props.sub.state === 'exceeded' || pct.value >= 100 ? 'bad' : pct.value >= 80 ? 'warn' : ''))
</script>

<template>
  <div :class="['meter', { large }]">
    <div class="line">
      <span class="val mono">{{ usedParts[0] }}<small>{{ usedParts[1] }}</small></span>
      <span v-if="sub.limitBytes" class="lim mono">/ {{ limitParts[0] }} {{ limitParts[1] }}</span>
      <span v-else class="lim">不限量</span>
      <span v-if="sub.state === 'exceeded'" class="flag bad"><AlertTriangle :size="13" />已超额</span>
      <span v-else-if="sub.state === 'expired'" class="flag bad"><CalendarX :size="13" />已到期</span>
    </div>
    <div v-if="sub.limitBytes" class="bar"><i :class="tone" :style="{ width: `${pct}%` }" /></div>
  </div>
</template>

<style scoped>
.line {
  display: flex;
  align-items: baseline;
  gap: 6px;
  flex-wrap: wrap;
}
.val {
  font-size: 22px;
  letter-spacing: -0.03em;
  line-height: 1.1;
}
.large .val {
  font-size: 34px;
}
.val small {
  font-size: 12px;
  margin-left: 3px;
  color: var(--text-3);
  letter-spacing: 0;
}
.lim {
  font-size: 12.5px;
  color: var(--text-3);
}
.flag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
  font-size: 12px;
  font-weight: 500;
}
.flag.bad {
  color: var(--bad);
}
.bar {
  height: 6px;
  margin-top: 8px;
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
</style>
