<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type NodeItem, type Release } from '../lib/api'

// 为服务器安装或切换 Xray 版本，超过版本上限的版本不可选。
const props = defineProps<{ node: NodeItem | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ done: [] }>()

const releases = ref<Release[]>([])
const selected = ref<string>()
const pin = ref(false)
const loading = ref(false)
const installing = ref(false)
const error = ref('')

const current = computed(() => props.node?.runtime.xray.version || props.node?.xrayVersion || '')

const options = computed(() =>
  releases.value.map((r) => ({
    value: r.tag,
    disabled: !r.allowed,
    label: r.tag,
    prerelease: r.prerelease,
    allowed: r.allowed,
    date: r.publishedAt.slice(0, 10),
  })),
)

watch(open, async (v) => {
  if (!v) return
  loading.value = true
  error.value = ''
  pin.value = !!props.node?.xrayPinnedVersion
  try {
    const r = await api.get<{ releases: Release[]; default: string }>('xray/releases')
    releases.value = r.releases
    selected.value = props.node?.xrayPinnedVersion || r.default || undefined
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
})

async function install() {
  if (!props.node || !selected.value) return
  installing.value = true
  try {
    if (selected.value !== current.value) {
      await api.post(`nodes/${props.node.id}/xray`, { action: 'install', version: selected.value })
    }
    const wantPin = pin.value ? selected.value : ''
    if (wantPin !== (props.node.xrayPinnedVersion || '')) {
      await api.put(`nodes/${props.node.id}/xray-pin`, { version: wantPin })
    }
    message.success(pin.value ? `已切换并锁定 Xray ${selected.value}` : `已切换到 Xray ${selected.value}`)
    open.value = false
    emit('done')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    installing.value = false
  }
}
</script>

<template>
  <a-modal
    v-model:open="open"
    title="安装 / 切换 Xray 版本"
    :width="480"
    :ok-text="installing ? '安装中…' : '安装'"
    :confirm-loading="installing"
    :ok-button-props="{ disabled: !selected || (selected === current && pin === !!node?.xrayPinnedVersion && (!pin || selected === node?.xrayPinnedVersion)) }"
    :mask-closable="!installing"
    :closable="!installing"
    @ok="install"
  >
    <p class="muted intro">
      当前版本：<span class="mono">{{ current || '未安装' }}</span>。安装包由主控中转下载并校验，新版本启动失败会自动回滚。
    </p>
    <a-alert v-if="error" type="error" show-icon :message="error" />
    <a-select v-else v-model:value="selected" :loading="loading" :options="options" show-search class="select" placeholder="选择版本">
      <template #option="{ label, prerelease, allowed, date }">
        <div class="opt">
          <span class="mono">{{ label }}</span>
          <a-tag v-if="label === current" color="green" :bordered="false">当前</a-tag>
          <a-tag v-if="prerelease" :bordered="false">预发布</a-tag>
          <a-tag v-if="!allowed" color="red" :bordered="false">超过上限</a-tag>
          <span class="date mono muted">{{ date }}</span>
        </div>
      </template>
    </a-select>
    <a-checkbox v-if="!error" v-model:checked="pin" class="pin">锁定为此版本（批量升级时保持该版本，新装 Xray 也使用它）</a-checkbox>
  </a-modal>
</template>

<style scoped>
.intro {
  margin: 12px 0 16px;
}
.select {
  width: 100%;
}
.pin {
  margin-top: 14px;
}
.opt {
  display: flex;
  align-items: center;
  gap: 6px;
}
.date {
  margin-left: auto;
  font-size: 12px;
}
</style>
