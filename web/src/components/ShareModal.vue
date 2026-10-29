<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { Check, Copy } from 'lucide-vue-next'
import QRCode from 'qrcode'
import { api, type InboundItem, type ShareLink } from '../lib/api'
import { copyText } from '../lib/clipboard'

// 单个入站的分享链接与二维码，按订阅区分凭据。
const props = defineProps<{ inbound: InboundItem | null }>()
const open = defineModel<boolean>('open', { required: true })

const links = ref<ShareLink[]>([])
const selected = ref<number>()
const qr = ref('')
const loading = ref(false)
const error = ref('')
const copied = ref(false)

const current = computed(() => links.value.find((l) => l.subscriptionId === selected.value))

watch(open, async (v) => {
  if (!v || !props.inbound) return
  loading.value = true
  error.value = ''
  links.value = []
  try {
    const r = await api.get<{ links: ShareLink[] }>(`inbounds/${props.inbound.id}/links`)
    links.value = r.links
    selected.value = r.links[0]?.subscriptionId
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
})

watch(current, async (l) => {
  copied.value = false
  qr.value = l ? await QRCode.toDataURL(l.link, { margin: 1, width: 280, errorCorrectionLevel: 'M' }) : ''
})

async function copy() {
  if (!current.value) return
  if (await copyText(current.value.link)) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1800)
  } else {
    message.error('复制失败，请手动选择复制')
  }
}
</script>

<template>
  <a-modal v-model:open="open" :footer="null" :width="560" centered :title="`分享 · ${inbound?.nodeName ?? ''} ${inbound?.remark ?? ''}`">
    <a-skeleton v-if="loading" active />
    <a-alert v-else-if="error" type="warning" show-icon :message="error" />
    <a-empty v-else-if="!links.length" description="没有可用的订阅，请先在「订阅」中创建或启用订阅" />
    <div v-else class="body">
      <a-alert v-if="!inbound?.enabled" type="info" show-icon message="该入站已停用，链接暂时无法连接" class="tip" />
      <a-segmented v-if="links.length > 1" v-model:value="selected" :options="links.map((l) => ({ value: l.subscriptionId, label: l.subscriptionName }))" block />
      <p v-else class="muted who">订阅：{{ links[0].subscriptionName }}</p>
      <div class="qr"><img v-if="qr" :src="qr" alt="分享二维码" /></div>
      <div class="link mono">{{ current?.link }}</div>
      <a-button type="primary" block class="copy" @click="copy">
        <component :is="copied ? Check : Copy" :size="15" />
        {{ copied ? '已复制' : '复制链接' }}
      </a-button>
    </div>
  </a-modal>
</template>

<style scoped>
.body {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-top: 12px;
}
.tip {
  margin: 0;
}
.who {
  margin: 0;
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
  width: 240px;
  height: 240px;
}
.link {
  font-size: 11.5px;
  line-height: 1.6;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  word-break: break-all;
  max-height: 120px;
  overflow: auto;
}
.copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
</style>
