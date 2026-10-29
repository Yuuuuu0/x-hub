<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRight, Lock, User } from 'lucide-vue-next'
import { ApiError, api, loadSession, session } from '../lib/api'

const router = useRouter()
const route = useRoute()
const form = reactive({ username: '', password: '' })
const loading = ref(false)
const error = ref('')

async function submit() {
  if (!form.username || !form.password) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await api.post('login', form)
    await loadSession()
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/nodes'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="login">
    <section class="poster" aria-hidden="true">
      <div class="poster-meta mono">
        <span>MASTER / AGENT</span>
        <span>XRAY-CORE</span>
      </div>
      <h1 class="wordmark">
        <span class="line">X<span class="dash">—</span></span>
        <span class="line outline">HUB</span>
      </h1>
      <div class="scan" />
      <p class="tagline">一台主控，<br />管理所有节点。</p>
      <div class="poster-foot mono">
        <span>{{ session.version || 'dev' }}</span>
        <span class="blink">●</span>
        <span>SELF-HOSTED</span>
      </div>
    </section>

    <section class="panel">
      <form class="card" @submit.prevent="submit">
        <div class="card-head">
          <span class="mono step">01 / 登录</span>
          <h2>欢迎回来</h2>
          <p class="muted">使用管理员账号登录面板</p>
        </div>
        <a-input v-model:value="form.username" size="large" placeholder="用户名" autocomplete="username" autofocus>
          <template #prefix><User :size="16" class="ico" /></template>
        </a-input>
        <a-input-password v-model:value="form.password" size="large" placeholder="密码" autocomplete="current-password">
          <template #prefix><Lock :size="16" class="ico" /></template>
        </a-input-password>
        <p v-if="error" class="error">{{ error }}</p>
        <a-button type="primary" size="large" html-type="submit" block :loading="loading" class="submit">
          进入控制台
          <ArrowRight :size="16" />
        </a-button>
        <p class="hint muted">忘记密码？在主控上执行 <code class="mono">xhub-master admin reset</code></p>
      </form>
    </section>
  </main>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(360px, 1fr);
}

.poster {
  position: relative;
  overflow: hidden;
  background: var(--ink);
  color: #eef0f3;
  padding: 40px 48px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  isolation: isolate;
}

.poster::before {
  content: '';
  position: absolute;
  inset: 0;
  z-index: -1;
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.05) 1px, transparent 1px);
  background-size: 64px 64px;
  mask-image: radial-gradient(ellipse at 30% 60%, #000 20%, transparent 75%);
}

.poster-meta,
.poster-foot {
  display: flex;
  gap: 24px;
  font-size: 12px;
  letter-spacing: 0.16em;
  color: rgba(238, 240, 243, 0.55);
}

.wordmark {
  margin: 0;
  font-family: var(--font-display);
  font-weight: 700;
  font-size: clamp(96px, 17vw, 260px);
  line-height: 0.82;
  letter-spacing: -0.06em;
  display: flex;
  flex-direction: column;
}

.wordmark .line {
  display: block;
  animation: rise 0.9s cubic-bezier(0.2, 0.8, 0.2, 1) both;
}

.wordmark .line:nth-child(2) {
  animation-delay: 0.08s;
}

.dash {
  color: var(--lime);
}

.outline {
  color: transparent;
  -webkit-text-stroke: 2px rgba(238, 240, 243, 0.85);
}

.scan {
  position: absolute;
  left: 0;
  right: 0;
  height: 2px;
  top: 0;
  background: linear-gradient(90deg, transparent, var(--lime), transparent);
  box-shadow: 0 0 24px 4px rgba(198, 255, 61, 0.35);
  animation: scan 6s cubic-bezier(0.65, 0, 0.35, 1) infinite;
  opacity: 0.8;
}

.tagline {
  position: absolute;
  right: 48px;
  bottom: 96px;
  margin: 0;
  font-size: 20px;
  line-height: 1.5;
  text-align: right;
  color: rgba(238, 240, 243, 0.8);
}

.blink {
  color: var(--lime);
  animation: blink 1.6s steps(2, start) infinite;
}

.panel {
  display: grid;
  place-items: center;
  padding: 48px 24px;
}

.card {
  width: 100%;
  max-width: 380px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  animation: rise 0.7s 0.15s cubic-bezier(0.2, 0.8, 0.2, 1) both;
}

.card-head {
  margin-bottom: 12px;
}

.step {
  font-size: 12px;
  letter-spacing: 0.12em;
  color: var(--text-3);
}

.card-head h2 {
  font-family: var(--font-display);
  font-size: 40px;
  letter-spacing: -0.03em;
  line-height: 1.1;
  margin: 10px 0 6px;
  color: var(--text);
}

.card-head p {
  margin: 0;
}

.ico {
  color: var(--text-3);
}

.submit {
  margin-top: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.error {
  margin: 0;
  color: var(--bad);
  font-size: 13px;
}

.hint {
  font-size: 12px;
  margin: 8px 0 0;
}

.hint code {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 6px;
  background: var(--surface-2);
  border: 1px solid var(--border);
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(24px);
  }
}

@keyframes scan {
  0% {
    top: 8%;
  }
  50% {
    top: 92%;
  }
  100% {
    top: 8%;
  }
}

@keyframes blink {
  to {
    visibility: hidden;
  }
}

@media (max-width: 900px) {
  .login {
    grid-template-columns: 1fr;
  }
  .poster {
    min-height: 300px;
    padding: 24px 16px;
  }
  .wordmark {
    font-size: clamp(72px, 26vw, 140px);
  }
  .tagline {
    right: 16px;
    bottom: 56px;
    font-size: 15px;
  }
}
</style>
