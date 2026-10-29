<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState, Compartment } from '@codemirror/state'
import { yaml } from '@codemirror/lang-yaml'

// 代码编辑器（YAML），配色取自页面设计令牌，随明暗主题切换。
const props = defineProps<{ readonly?: boolean; minHeight?: string }>()
const model = defineModel<string>({ required: true })

const host = ref<HTMLElement>()
let view: EditorView | undefined
const editable = new Compartment()

const theme = EditorView.theme({
  '&': { fontSize: '12.5px', backgroundColor: 'var(--surface)', color: 'var(--text)', border: '1px solid var(--border)', borderRadius: '10px' },
  '&.cm-focused': { outline: 'none', borderColor: 'var(--border-strong)' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.65' },
  '.cm-content': { caretColor: 'var(--text)' },
  '.cm-gutters': { backgroundColor: 'var(--surface-2)', color: 'var(--text-3)', borderRight: '1px solid var(--border)', borderRadius: '10px 0 0 10px' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--text) 4%, transparent)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'color-mix(in srgb, var(--lime) 35%, transparent) !important' },
})

onMounted(() => {
  view = new EditorView({
    parent: host.value!,
    state: EditorState.create({
      doc: model.value,
      extensions: [
        basicSetup,
        yaml(),
        theme,
        editable.of([EditorState.readOnly.of(!!props.readonly), EditorView.editable.of(!props.readonly)]),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) model.value = u.state.doc.toString()
        }),
      ],
    }),
  })
})

watch(model, (v) => {
  if (view && v !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } })
  }
})

watch(
  () => props.readonly,
  (ro) => view?.dispatch({ effects: editable.reconfigure([EditorState.readOnly.of(!!ro), EditorView.editable.of(!ro)]) }),
)

onBeforeUnmount(() => view?.destroy())
</script>

<template>
  <div ref="host" class="code" :style="{ '--min': minHeight ?? '320px' }" />
</template>

<style scoped>
.code :deep(.cm-editor) {
  min-height: var(--min);
}
.code :deep(.cm-scroller) {
  min-height: var(--min);
}
</style>
