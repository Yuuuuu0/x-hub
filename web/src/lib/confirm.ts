// 带错误处理的确认弹窗。
import { Modal, message } from 'ant-design-vue'
import type { ModalFuncProps } from 'ant-design-vue'

/**
 * 弹出确认框，确认后执行 action；action 失败时关闭弹窗并用 message.error 显示错误信息
 * （如后端 409 返回的冲突说明），避免异常被吞掉、用户看不到原因。
 * @param props Modal.confirm 的配置（不含 onOk）
 * @param action 确认后执行的异步操作，执行期间确认按钮显示加载态
 */
export function confirmAction(props: Omit<ModalFuncProps, 'onOk'>, action: () => Promise<unknown>) {
  return Modal.confirm({
    cancelText: '取消',
    ...props,
    async onOk() {
      try {
        await action()
      } catch (e: any) {
        message.error(e?.message ?? '操作失败')
      }
    },
  })
}
