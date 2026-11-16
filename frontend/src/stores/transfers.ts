import {defineStore} from 'pinia'
import {computed, ref} from 'vue'
import {App} from '../api'
import type {transfer} from '../../wailsjs/go/models'
import {baseName} from '../format'
import {useUi} from './ui'
import {usePanes} from './panes'

export const useTransfers = defineStore('transfers', () => {
  const ui = useUi()
  const panes = usePanes()
  const jobs = ref<transfer.Job[]>([])

  const active = computed(() => jobs.value.filter((j) => j.status === 'queued' || j.status === 'running').length)

  async function load() {
    jobs.value = (await App.ListTransfers()) ?? []
  }

  /** Called for every `transfer:update` event from the backend. */
  function onUpdate(j: transfer.Job) {
    const i = jobs.value.findIndex((x) => x.id === j.id)
    const wasFinished = i >= 0 && !['queued', 'running'].includes(jobs.value[i].status)
    if (i >= 0) jobs.value[i] = j
    else jobs.value.push(j)
    // A finished job changed the other side's directory: refresh it.
    if (!wasFinished && ['done', 'skipped'].includes(j.status)) {
      panes.refresh(j.direction === 'upload' ? 'remote' : 'local')
    }
  }

  /**
   * Queues files for transfer. Uploads go from the local to the remote pane and
   * downloads the other way, into dstDir (default: the other pane's directory).
   */
  async function send(direction: 'upload' | 'download', paths: string[], dstDir?: string) {
    if (!paths.length) return
    const dst = dstDir ?? panes.cur[direction === 'upload' ? 'remote' : 'local']
    if (!dst) {
      ui.toast('Kein Zielverzeichnis – bitte zuerst verbinden', 'error')
      return
    }
    try {
      let policy = 'overwrite'
      const clashes = (await App.CheckConflicts(direction, paths, dst)) ?? [] // Go sends null for an empty list
      if (clashes.length) {
        const list = clashes.slice(0, 5).join(', ') + (clashes.length > 5 ? ` … (+${clashes.length - 5})` : '')
        const choice = await ui.choose('Ziel existiert bereits', `Bereits vorhanden in ${dst}:\n${list}`, [
          {label: 'Überschreiben', value: 'overwrite', primary: true},
          {label: 'Überspringen', value: 'skip'},
          {label: 'Umbenennen', value: 'rename'},
          {label: 'Abbrechen', value: ''},
        ])
        if (!choice) return
        policy = choice
      }
      await App.EnqueueTransfers(direction, paths, dst, policy)
    } catch (e) {
      ui.error(e)
    }
  }

  async function cancel(id: string) {
    await App.CancelTransfer(id)
  }
  async function retry(id: string) {
    await App.RetryTransfer(id)
  }
  async function clearFinished() {
    await App.ClearFinishedTransfers()
    jobs.value = jobs.value.filter((j) => j.status === 'queued' || j.status === 'running')
  }

  return {jobs, active, load, onUpdate, send, cancel, retry, clearFinished, baseName}
})
