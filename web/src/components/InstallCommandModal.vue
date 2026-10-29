<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { Check, CircleCheck, Copy, LoaderCircle, RefreshCw, Terminal } from 'lucide-vue-next'
import { api, type NodeItem } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { datetime } from '../lib/format'

// 为服务器生成一次性接入令牌与安装命令，并实时显示接入进度（node 由父组件随轮询更新）。
const props = defineProps<{ node: NodeItem | null }>()
const open = defineModel<boolean>('open', { required: true })

const command = ref('')
const expireAt = ref(0)
const loading = ref(false)
const error = ref('')
const copied = ref(false)
const baseline = ref(0)

/** 接入进度：只认打开弹窗之后建立的新连接（重装时旧连接会被断开），不依赖浏览器与主控的时钟一致。 */
const progress = computed(() => {
  const rt = props.node?.runtime
  if (!rt?.online || rt.connectedAt === baseline.value) return { done: false, text: '等待在 VPS 上执行安装命令…' }
  if (rt.xray.running) return { done: true, text: `已上线，Xray ${rt.xray.version} 运行中` }
  return { done: false, text: '已接入，正在安装并启动 Xray…' }
})

// 卸载命令复用同一下载方式，只替换脚本参数。
const uninstall = computed(() => command.value.split('| bash -s --')[0] + '| bash -s -- --uninstall')

async function generate() {
  if (!props.node) return
  loading.value = true
  error.value = ''
  copied.value = false
  try {
    const r = await api.post<{ command: string; expireAt: number }>(`nodes/${props.node.id}/install-command`)
    command.value = r.command
    expireAt.value = r.expireAt
  } catch (e: any) {
    command.value = ''
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(open, (v) => {
  if (!v) return
  const rt = props.node?.runtime
  baseline.value = rt?.online ? rt.connectedAt : -1
  generate()
})

async function copy() {
  if (await copyText(command.value)) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1800)
  } else {
    message.error('复制失败，请手动选择复制')
  }
}
</script>

<template>
  <a-modal v-model:open="open" :footer="null" :width="720" centered>
    <div class="head">
      <span class="badge"><Terminal :size="18" /></span>
      <div>
        <h3>接入「{{ node?.name }}」</h3>
        <p class="muted">以 root 身份在目标 VPS 上执行下面的命令，完成后服务器会自动上线并安装 Xray。</p>
      </div>
    </div>

    <a-skeleton v-if="loading" active :paragraph="{ rows: 2 }" :title="false" />
    <a-alert v-else-if="error" type="warning" show-icon :message="error" />
    <template v-else>
      <div class="cmd">
        <pre class="mono">{{ command }}</pre>
        <a-button class="copy" @click="copy">
          <component :is="copied ? Check : Copy" :size="15" />
          {{ copied ? '已复制' : '复制' }}
        </a-button>
      </div>
      <div :class="['progress', { done: progress.done }]" role="status">
        <component :is="progress.done ? CircleCheck : LoaderCircle" :size="16" :class="{ spin: !progress.done }" />
        {{ progress.text }}
      </div>
      <ul class="notes">
        <li>令牌仅能使用一次，<span class="mono">{{ datetime(expireAt) }}</span> 前有效。</li>
        <li>需要 systemd，支持 amd64 / arm64。重复执行即为重装，旧凭据随之失效。</li>
        <li v-if="command.includes('--fp')">命令锁定了主控自签证书的公钥。主控位于 Nginx / Cloudflare 之后时，请先在「设置 → 访问」切换为反向代理模式，再重新生成。</li>
        <li>卸载：<code class="mono">{{ uninstall }}</code></li>
      </ul>
      <a-button type="link" class="regen" @click="generate">
        <RefreshCw :size="14" />
        重新生成
      </a-button>
    </template>
  </a-modal>
</template>

<style scoped>
.head {
  display: flex;
  gap: 14px;
  margin-bottom: 18px;
}
.badge {
  flex: none;
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  background: var(--ink);
  color: var(--lime);
}
h3 {
  margin: 0 0 4px;
  font-size: 18px;
}
.head p {
  margin: 0;
}
.cmd {
  position: relative;
  border-radius: 12px;
  background: var(--ink);
  border: 1px solid #22262f;
  padding: 18px 18px 56px;
}
.cmd pre {
  margin: 0;
  color: #d8f7a0;
  font-size: 12.5px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
}
.copy,
.copy:hover,
.copy:focus {
  background: var(--lime) !important;
  color: var(--ink) !important;
  border-color: transparent !important;
}
.copy {
  position: absolute;
  right: 12px;
  bottom: 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.progress {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  padding: 10px 14px;
  border-radius: 10px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  font-size: 13px;
  color: var(--text-2);
}
.progress.done {
  color: var(--ok);
  border-color: color-mix(in srgb, var(--ok) 40%, transparent);
  background: color-mix(in srgb, var(--ok) 8%, transparent);
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.notes {
  margin: 16px 0 0;
  padding-left: 18px;
  color: var(--text-2);
  font-size: 13px;
  line-height: 1.9;
}
.notes code {
  font-size: 11.5px;
  word-break: break-all;
}
.regen {
  padding-left: 0;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>
