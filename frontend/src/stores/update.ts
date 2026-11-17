import {defineStore} from 'pinia'
import {ref} from 'vue'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {App, errMsg} from '../api'
import type {update} from '../../wailsjs/go/models'
import {useUi} from './ui'

const SKIPPED_KEY = 'noxioftp.skippedVersion'

function skippedVersion(): string {
  try {
    return localStorage.getItem(SKIPPED_KEY) ?? ''
  } catch {
    return ''
  }
}

export const useUpdate = defineStore('update', () => {
  const ui = useUi()

  const version = ref('')
  const info = ref<update.Info | null>(null)
  const open = ref(false)
  const checking = ref(false)
  const applying = ref(false)
  const restarting = ref(false)
  const progress = ref({done: 0, total: 0})

  /** Reads the version and, a few seconds after start, checks for a new release. */
  async function init() {
    version.value = await App.AppVersion()
    EventsOn('update:progress', (p: {done: number; total: number}) => (progress.value = p))
    setTimeout(() => void check(true), 3000)
  }

  /** auto = the silent check at start: no messages, and a skipped version stays quiet. */
  async function check(auto = false) {
    if (checking.value || applying.value) return
    checking.value = true
    try {
      const i = await App.CheckForUpdate(auto)
      info.value = i
      if (i.available) {
        if (!(auto && skippedVersion() === i.latest)) open.value = true
      } else if (!auto) {
        ui.toast(`Du nutzt bereits die neueste Version (${i.current}).`, 'success')
      }
    } catch (e) {
      if (!auto) ui.toast(`Update-Prüfung fehlgeschlagen: ${errMsg(e)}`, 'error', 8000)
    } finally {
      checking.value = false
    }
  }

  async function apply() {
    applying.value = true
    progress.value = {done: 0, total: 0}
    try {
      await App.ApplyUpdate()
      restarting.value = true // the program quits and the new version starts
    } catch (e) {
      applying.value = false
      ui.toast(errMsg(e), 'error', 10000)
    }
  }

  function skip() {
    try {
      if (info.value) localStorage.setItem(SKIPPED_KEY, info.value.latest)
    } catch {
      /* private mode: the dialog just shows again next time */
    }
    open.value = false
  }

  async function copyCommand() {
    try {
      await navigator.clipboard.writeText(info.value?.command ?? '')
      ui.toast('Befehl kopiert', 'success', 2000)
    } catch {
      ui.toast('Zwischenablage nicht verfügbar – bitte den Befehl von Hand markieren', 'error')
    }
  }

  return {version, info, open, checking, applying, restarting, progress, init, check, apply, skip, copyCommand}
})
