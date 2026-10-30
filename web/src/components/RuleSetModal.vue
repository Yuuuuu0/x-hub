<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { api, type RuleSetCheck, type RuleSetItem } from '../lib/api'

// 新建或编辑规则集：托管（在面板编辑内容）或外部链接。
const props = defineProps<{ ruleSet: RuleSetItem | null }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [] }>()

const HINT: Record<string, string> = {
  domain: '每行一个域名：example.com 精确匹配，+.example.com 匹配域名及其子域名。',
  ipcidr: '每行一个 IP 段，如 1.2.3.0/24、2001:db8::/32。',
  classical: '每行一条规则，如 DOMAIN-SUFFIX,example.com、IP-CIDR,1.2.3.0/24。',
}

const form = reactive({ name: '', behavior: 'domain', source: 'hosted', url: '', format: '', interval: 86400, content: '', note: '' })
const saving = ref(false)
// 格式检测的提示；confirmed 记录用户看过提示的链接与格式，再次保存时不再拦截。
const notice = reactive({ warning: '', suggest: '' })
let confirmed = ''

watch(() => [form.source, form.url, form.format], () => Object.assign(notice, { warning: '', suggest: '' }))

watch(open, (v) => {
  if (!v) return
  confirmed = ''
  const r = props.ruleSet
  Object.assign(form, r
    ? { name: r.name, behavior: r.behavior, source: r.source, url: r.url, format: r.format, interval: r.interval, content: r.content, note: r.note }
    : { name: '', behavior: 'domain', source: 'hosted', url: '', format: '', interval: 86400, content: '', note: '' })
})

// check 检测外部链接的内容并记下提示；托管规则集或用户已确认过时不检测，返回 null。
async function check() {
  const key = `${form.url}|${form.format}`
  if (form.source !== 'url' || confirmed === key) return null
  const r = await api.post<RuleSetCheck>('rulesets/check', { url: form.url, format: form.format })
  if (r.warning) {
    Object.assign(notice, { warning: r.warning, suggest: r.suggest ?? '' })
    confirmed = key
  }
  return r
}

async function save() {
  saving.value = true
  try {
    const r = await check()
    if (r?.warning) return
    if (props.ruleSet) await api.put(`rulesets/${props.ruleSet.id}`, form)
    else await api.post('rulesets', form)
    if (r?.fetchError) message.warning(`已保存，但未能检测规则集：${r.fetchError}`)
    else if (props.ruleSet && props.ruleSet.name !== form.name) message.success('已保存，引用它的方案已同步改名')
    else message.success(r?.rules ? `已保存，共 ${r.rules} 条规则` : '已保存')
    open.value = false
    emit('saved')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}

// applySuggest 改用检测到的格式并保存。
function applySuggest() {
  form.format = notice.suggest === 'yaml' ? '' : notice.suggest
  save()
}
</script>

<template>
  <a-modal v-model:open="open" :title="ruleSet ? '编辑规则集' : '新建规则集'" ok-text="保存" :confirm-loading="saving" :width="640" @ok="save">
    <a-form layout="vertical" class="form">
      <div class="row">
        <a-form-item label="名称" required extra="在规则中以名称引用。">
          <a-input v-model:value="form.name" class="mono" :maxlength="64" placeholder="如 MyDirect" />
        </a-form-item>
        <a-form-item label="行为">
          <a-select v-model:value="form.behavior" :options="[{ value: 'domain', label: '域名（domain）' }, { value: 'ipcidr', label: 'IP 段（ipcidr）' }, { value: 'classical', label: '混合规则（classical）' }]" />
        </a-form-item>
      </div>
      <a-form-item label="来源">
        <a-radio-group v-model:value="form.source" button-style="solid">
          <a-radio-button value="hosted">托管（在这里编辑）</a-radio-button>
          <a-radio-button value="url">外部链接</a-radio-button>
        </a-radio-group>
      </a-form-item>
      <a-form-item v-if="form.source === 'hosted'" label="规则内容" :extra="HINT[form.behavior] + ' # 开头为注释。'">
        <a-textarea v-model:value="form.content" class="mono" :auto-size="{ minRows: 10, maxRows: 20 }" />
      </a-form-item>
      <template v-else>
        <a-form-item label="链接" required extra="由面板拉取后提供给客户端，客户端无需能访问该链接（需在「设置 → 访问」中填写公开访问地址）。">
          <a-input v-model:value="form.url" class="mono" placeholder="https://example.com/rules/MyDirect.txt" />
        </a-form-item>
        <div class="row">
          <a-form-item label="格式" extra="以文件内容为准，与扩展名无关：含 payload: 的是 yaml，每行一条的是 text。">
            <a-select v-model:value="form.format" :options="[{ value: '', label: '默认（yaml）' }, { value: 'text', label: 'text' }, { value: 'mrs', label: 'mrs' }]" />
          </a-form-item>
          <a-form-item label="更新间隔（秒）">
            <a-input-number v-model:value="form.interval" :min="600" :max="604800" :step="3600" class="full" />
          </a-form-item>
        </div>
      </template>
      <a-form-item label="备注">
        <a-input v-model:value="form.note" :maxlength="100" />
      </a-form-item>
      <a-alert v-if="notice.warning" type="warning" show-icon :message="notice.warning" description="确认无误可再次点击保存。">
        <template v-if="notice.suggest" #action>
          <a-button size="small" type="primary" :loading="saving" @click="applySuggest">改为 {{ notice.suggest }} 并保存</a-button>
        </template>
      </a-alert>
    </a-form>
  </a-modal>
</template>

<style scoped>
.form {
  margin-top: 12px;
}
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 14px;
}
.full {
  width: 100%;
}
</style>
