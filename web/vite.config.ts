import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把 /api 代理到本地主控：XHUB_DEV_TARGET=https://127.0.0.1:2096/<面板路径>
const target = process.env.XHUB_DEV_TARGET ? new URL(process.env.XHUB_DEV_TARGET) : null
// 演示环境构建（VITE_DEMO=1）：输出到 dist-demo，去掉由主控改写的 <base>，页面按自身所在路径加载资源
const demo = !!process.env.VITE_DEMO

export default defineConfig({
  base: './',
  plugins: [
    vue(),
    demo && { name: 'demo-base', transformIndexHtml: (html: string) => html.replace(/\s*<base href="\/">/, '') },
  ],
  build: { outDir: demo ? 'dist-demo' : 'dist', emptyOutDir: true, chunkSizeWarningLimit: 1500 },
  server: target
    ? {
        proxy: {
          '/api': {
            target: target.origin,
            secure: false,
            changeOrigin: true,
            rewrite: (p) => target.pathname.replace(/\/$/, '') + p,
          },
        },
      }
    : undefined,
})
