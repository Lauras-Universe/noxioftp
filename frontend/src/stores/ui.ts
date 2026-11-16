import {defineStore} from 'pinia'
import {ref} from 'vue'
import {errMsg} from '../api'

export interface Toast {
  id: number
  kind: 'error' | 'info' | 'success'
  text: string
}

export interface ChoiceOption {
  label: string
  value: string
  primary?: boolean
}

export type Dialog =
  | {kind: 'prompt'; title: string; message: string; value: string; secret: boolean; okLabel: string; resolve: (v: string | null) => void}
  | {kind: 'confirm'; title: string; message: string; okLabel: string; danger: boolean; resolve: (v: boolean) => void}
  | {kind: 'choice'; title: string; message: string; options: ChoiceOption[]; resolve: (v: string | null) => void}

let toastId = 0

export const useUi = defineStore('ui', () => {
  const toasts = ref<Toast[]>([])
  const dialog = ref<Dialog | null>(null)

  function toast(text: string, kind: Toast['kind'] = 'info', ms = 5000) {
    const id = ++toastId
    toasts.value.push({id, kind, text})
    setTimeout(() => dismiss(id), ms)
  }
  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }
  function error(e: unknown) {
    toast(errMsg(e), 'error', 8000)
  }

  /** Dismisses an open dialog as "cancelled" before showing a new one. */
  function supersede() {
    const d = dialog.value
    if (!d) return
    if (d.kind === 'confirm') d.resolve(false)
    else d.resolve(null)
    dialog.value = null
  }

  function prompt(title: string, value = '', opts: {message?: string; secret?: boolean; okLabel?: string} = {}) {
    supersede()
    return new Promise<string | null>((resolve) => {
      dialog.value = {kind: 'prompt', title, value, message: opts.message ?? '', secret: !!opts.secret, okLabel: opts.okLabel ?? 'OK', resolve}
    })
  }
  function confirm(title: string, message: string, opts: {okLabel?: string; danger?: boolean} = {}) {
    supersede()
    return new Promise<boolean>((resolve) => {
      dialog.value = {kind: 'confirm', title, message, okLabel: opts.okLabel ?? 'OK', danger: !!opts.danger, resolve}
    })
  }
  function choose(title: string, message: string, options: ChoiceOption[]) {
    supersede()
    return new Promise<string | null>((resolve) => {
      dialog.value = {kind: 'choice', title, message, options, resolve}
    })
  }

  return {toasts, dialog, toast, dismiss, error, prompt, confirm, choose}
})
