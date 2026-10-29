<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { theme } from 'ant-design-vue'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
import { isDark } from './lib/theme'

watchEffect(() => {
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light'
})

// 浅色主题以墨色为主色；深色主题以青柠为主色，按钮文字反转为墨色。
const themeConfig = computed(() => ({
  algorithm: isDark.value ? theme.darkAlgorithm : theme.defaultAlgorithm,
  token: {
    colorPrimary: isDark.value ? '#c6ff3d' : '#0b0d12',
    colorTextLightSolid: isDark.value ? '#0b0d12' : '#ffffff',
    colorLink: isDark.value ? '#c6ff3d' : '#0b0d12',
    colorSuccess: isDark.value ? '#4ade80' : '#17a34a',
    colorError: isDark.value ? '#ff6b6b' : '#e5484d',
    colorBgContainer: isDark.value ? '#111318' : '#ffffff',
    colorBgElevated: isDark.value ? '#161920' : '#ffffff',
    colorBorder: isDark.value ? '#2f3440' : '#d9d6cc',
    colorBorderSecondary: isDark.value ? '#22262f' : '#e8e6df',
    borderRadius: 10,
    fontFamily: "'Space Grotesk Variable', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', system-ui, sans-serif",
    fontFamilyCode: "'JetBrains Mono Variable', ui-monospace, Menlo, monospace",
    controlHeight: 36,
  },
}))
</script>

<template>
  <a-config-provider :theme="themeConfig" :locale="zhCN">
    <router-view />
  </a-config-provider>
</template>
