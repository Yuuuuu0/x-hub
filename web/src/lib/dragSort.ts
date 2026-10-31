import { computed, reactive, ref, type Ref } from 'vue'
import { message } from 'ant-design-vue'
import { api } from './api'

/**
 * 列表的拖动排序：按住手柄拖到目标位置松手，保存新顺序。
 * @param list 列表数据，松手时就地更新
 * @param path 保存顺序的接口
 * @param reload 保存结束后重新加载（失败时借此恢复）
 * @returns busy 为真时轮询不应覆盖列表；itemProps / gripProps 绑定到行与手柄；drop 绑定到容器
 */
export function useDragSort<T extends { id: number }>(list: Ref<T[]>, path: string, reload: () => unknown) {
  const drag = reactive({ armed: 0, from: 0, over: 0 })
  const saving = ref(false)
  const busy = computed(() => drag.from !== 0 || saving.value)

  const indexOf = (id: number) => list.value.findIndex((x) => x.id === id)
  const end = () => Object.assign(drag, { armed: 0, from: 0, over: 0 })

  function itemProps(id: number) {
    const moved = drag.from !== 0 && drag.over === id && drag.from !== id
    return {
      draggable: drag.armed === id,
      class: {
        'sort-lifted': drag.from === id,
        'sort-before': moved && indexOf(drag.from) > indexOf(id),
        'sort-after': moved && indexOf(drag.from) < indexOf(id),
      },
      onDragstart(e: DragEvent) {
        drag.from = id
        e.dataTransfer!.effectAllowed = 'move'
      },
      onDragenter() {
        if (drag.from) drag.over = id
      },
      onDragend: end,
    }
  }

  function gripProps(id: number) {
    return { class: 'sort-grip', title: '拖动调整顺序', onMousedown: () => (drag.armed = id), onMouseup: () => (drag.armed = 0) }
  }

  async function drop() {
    const { from, over } = drag
    end()
    if (!from || !over || from === over) return
    const next = [...list.value]
    const [item] = next.splice(indexOf(from), 1)
    next.splice(indexOf(over), 0, item)
    list.value = next
    saving.value = true
    try {
      await api.put(path, { ids: next.map((x) => x.id) })
    } catch (e: any) {
      message.error(e.message)
    } finally {
      saving.value = false
      reload()
    }
  }

  return { busy, itemProps, gripProps, drop }
}
