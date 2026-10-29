<script setup lang="ts">
import { ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { Globe } from 'lucide-vue-next'
import { api, session, type AllSettings } from '../lib/api'

// 首次登录时确认公开访问地址：安装命令与订阅链接都依赖它。
const open = defineModel<boolean>('open', { required: true })
const url = ref(location.origin)
const saving = ref(false)

watch(open, (v) => {
  if (v) url.value = location.origin
})

async function save() {
  saving.value = true
  try {
    const s = await api.get<AllSettings>('settings')
    await api.put('settings/access', { ...s.access, publicUrl: url.value.trim() })
    session.publicUrlSet = true
    open.value = false
    message.success('公开访问地址已保存')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <a-modal v-model:open="open" :mask-closable="false" :width="520" :footer="null" centered>
    <div class="head">
      <span class="badge"><Globe :size="18" /></span>
      <div>
        <h3>确认公开访问地址</h3>
        <p class="muted">被控端通过它连接主控，安装命令与订阅链接也会使用它。之后可在「设置 → 访问」修改。</p>
      </div>
    </div>
    <a-input v-model:value="url" size="large" class="mono" placeholder="https://hub.example.com:2096" />
    <p class="muted small">只填协议、域名与端口，不含面板路径。若通过 Cloudflare 或反向代理访问，请填写对外的地址。</p>
    <div class="actions">
      <a-button @click="open = false">稍后</a-button>
      <a-button type="primary" :loading="saving" @click="save">保存</a-button>
    </div>
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
p {
  margin: 0;
}
.small {
  font-size: 12px;
  margin-top: 8px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}
</style>
