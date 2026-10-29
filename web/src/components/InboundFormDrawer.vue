<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { Dice5, FileCheck2, KeyRound, Plus, Radar, Shuffle } from 'lucide-vue-next'
import { api, type CertProbe, type InboundItem, type NodeItem, type ProbeTargetResult } from '../lib/api'
import { datetime } from '../lib/format'
import { PRESETS, presetOf, type Preset } from '../lib/presets'

// 新建或编辑入站：按协议组合显示 Reality / TLS / 传输 / Shadowsocks 参数区块。
const props = defineProps<{ inbound: InboundItem | null; nodes: NodeItem[]; presetNodeId?: number }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [] }>()

// 常用 Reality 目标网站：支持 TLS 1.3 / H2 的大型站点，实际可用性以「检测」结果为准。
// 不包含 Xray 明确警告的 apple / icloud / microsoft 类域名。
const TARGETS = [
  'dl.google.com', 'www.amazon.com', 'addons.mozilla.org', 'www.lovelive-anime.jp',
  'www.nvidia.com', 'www.amd.com', 'www.cisco.com', 'www.yahoo.com',
]
const FINGERPRINTS = ['chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random', 'randomized']
const SS_METHODS = ['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm']
const XHTTP_MODES = ['auto', 'packet-up', 'stream-up', 'stream-one']

// 第三方 Reality 客户端上报的版本号很低（Mihomo 固定为 1.8.2），而 Xray v26.3.27 之后的版本
// 默认拒绝低于 26.3.27 的客户端，因此兼容模式把 minClientVer 设为 1.8.0。
const COMPAT_CLIENT_VER = '1.8.0'

// 与 Xray 的告警规则一致：这些 SNI 会增加服务器 IP 被封锁的概率。
function riskySNI(sn: string) {
  const s = sn.toLowerCase()
  return /\.(ru|ir|cn)$/.test(s) || ['apple', 'icloud', 'microsoft'].some((k) => s.includes(k))
}

function randomPath() {
  return '/' + Array.from(crypto.getRandomValues(new Uint8Array(6)), (b) => b.toString(16).padStart(2, '0')).join('')
}

/** 表单持有全部区块的参数，切换组合时互不丢失；保存时后端只保留当前组合需要的部分。 */
function blankSpec() {
  return {
    network: 'tcp' as string,
    security: 'reality' as string,
    vless: { flow: 'xtls-rprx-vision' },
    reality: { target: 'dl.google.com:443', serverNames: ['dl.google.com'], privateKey: '', publicKey: '', shortIds: [] as string[], fingerprint: 'chrome', spiderX: '/', minClientVer: COMPAT_CLIENT_VER },
    tls: { serverName: '', certMode: 'acme', certFile: '', keyFile: '', alpn: ['h2', 'http/1.1'], fingerprint: 'chrome' },
    ws: { path: randomPath(), host: '' },
    xhttp: { path: randomPath(), host: '', mode: 'auto' },
    ss: { method: SS_METHODS[0], serverKey: '' },
    sniffing: true,
  }
}

const form = reactive({
  nodeId: undefined as number | undefined,
  remark: '',
  enabled: true,
  listen: '',
  port: 443,
  linkHost: '',
  linkPort: 0,
  preset: PRESETS[0].key,
  spec: blankSpec(),
})
const saving = ref(false)
const probing = ref(false)
const probe = ref<ProbeTargetResult | null>(null)
const certProbe = ref<CertProbe | null>(null)
const advanced = ref<string[]>([])
let sniTouched = false

const editing = computed(() => !!props.inbound)
const preset = computed<Preset>(() => PRESETS.find((p) => p.key === form.preset) ?? PRESETS[0])
const isReality = computed(() => preset.value.security === 'reality')
const isTLS = computed(() => preset.value.security === 'tls')
const isSS = computed(() => preset.value.protocol === 'shadowsocks')
const nodeOptions = computed(() => props.nodes.map((n) => ({ value: n.id, label: n.name, online: n.runtime.online })))
const nodeOnline = computed(() => props.nodes.find((n) => n.id === form.nodeId)?.runtime.online ?? false)
const targetOptions = computed(() => TARGETS.map((t) => ({ value: `${t}:443` })))
const risky = computed(() => form.spec.reality.serverNames.filter(riskySNI))
const compat = computed({
  get: () => !!form.spec.reality.minClientVer,
  set: (v) => (form.spec.reality.minClientVer = v ? COMPAT_CLIENT_VER : ''),
})
const presetOptions = computed(() => PRESETS.map((p) => ({ value: p.key, label: p.label, hint: p.hint })))

// 切换组合时同步协议层字段；Vision 只用于 TCP + Reality。
watch(
  () => form.preset,
  () => {
    if (editing.value) return // 编辑时组合不可变，保留已保存的参数
    const p = preset.value
    form.spec.network = p.network
    form.spec.security = p.security
    form.spec.vless.flow = p.vision ? 'xtls-rprx-vision' : ''
    probe.value = certProbe.value = null
    if (p.protocol === 'shadowsocks' && !form.spec.ss.serverKey) genSSKey()
  },
)

function randomPort() {
  form.port = 10000 + Math.floor(Math.random() * 50000)
}

async function genKeys() {
  const r = await api.post<{ privateKey: string; publicKey: string }>('tools/x25519')
  form.spec.reality.privateKey = r.privateKey
  form.spec.reality.publicKey = r.publicKey
}

async function derivePublic() {
  const pk = form.spec.reality.privateKey.trim()
  if (!pk) return
  try {
    form.spec.reality.publicKey = (await api.post<{ publicKey: string }>('tools/x25519', { privateKey: pk })).publicKey
  } catch (e: any) {
    form.spec.reality.publicKey = ''
    message.error(e.message)
  }
}

async function addShortId() {
  const r = await api.get<{ shortId: string }>('tools/short-id?bytes=8')
  form.spec.reality.shortIds = [...form.spec.reality.shortIds, r.shortId]
}

async function genSSKey() {
  try {
    form.spec.ss.serverKey = (await api.get<{ key: string }>(`tools/ss-key?method=${form.spec.ss.method}`)).key
  } catch (e: any) {
    message.error(e.message)
  }
}

watch(
  () => form.spec.reality.target,
  (t) => {
    probe.value = null
    const host = t.split(':')[0]
    if (!sniTouched && host) form.spec.reality.serverNames = [host]
  },
)

watch(open, async (v) => {
  if (!v) return
  probe.value = certProbe.value = null
  advanced.value = []
  const src = props.inbound
  sniTouched = !!src
  if (src) {
    const base = blankSpec()
    const spec = JSON.parse(JSON.stringify(src.spec))
    Object.assign(form, {
      nodeId: src.nodeId, remark: src.remark, enabled: src.enabled, listen: src.listen, port: src.port,
      linkHost: src.linkHost, linkPort: src.linkPort, preset: presetOf(src)?.key ?? PRESETS[0].key,
      spec: { ...base, ...spec, vless: spec.vless ?? base.vless, reality: spec.reality ?? base.reality, tls: spec.tls ?? base.tls, ws: spec.ws ?? base.ws, xhttp: spec.xhttp ?? base.xhttp, ss: spec.ss ?? base.ss },
    })
    return
  }
  Object.assign(form, {
    nodeId: props.presetNodeId ?? props.nodes[0]?.id, remark: '', enabled: true, listen: '', port: 443,
    linkHost: '', linkPort: 0, preset: PRESETS[0].key, spec: blankSpec(),
  })
  try {
    await Promise.all([genKeys(), addShortId()])
  } catch (e: any) {
    message.error(e.message)
  }
})

async function runProbe() {
  if (!form.nodeId) return
  probing.value = true
  probe.value = null
  try {
    probe.value = await api.post<ProbeTargetResult>(`nodes/${form.nodeId}/probe`, {
      kind: 'reality_target',
      target: form.spec.reality.target,
      serverName: form.spec.reality.serverNames[0] ?? '',
    })
  } catch (e: any) {
    message.error(e.message)
  } finally {
    probing.value = false
  }
}

async function checkCertFiles() {
  if (!form.nodeId) return
  probing.value = true
  certProbe.value = null
  try {
    certProbe.value = await api.post<CertProbe>(`nodes/${form.nodeId}/probe`, {
      kind: 'cert_files', certFile: form.spec.tls.certFile, keyFile: form.spec.tls.keyFile,
    })
  } catch (e: any) {
    message.error(e.message)
  } finally {
    probing.value = false
  }
}

async function save() {
  if (!form.nodeId) {
    message.error('请选择服务器')
    return
  }
  saving.value = true
  try {
    const { preset: _, ...rest } = form
    const body = { ...rest, protocol: preset.value.protocol, linkPort: form.linkPort || 0 }
    if (editing.value) await api.put(`inbounds/${props.inbound!.id}`, body)
    else await api.post('inbounds', body)
    message.success(editing.value ? '已保存，正在下发到服务器' : '已创建，正在下发到服务器')
    open.value = false
    emit('saved')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <a-drawer v-model:open="open" :title="editing ? '编辑入站' : '新建入站'" :width="640" class="inbound-drawer">
    <a-form layout="vertical" @submit.prevent="save">
      <section class="sec">
        <h4>基础</h4>
        <a-form-item label="协议组合" :extra="preset.hint">
          <a-select v-model:value="form.preset" :options="presetOptions" :disabled="editing" />
        </a-form-item>
        <div class="row">
          <a-form-item label="服务器" required>
            <a-select v-model:value="form.nodeId" :options="nodeOptions" :disabled="editing" placeholder="选择服务器">
              <template #option="{ label, online }">
                <span class="opt"><i :class="['dot', { on: online }]" />{{ label }}</span>
              </template>
            </a-select>
          </a-form-item>
          <a-form-item label="备注">
            <a-input v-model:value="form.remark" :maxlength="64" placeholder="如：主力" />
          </a-form-item>
        </div>
        <div class="row">
          <a-form-item label="端口" required :extra="isSS ? '' : '推荐 443；被占用时会在保存前提示。'">
            <a-input-number v-model:value="form.port" :min="1" :max="65535" class="full mono">
              <template #addonAfter>
                <a-tooltip title="随机端口"><Dice5 :size="15" class="click" @click="randomPort" /></a-tooltip>
              </template>
            </a-input-number>
          </a-form-item>
          <a-form-item label="启用">
            <a-switch v-model:checked="form.enabled" />
          </a-form-item>
        </div>
      </section>

      <section v-if="isReality" class="sec">
        <h4>Reality</h4>
        <a-form-item label="目标网站（target）" required extra="伪装的目标站点，需支持 TLS 1.3。「检测」会从这台服务器发起真实握手。">
          <div class="inline">
            <a-auto-complete v-model:value="form.spec.reality.target" :options="targetOptions" class="grow mono" placeholder="dl.google.com:443" />
            <a-button :loading="probing" :disabled="!nodeOnline" @click="runProbe">
              <Radar :size="15" class="ico" />检测
            </a-button>
          </div>
          <div v-if="probe" class="probe">
            <a-tag v-if="probe.error && !probe.tls13" color="red" :bordered="false">{{ probe.error }}</a-tag>
            <template v-else>
              <a-tag :color="probe.tls13 ? 'green' : 'red'" :bordered="false">TLS 1.3 {{ probe.tls13 ? '支持' : '不支持' }}</a-tag>
              <a-tag :color="probe.h2 ? 'green' : 'orange'" :bordered="false">H2 {{ probe.h2 ? '支持' : '不支持' }}</a-tag>
              <a-tag :bordered="false" class="mono">{{ probe.latencyMs }} ms</a-tag>
            </template>
          </div>
          <a-alert v-if="risky.length" type="warning" show-icon class="hint" :message="`${risky.join('、')} 属于 Xray 告警的目标类型，会增加服务器 IP 被封锁的概率`" />
        </a-form-item>
        <a-form-item label="SNI（serverNames）" required extra="默认取目标网站域名；第一个用于生成分享链接。">
          <a-select v-model:value="form.spec.reality.serverNames" mode="tags" class="mono" :token-separators="[',', ' ']" @change="sniTouched = true" />
        </a-form-item>
        <a-form-item label="私钥" required>
          <div class="inline">
            <a-input v-model:value="form.spec.reality.privateKey" class="grow mono" @blur="derivePublic" />
            <a-button @click="genKeys"><KeyRound :size="15" class="ico" />重新生成</a-button>
          </div>
        </a-form-item>
        <a-form-item label="公钥" extra="由私钥派生，客户端链接中的 pbk。">
          <a-input :value="form.spec.reality.publicKey" readonly class="mono" />
        </a-form-item>
        <a-form-item label="ShortID" required extra="0 到 16 位偶数长度十六进制；第一个用于生成分享链接。">
          <div class="inline">
            <a-select v-model:value="form.spec.reality.shortIds" mode="tags" class="grow mono" :token-separators="[',', ' ']" />
            <a-button @click="addShortId"><Plus :size="15" class="ico" />添加</a-button>
          </div>
        </a-form-item>
        <a-form-item
          label="兼容第三方客户端"
          extra="Mihomo / Clash Verge / sing-box 等非 Xray 客户端需要开启，否则会被新版 Xray 拒绝连接。Xray 提示放宽该限制会增加服务器被识别的概率；只使用 Xray 内核客户端（v2rayN 等）时可关闭。"
        >
          <a-switch v-model:checked="compat" />
        </a-form-item>
        <div class="row">
          <a-form-item label="客户端指纹（fp）">
            <a-select v-model:value="form.spec.reality.fingerprint" :options="FINGERPRINTS.map((v) => ({ value: v }))" />
          </a-form-item>
          <a-form-item label="SpiderX">
            <a-input v-model:value="form.spec.reality.spiderX" class="mono" />
          </a-form-item>
        </div>
      </section>

      <section v-if="isTLS" class="sec">
        <h4>TLS</h4>
        <a-form-item label="域名" required extra="证书对应的域名，也是客户端连接时的 SNI。套 CDN 时填写 CDN 上的域名。">
          <a-input v-model:value="form.spec.tls.serverName" class="mono" placeholder="node.example.com" />
        </a-form-item>
        <a-form-item label="证书来源">
          <a-radio-group v-model:value="form.spec.tls.certMode" button-style="solid">
            <a-radio-button value="acme">自动申请</a-radio-button>
            <a-radio-button value="file">节点上的证书文件</a-radio-button>
          </a-radio-group>
          <p class="muted small">
            <template v-if="form.spec.tls.certMode === 'acme'">被控端通过 Let's Encrypt + Cloudflare DNS 验证申请并自动续签，需先在「设置 → 证书」填写 Cloudflare API Token。首次申请约需 1 分钟。</template>
            <template v-else>Xray 每小时重新读取证书文件，续签后无需重启。文件需能被 xhub 用户读取。</template>
          </p>
        </a-form-item>
        <template v-if="form.spec.tls.certMode === 'file'">
          <a-form-item label="证书路径（fullchain）" required>
            <a-input v-model:value="form.spec.tls.certFile" class="mono" placeholder="/etc/xhub-agent/certs/fullchain.pem" />
          </a-form-item>
          <a-form-item label="私钥路径" required>
            <div class="inline">
              <a-input v-model:value="form.spec.tls.keyFile" class="grow mono" placeholder="/etc/xhub-agent/certs/privkey.pem" />
              <a-button :loading="probing" :disabled="!nodeOnline" @click="checkCertFiles"><FileCheck2 :size="15" class="ico" />检查</a-button>
            </div>
            <div v-if="certProbe" class="probe">
              <a-tag v-if="!certProbe.ok" color="red" :bordered="false">{{ certProbe.error }}</a-tag>
              <template v-else>
                <a-tag color="green" :bordered="false">可读取</a-tag>
                <a-tag :bordered="false" class="mono">{{ certProbe.names?.join(', ') }}</a-tag>
                <a-tag :bordered="false" class="mono">到期 {{ datetime(certProbe.notAfter ?? 0) }}</a-tag>
              </template>
            </div>
          </a-form-item>
        </template>
        <div class="row">
          <a-form-item label="ALPN">
            <a-checkbox-group v-model:value="form.spec.tls.alpn" :options="['h2', 'http/1.1']" />
          </a-form-item>
          <a-form-item label="客户端指纹（fp）">
            <a-select v-model:value="form.spec.tls.fingerprint" :options="FINGERPRINTS.map((v) => ({ value: v }))" />
          </a-form-item>
        </div>
      </section>

      <section v-if="preset.network === 'ws' || preset.network === 'xhttp'" class="sec">
        <h4>{{ preset.network === 'ws' ? 'WebSocket' : 'XHTTP' }}</h4>
        <template v-if="preset.network === 'ws'">
          <a-form-item label="路径" extra="随机路径可降低被扫描的概率。">
            <a-input v-model:value="form.spec.ws.path" class="mono">
              <template #suffix>
                <a-tooltip title="随机生成"><Shuffle :size="15" class="click muted" @click="form.spec.ws.path = randomPath()" /></a-tooltip>
              </template>
            </a-input>
          </a-form-item>
          <a-form-item label="Host" extra="留空使用 TLS 域名。">
            <a-input v-model:value="form.spec.ws.host" class="mono" />
          </a-form-item>
        </template>
        <template v-else>
          <div class="row">
            <a-form-item label="路径">
              <a-input v-model:value="form.spec.xhttp.path" class="mono">
                <template #suffix>
                  <a-tooltip title="随机生成"><Shuffle :size="15" class="click muted" @click="form.spec.xhttp.path = randomPath()" /></a-tooltip>
                </template>
              </a-input>
            </a-form-item>
            <a-form-item label="模式">
              <a-select v-model:value="form.spec.xhttp.mode" :options="XHTTP_MODES.map((v) => ({ value: v }))" />
            </a-form-item>
          </div>
          <a-form-item label="Host" extra="留空使用 SNI。">
            <a-input v-model:value="form.spec.xhttp.host" class="mono" />
          </a-form-item>
        </template>
      </section>

      <section v-if="isSS" class="sec">
        <h4>Shadowsocks 2022</h4>
        <a-form-item label="加密方式" extra="Xray 的多用户 SS2022 仅支持 blake3-aes-*-gcm。">
          <a-select v-model:value="form.spec.ss.method" :options="SS_METHODS.map((v) => ({ value: v }))" @change="genSSKey" />
        </a-form-item>
        <a-form-item label="服务端密钥" required extra="每个订阅另有独立的用户密钥，自动派生。">
          <div class="inline">
            <a-input v-model:value="form.spec.ss.serverKey" class="grow mono" />
            <a-button @click="genSSKey"><KeyRound :size="15" class="ico" />重新生成</a-button>
          </div>
        </a-form-item>
      </section>

      <a-collapse v-model:activeKey="advanced" ghost class="adv">
        <a-collapse-panel key="adv" header="高级">
          <div class="row">
            <a-form-item v-if="preset.key === 'vless-reality-vision'" label="XTLS Vision" extra="推荐开启，客户端需同样使用 xtls-rprx-vision。">
              <a-switch :checked="!!form.spec.vless.flow" @change="(v: any) => (form.spec.vless.flow = v ? 'xtls-rprx-vision' : '')" />
            </a-form-item>
            <a-form-item label="流量探测（sniffing）" extra="仅用于路由判断，不改变目标地址。">
              <a-switch v-model:checked="form.spec.sniffing" />
            </a-form-item>
          </div>
          <a-form-item
            v-if="isReality"
            label="最低客户端版本（minClientVer）"
            extra="与「兼容第三方客户端」联动，可填写自定义值。留空使用 Xray 默认值（v26.3.27 之后的版本要求客户端不低于 26.3.27）。"
          >
            <a-input v-model:value="form.spec.reality.minClientVer" class="mono" placeholder="Xray 默认" />
          </a-form-item>
          <a-form-item label="监听 IP" extra="留空监听全部地址。">
            <a-input v-model:value="form.listen" class="mono" placeholder="0.0.0.0" />
          </a-form-item>
          <div class="row">
            <a-form-item label="链接地址" extra="留空使用服务器公网地址；套 CDN 时填写 CDN 域名或优选 IP。">
              <a-input v-model:value="form.linkHost" class="mono" />
            </a-form-item>
            <a-form-item label="链接端口" extra="0 表示使用入站端口，适用于端口转发场景。">
              <a-input-number v-model:value="form.linkPort" :min="0" :max="65535" class="full mono" />
            </a-form-item>
          </div>
        </a-collapse-panel>
      </a-collapse>
    </a-form>

    <template #footer>
      <div class="foot">
        <a-button @click="open = false">取消</a-button>
        <a-button type="primary" :loading="saving" @click="save">{{ editing ? '保存' : '创建' }}</a-button>
      </div>
    </template>
  </a-drawer>
</template>

<style scoped>
.sec {
  padding: 18px 18px 2px;
  margin-bottom: 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
}
.sec h4 {
  margin: 0 0 14px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--text-3);
}
.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 14px;
}
.inline {
  display: flex;
  gap: 8px;
}
.grow {
  flex: 1;
  min-width: 0;
}
.full {
  width: 100%;
}
.mono :deep(input) {
  font-family: var(--font-mono);
}
.ico {
  margin-right: 6px;
  vertical-align: -2px;
}
.click {
  cursor: pointer;
}
.small {
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 1.6;
}
.probe {
  margin-top: 8px;
}
.hint {
  margin-top: 8px;
}
.opt {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--idle);
}
.dot.on {
  background: var(--ok);
}
.adv :deep(.ant-collapse-header) {
  padding-left: 4px !important;
  color: var(--text-2);
}
.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
@media (max-width: 640px) {
  .row {
    grid-template-columns: 1fr;
  }
}
</style>
