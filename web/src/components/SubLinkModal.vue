<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { Check, Copy } from 'lucide-vue-next'
import QRCode from 'qrcode'
import type { Subscription } from '../lib/api'
import { copyText } from '../lib/clipboard'

// 订阅链接：自动识别客户端，也可显式指定格式。
const props = defineProps<{ sub: Subscription | null }>()
const open = defineModel<boolean>('open', { required: true })

const FORMATS = [
  { value: 'auto', label: '自动识别', hint: '按客户端 User-Agent 返回对应格式，适用于大多数客户端。' },
  { value: 'base64', label: '通用', hint: 'v2rayN / v2rayNG / Shadowrocket / NekoBox 等，Base64 编码的分享链接列表。' },
  { value: 'mihomo', label: 'Mihomo', hint: 'Clash Verge / Mihomo Party / Stash 等，含分组与分流规则。' },
  { value: 'singbox', label: 'sing-box', hint: 'sing-box 官方客户端（SFA / SFI / SFM，1.12 及以上），含 TUN 与分流规则；不包含 XHTTP 节点。' },
  {
    value: 'shadowrocket',
    label: 'Shadowrocket',
    hint: 'Shadowrocket 的分流配置（分组与规则），在 Shadowrocket「配置」页添加；节点仍需在首页用「自动识别」或「通用」链接订阅。',
  },
]
const format = ref('auto')
const qr = ref('')
const copied = ref(false)

const link = computed(() => {
  const u = props.sub?.url ?? ''
  return !u || format.value === 'auto' ? u : `${u}?format=${format.value}`
})
const hint = computed(() => FORMATS.find((f) => f.value === format.value)?.hint)

watch(open, (v) => v && (format.value = 'auto'))
watch(link, async (l) => {
  copied.value = false
  qr.value = l ? await QRCode.toDataURL(l, { margin: 1, width: 280, errorCorrectionLevel: 'M' }) : ''
}, { immediate: true })

async function copy() {
  if (await copyText(link.value)) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1800)
  } else {
    message.error('复制失败，请手动选择复制')
  }
}
</script>

<template>
  <a-modal v-model:open="open" :footer="null" :width="600" centered :title="`订阅链接 · ${sub?.name ?? ''}`">
    <a-alert v-if="!sub?.url" type="warning" show-icon message="请先在「设置 → 访问」中填写公开访问地址" />
    <div v-else class="body">
      <a-segmented v-model:value="format" :options="FORMATS.map(({ value, label }) => ({ value, label }))" block />
      <p class="muted hint">{{ hint }}</p>
      <div class="qr"><img v-if="qr" :src="qr" alt="订阅二维码" /></div>
      <div class="link mono">{{ link }}</div>
      <a-button type="primary" block class="copy" @click="copy">
        <component :is="copied ? Check : Copy" :size="15" />
        {{ copied ? '已复制' : '复制订阅链接' }}
      </a-button>
      <p class="muted foot">链接即凭据，请勿公开分享。泄露后可在订阅卡片中「重置链接」。</p>
    </div>
  </a-modal>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}
.hint,
.foot {
  margin: 0;
  font-size: 12.5px;
}
.qr {
  align-self: center;
  padding: 12px;
  border-radius: 14px;
  background: #fff;
  border: 1px solid var(--border);
  line-height: 0;
}
.qr img {
  width: 220px;
  height: 220px;
}
.link {
  font-size: 12px;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  word-break: break-all;
}
.copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
</style>
