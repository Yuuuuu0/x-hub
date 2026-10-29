<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Modal, message } from 'ant-design-vue'
import { Cloud, Download, FileKey, ShieldCheck, Shuffle } from 'lucide-vue-next'
import QRCode from 'qrcode'
import { BASE, api, session, type AccessSettings, type AllSettings, type Release, type TLSMode } from '../lib/api'

// Cloudflare 公布的回源 IP 段（https://www.cloudflare.com/ips/），可能变动，按需更新。
const CLOUDFLARE_RANGES = [
  '173.245.48.0/20', '103.21.244.0/22', '103.22.200.0/22', '103.31.4.0/22', '141.101.64.0/18',
  '108.162.192.0/18', '190.93.240.0/20', '188.114.96.0/20', '197.234.240.0/22', '198.41.128.0/17',
  '162.158.0.0/15', '104.16.0.0/13', '104.24.0.0/14', '172.64.0.0/13', '131.0.72.0/22',
  '2400:cb00::/32', '2606:4700::/32', '2803:f800::/32', '2405:b500::/32', '2405:8100::/32',
  '2a06:98c0::/29', '2c0f:f248::/32',
]

const TLS_MODES: { value: TLSMode; title: string; desc: string; icon: any }[] = [
  { value: 'proxy', title: '反向代理', desc: '由 Nginx / Caddy / Cloudflare 终结 TLS，主控只提供 HTTP', icon: Cloud },
  { value: 'file', title: '证书文件', desc: '读取外部脚本续签的证书，文件更新后自动热加载', icon: FileKey },
  { value: 'selfsigned', title: '自签证书', desc: '无需域名，被控端通过公钥指纹校验主控身份', icon: ShieldCheck },
]

const tab = ref('access')
const loaded = ref(false)
const fingerprint = ref('')
const access = reactive<AccessSettings>({
  listen: '', panelPath: '', publicUrl: '', tlsMode: 'selfsigned', certFile: '', keyFile: '', trustedProxies: [],
})
const proxiesText = ref('')
let savedAccess: AccessSettings | null = null

const xray = reactive({ defaultVersion: undefined as string | undefined, maxVersion: undefined as string | undefined })
const releases = ref<Release[]>([])
const releasesLoading = ref(false)

const subSettings = reactive({ path: '', updateHours: 12, nameTemplate: '' })
const statsSettings = reactive({ timezone: 'Asia/Shanghai', retentionDays: 90 })
const TIMEZONES = ['Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Taipei', 'Asia/Tokyo', 'Asia/Singapore', 'Europe/London', 'Europe/Berlin', 'America/New_York', 'America/Los_Angeles', 'UTC'].map((value) => ({ value }))
const namePreview = computed(() =>
  subSettings.nameTemplate.replace(/\{node\}/g, '东京-01').replace(/\{remark\}/g, '主力').replace(/\{port\}/g, '443').replace(/\s+/g, ' ').trim(),
)

const sessionHours = ref(168)
const account = reactive({ username: '', currentPassword: '', newPassword: '', confirm: '' })

const saving = reactive({ access: false, xray: false, security: false, account: false, sub: false, stats: false, certs: false, totp: false })

const certs = reactive({ acmeEmail: '', cloudflareToken: '', tokenSet: false })
const totp = reactive({ secret: '', qr: '', code: '', password: '' })

async function load() {
  const s = await api.get<AllSettings>('settings')
  Object.assign(access, s.access)
  savedAccess = { ...s.access }
  proxiesText.value = (s.access.trustedProxies ?? []).join('\n')
  fingerprint.value = s.selfSignedFingerprint ?? ''
  xray.defaultVersion = s.xray.defaultVersion || undefined
  xray.maxVersion = s.xray.maxVersion || undefined
  sessionHours.value = s.security.sessionHours
  Object.assign(subSettings, s.subscription)
  Object.assign(statsSettings, s.stats)
  certs.acmeEmail = s.certs.acmeEmail
  certs.tokenSet = s.certs.cloudflareTokenSet
  certs.cloudflareToken = ''
  account.username = session.username
  loaded.value = true
}

async function loadReleases() {
  releasesLoading.value = true
  try {
    releases.value = (await api.get<{ releases: Release[] }>('xray/releases')).releases
  } catch (e: any) {
    message.error(e.message)
  } finally {
    releasesLoading.value = false
  }
}

onMounted(() => {
  load().catch((e) => message.error(e.message))
  loadReleases()
})

const releaseOptions = computed(() =>
  releases.value.map((r) => ({ value: r.tag, label: r.prerelease ? `${r.tag}（预发布）` : r.tag })),
)

function randomString(n: number) {
  const chars = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  return Array.from(crypto.getRandomValues(new Uint8Array(n)), (b) => chars[b % chars.length]).join('')
}

function randomPath() {
  access.panelPath = randomString(12)
}

async function saveCerts() {
  saving.certs = true
  try {
    const r = await api.put<{ acmeEmail: string; cloudflareTokenSet: boolean }>('settings/certs', { acmeEmail: certs.acmeEmail, cloudflareToken: certs.cloudflareToken })
    certs.tokenSet = r.cloudflareTokenSet
    certs.cloudflareToken = ''
    message.success('证书设置已保存，需要证书的服务器会重新尝试申请')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.certs = false
  }
}

async function startTotp() {
  try {
    const r = await api.post<{ secret: string; uri: string }>('account/totp/setup')
    totp.secret = r.secret
    totp.qr = await QRCode.toDataURL(r.uri, { margin: 1, width: 220 })
    totp.code = ''
  } catch (e: any) {
    message.error(e.message)
  }
}

async function enableTotp() {
  saving.totp = true
  try {
    await api.post('account/totp/enable', { secret: totp.secret, code: totp.code })
    session.totpEnabled = true
    Object.assign(totp, { secret: '', qr: '', code: '' })
    message.success('两步验证已启用，下次登录需要输入验证码')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.totp = false
  }
}

async function disableTotp() {
  saving.totp = true
  try {
    await api.post('account/totp/disable', { password: totp.password })
    session.totpEnabled = false
    totp.password = ''
    message.success('两步验证已关闭')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.totp = false
  }
}

function downloadBackup() {
  const a = document.createElement('a')
  a.href = `${BASE}api/backup`
  a.click()
}

async function saveStats() {
  saving.stats = true
  try {
    Object.assign(statsSettings, await api.put('settings/stats', statsSettings))
    message.success('统计设置已保存')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.stats = false
  }
}

async function saveSub() {
  saving.sub = true
  try {
    Object.assign(subSettings, await api.put('settings/subscription', subSettings))
    message.success('订阅设置已保存')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.sub = false
  }
}

function fillCloudflare() {
  const set = new Set(proxiesText.value.split('\n').map((s) => s.trim()).filter(Boolean))
  CLOUDFLARE_RANGES.forEach((r) => set.add(r))
  proxiesText.value = [...set].join('\n')
}

/** 计算保存后的面板地址，用于切换监听 / 路径 / TLS 后引导跳转。 */
function panelUrl(a: AccessSettings) {
  if (a.publicUrl) return `${a.publicUrl.replace(/\/$/, '')}/${a.panelPath}/settings`
  const port = a.listen.split(':').pop()
  const scheme = a.tlsMode === 'proxy' ? 'http:' : 'https:'
  return `${scheme}//${location.hostname}:${port}/${a.panelPath}/settings`
}

function saveAccess() {
  const next: AccessSettings = {
    ...access,
    trustedProxies: proxiesText.value.split('\n').map((s) => s.trim()).filter(Boolean),
  }
  const prev = savedAccess!
  const moving = prev.listen !== next.listen || prev.panelPath !== next.panelPath || prev.tlsMode !== next.tlsMode
  const run = async () => {
    saving.access = true
    try {
      const saved = await api.put<AccessSettings>('settings/access', next)
      Object.assign(access, saved)
      savedAccess = { ...saved }
      session.publicUrlSet = !!saved.publicUrl
      const s = await api.get<AllSettings>('settings').catch(() => null)
      if (s) fingerprint.value = s.selfSignedFingerprint ?? ''
      const target = panelUrl(saved)
      if (moving && new URL(target).origin + new URL(target).pathname !== location.origin + BASE + 'settings') {
        Modal.success({
          title: '访问设置已生效',
          content: `面板地址已变更为 ${target}，旧地址将很快失效。已在线的被控端会自动切换到新地址。`,
          okText: '前往新地址',
          onOk: () => location.assign(target),
        })
      } else {
        message.success('访问设置已保存')
      }
    } catch (e: any) {
      message.error(e.message)
    } finally {
      saving.access = false
    }
  }
  if (!moving) return run()
  Modal.confirm({
    title: '确认修改访问设置？',
    content: '监听地址、面板路径或 TLS 模式变化后，当前页面地址会失效。请确认新的访问方式可用，否则需在主控上执行 settings reset-access 恢复。',
    okText: '保存并切换',
    cancelText: '取消',
    onOk: run,
  })
}

async function saveXray() {
  saving.xray = true
  try {
    await api.put('settings/xray', { defaultVersion: xray.defaultVersion ?? '', maxVersion: xray.maxVersion ?? '' })
    message.success('Xray 设置已保存')
    loadReleases()
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.xray = false
  }
}

async function saveSecurity() {
  saving.security = true
  try {
    await api.put('settings/security', { sessionHours: sessionHours.value })
    message.success('已保存，新会话生效')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.security = false
  }
}

async function saveAccount() {
  if (account.newPassword && account.newPassword !== account.confirm) {
    message.error('两次输入的新密码不一致')
    return
  }
  saving.account = true
  try {
    const r = await api.put<{ username: string }>('account', {
      username: account.username,
      currentPassword: account.currentPassword,
      newPassword: account.newPassword,
    })
    session.username = r.username
    Object.assign(account, { currentPassword: '', newPassword: '', confirm: '' })
    message.success('账户已更新，其他设备上的会话已失效')
  } catch (e: any) {
    message.error(e.message)
  } finally {
    saving.account = false
  }
}
</script>

<template>
  <section class="page">
    <header class="hero">
      <p class="eyebrow mono">SETTINGS</p>
      <h1>设置</h1>
    </header>

    <a-skeleton v-if="!loaded" active />
    <a-tabs v-else v-model:activeKey="tab" class="tabs">
      <a-tab-pane key="access" tab="访问">
        <a-form layout="vertical" class="form" @submit.prevent="saveAccess">
          <div class="group">
            <h2>地址</h2>
            <a-form-item label="公开访问地址" extra="被控端连接主控、生成安装命令与订阅链接时使用。只含协议、域名与端口。">
              <a-input v-model:value="access.publicUrl" class="mono" placeholder="https://hub.example.com:2096" />
            </a-form-item>
            <div class="row">
              <a-form-item label="监听地址" extra="容器使用 host 网络时，修改后直接生效。">
                <a-input v-model:value="access.listen" class="mono" placeholder=":2096" />
              </a-form-item>
              <a-form-item label="面板路径" extra="面板只能通过该路径访问，其余路径一律 404。">
                <a-input v-model:value="access.panelPath" class="mono" addon-before="/">
                  <template #suffix>
                    <a-tooltip title="随机生成">
                      <Shuffle :size="15" class="clickable" @click="randomPath" />
                    </a-tooltip>
                  </template>
                </a-input>
              </a-form-item>
            </div>
          </div>

          <div class="group">
            <h2>TLS</h2>
            <div class="modes">
              <button
                v-for="m in TLS_MODES"
                :key="m.value"
                type="button"
                :class="['mode', { active: access.tlsMode === m.value }]"
                @click="access.tlsMode = m.value"
              >
                <component :is="m.icon" :size="20" :stroke-width="1.6" />
                <strong>{{ m.title }}</strong>
                <span>{{ m.desc }}</span>
              </button>
            </div>
            <div v-if="access.tlsMode === 'file'" class="row">
              <a-form-item label="证书路径（fullchain）" extra="Docker 部署时填写容器内路径。">
                <a-input v-model:value="access.certFile" class="mono" placeholder="/certs/fullchain.pem" />
              </a-form-item>
              <a-form-item label="私钥路径">
                <a-input v-model:value="access.keyFile" class="mono" placeholder="/certs/privkey.pem" />
              </a-form-item>
            </div>
            <a-form-item v-if="access.tlsMode === 'selfsigned' && fingerprint" label="公钥指纹（SHA-256）" extra="安装命令会自动携带该指纹。">
              <a-typography-paragraph :copyable="{ text: fingerprint }" class="fp mono">{{ fingerprint }}</a-typography-paragraph>
            </a-form-item>
          </div>

          <div class="group">
            <h2>可信代理</h2>
            <a-form-item extra="来自这些地址的请求才会采信 CF-Connecting-IP / X-Forwarded-For，用于登录限流与日志中的真实 IP。每行一个 IP 或 CIDR。">
              <a-textarea v-model:value="proxiesText" class="mono" :auto-size="{ minRows: 3, maxRows: 10 }" placeholder="127.0.0.1" />
            </a-form-item>
            <a-button size="small" @click="fillCloudflare">
              <Cloud :size="14" class="btn-ico" />
              填入 Cloudflare IP 段
            </a-button>
          </div>

          <div class="actions">
            <a-button type="primary" size="large" html-type="submit" :loading="saving.access">保存访问设置</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="xray" tab="Xray">
        <a-form layout="vertical" class="form" @submit.prevent="saveXray">
          <div class="group">
            <h2>版本</h2>
            <a-form-item label="默认安装版本" extra="新接入的服务器自动安装该版本。留空则使用不超过上限的最新稳定版。">
              <a-select v-model:value="xray.defaultVersion" :options="releaseOptions" :loading="releasesLoading" allow-clear show-search placeholder="最新稳定版" class="mono" />
            </a-form-item>
            <a-form-item label="版本上限" extra="高于该版本的 Xray 不允许安装，适合需要兼容特定客户端的场景。">
              <a-select v-model:value="xray.maxVersion" :options="releaseOptions" :loading="releasesLoading" allow-clear show-search placeholder="不限制" class="mono" />
            </a-form-item>
          </div>
          <div class="actions">
            <a-button type="primary" size="large" html-type="submit" :loading="saving.xray">保存 Xray 设置</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="subscription" tab="订阅">
        <a-form layout="vertical" class="form" @submit.prevent="saveSub">
          <div class="group">
            <h2>订阅链接</h2>
            <a-form-item label="订阅路径" extra="订阅链接形如 公开访问地址/订阅路径/令牌。修改后已分发的订阅链接全部失效。">
              <a-input v-model:value="subSettings.path" class="mono" addon-before="/">
                <template #suffix>
                  <a-tooltip title="随机生成">
                    <Shuffle :size="15" class="clickable" @click="subSettings.path = randomString(8)" />
                  </a-tooltip>
                </template>
              </a-input>
            </a-form-item>
            <a-form-item label="自动更新间隔（小时）" extra="通过 profile-update-interval 告知客户端。">
              <a-input-number v-model:value="subSettings.updateHours" :min="1" :max="168" class="num" />
            </a-form-item>
            <a-form-item label="节点名称模板" :extra="`可用变量 {node} {remark} {port}。预览：${namePreview || '（空）'}`">
              <a-input v-model:value="subSettings.nameTemplate" class="mono" placeholder="{node} {remark}" />
            </a-form-item>
          </div>
          <div class="actions">
            <a-button type="primary" size="large" html-type="submit" :loading="saving.sub">保存订阅设置</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="stats" tab="统计">
        <a-form layout="vertical" class="form" @submit.prevent="saveStats">
          <div class="group">
            <h2>流量统计</h2>
            <a-form-item label="统计时区" extra="决定按天汇总、每月流量重置与图表日期的分界。填写 IANA 时区名。">
              <a-auto-complete v-model:value="statsSettings.timezone" :options="TIMEZONES" class="mono" />
            </a-form-item>
            <a-form-item label="小时明细保留天数" extra="超过后按天汇总，图表与用量不受影响，只是不再保留小时粒度。">
              <a-input-number v-model:value="statsSettings.retentionDays" :min="7" :max="3650" class="num" />
            </a-form-item>
          </div>
          <div class="actions">
            <a-button type="primary" size="large" html-type="submit" :loading="saving.stats">保存统计设置</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="certs" tab="证书">
        <a-form layout="vertical" class="form" @submit.prevent="saveCerts">
          <div class="group">
            <h2>自动申请证书（ACME）</h2>
            <p class="muted desc">入站选择「自动申请」证书时，被控端使用 Let's Encrypt 与 Cloudflare DNS 验证签发并自动续签证书，无需开放 80 端口。域名需托管在 Cloudflare。</p>
            <a-form-item label="ACME 邮箱" extra="用于注册 Let's Encrypt 账户与接收证书到期提醒。">
              <a-input v-model:value="certs.acmeEmail" placeholder="you@example.com" />
            </a-form-item>
            <a-form-item label="Cloudflare API Token" extra="在 Cloudflare 创建 API Token，权限选择 Zone → DNS → Edit。Token 会下发给需要证书的服务器。">
              <a-input-password v-model:value="certs.cloudflareToken" :placeholder="certs.tokenSet ? '已设置，留空保持不变' : '未设置'" autocomplete="off" />
            </a-form-item>
          </div>
          <div class="actions">
            <a-button type="primary" size="large" html-type="submit" :loading="saving.certs">保存证书设置</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="account" tab="账户与安全">
        <a-form layout="vertical" class="form" @submit.prevent="saveAccount">
          <div class="group">
            <h2>管理员账户</h2>
            <a-form-item label="用户名">
              <a-input v-model:value="account.username" autocomplete="username" />
            </a-form-item>
            <div class="row">
              <a-form-item label="新密码" extra="留空则不修改，至少 8 位。">
                <a-input-password v-model:value="account.newPassword" autocomplete="new-password" />
              </a-form-item>
              <a-form-item label="确认新密码">
                <a-input-password v-model:value="account.confirm" autocomplete="new-password" />
              </a-form-item>
            </div>
            <a-form-item label="当前密码" required>
              <a-input-password v-model:value="account.currentPassword" autocomplete="current-password" />
            </a-form-item>
            <a-button type="primary" html-type="submit" :loading="saving.account">更新账户</a-button>
          </div>
        </a-form>
        <div class="form">
          <div class="group">
            <h2>两步验证</h2>
            <template v-if="session.totpEnabled">
              <p class="desc"><ShieldCheck :size="15" class="btn-ico ok" />已启用。登录时需要输入验证器 App 中的 6 位验证码。</p>
              <a-form-item label="当前密码" extra="关闭两步验证需要验证当前密码。">
                <div class="inline">
                  <a-input-password v-model:value="totp.password" autocomplete="current-password" />
                  <a-button danger :loading="saving.totp" @click="disableTotp">关闭两步验证</a-button>
                </div>
              </a-form-item>
            </template>
            <template v-else-if="totp.secret">
              <p class="muted desc">用 Google Authenticator、1Password 等验证器 App 扫描二维码，然后输入生成的验证码确认。</p>
              <div class="totp">
                <img :src="totp.qr" alt="两步验证二维码" />
                <div class="totp-side">
                  <p class="muted small">无法扫码时手动输入密钥：</p>
                  <a-typography-paragraph :copyable="{ text: totp.secret }" class="mono secret">{{ totp.secret }}</a-typography-paragraph>
                  <a-input v-model:value="totp.code" class="mono" :maxlength="6" placeholder="6 位验证码" inputmode="numeric" />
                  <a-button type="primary" :loading="saving.totp" @click="enableTotp">确认启用</a-button>
                </div>
              </div>
            </template>
            <template v-else>
              <p class="muted desc">启用后，登录除密码外还需要验证器 App 生成的动态验证码。忘记时可在主控执行 admin reset 关闭。</p>
              <a-button @click="startTotp"><ShieldCheck :size="14" class="btn-ico" />启用两步验证</a-button>
            </template>
          </div>
        </div>
        <a-form layout="vertical" class="form" @submit.prevent="saveSecurity">
          <div class="group">
            <h2>会话</h2>
            <a-form-item label="登录有效期（小时）">
              <a-input-number v-model:value="sessionHours" :min="1" :max="2160" class="num" />
            </a-form-item>
            <a-button html-type="submit" :loading="saving.security">保存</a-button>
          </div>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="backup" tab="备份">
        <div class="form">
          <div class="group">
            <h2>备份</h2>
            <p class="muted desc">备份文件包含全部设置、服务器、入站、订阅与流量统计，以及节点凭据与 Reality 私钥，请妥善保管。</p>
            <a-button type="primary" @click="downloadBackup"><Download :size="14" class="btn-ico" />下载备份</a-button>
          </div>
          <div class="group">
            <h2>恢复</h2>
            <p class="muted desc">恢复需要在主控机器上执行。把备份文件放入数据目录后：</p>
            <pre class="mono code">docker compose stop
docker compose run --rm xhub restore /data/xhub-backup-xxxx.db --data /data
docker compose up -d</pre>
            <p class="muted desc">原数据库会保留为 xhub.db.bak-时间戳。被控端凭据随备份一起恢复，无需重新接入。</p>
          </div>
        </div>
      </a-tab-pane>
    </a-tabs>
  </section>
</template>

<style scoped>
.page {
  max-width: 880px;
}

.hero {
  margin-bottom: 20px;
}

.eyebrow {
  margin: 0 0 6px;
  font-size: 12px;
  letter-spacing: 0.18em;
  color: var(--text-3);
}

h1 {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(44px, 6vw, 76px);
  font-weight: 700;
  letter-spacing: -0.05em;
  line-height: 0.95;
}

.form {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-bottom: 16px;
}

.group {
  padding: 22px 24px 8px;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--border);
}

.group h2 {
  margin: 0 0 16px;
  font-family: var(--font-display);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--text-3);
}

.group > :deep(.ant-btn) {
  margin-bottom: 16px;
}

.row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

.modes {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 20px;
}

.mode {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 14px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--surface-2);
  color: var(--text-2);
  text-align: left;
  cursor: pointer;
  font: inherit;
  transition: border-color 0.15s, background 0.15s;
}

.mode strong {
  color: var(--text);
  font-size: 14px;
}

.mode span {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-3);
}

.mode:hover {
  border-color: var(--border-strong);
}

.mode.active {
  border-color: var(--text);
  background: var(--surface);
  box-shadow: inset 0 0 0 1px var(--text);
}

:root[data-theme='dark'] .mode.active {
  border-color: var(--lime);
  box-shadow: inset 0 0 0 1px var(--lime);
}

.desc {
  margin: -6px 0 16px;
  font-size: 13px;
  line-height: 1.7;
}
.ok {
  color: var(--ok);
}
.inline {
  display: flex;
  gap: 8px;
}
.totp {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  margin-bottom: 16px;
}
.totp img {
  width: 180px;
  height: 180px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: #fff;
  padding: 6px;
}
.totp-side {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.small {
  margin: 0;
  font-size: 12px;
}
.secret {
  margin: 0 !important;
  font-size: 12px;
  word-break: break-all;
}
.code {
  margin: 0 0 14px;
  padding: 12px 14px;
  border-radius: 10px;
  background: var(--ink);
  color: #d8f7a0;
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
}
.fp {
  margin: 0 !important;
  font-size: 12px;
  word-break: break-all;
}

.clickable {
  cursor: pointer;
  color: var(--text-3);
}

.btn-ico {
  margin-right: 6px;
  vertical-align: -2px;
}

.actions {
  display: flex;
  justify-content: flex-end;
}

.num {
  width: 180px;
}

@media (max-width: 720px) {
  .row,
  .modes {
    grid-template-columns: 1fr;
  }
  .group {
    padding: 18px 16px 4px;
  }
}
</style>
