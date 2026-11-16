import {defineStore} from 'pinia'
import {reactive, ref} from 'vue'
import {App, errCode, errMsg} from '../api'
import {baseName} from '../format'
import {languageFor, monaco} from '../monaco'
import {useConnection} from './connection'
import {usePanes} from './panes'
import {useUi} from './ui'

export interface Tab {
  key: number
  path: string
  name: string
  profileId: string
  profileName: string
  modTime: number
  dirty: boolean
  saving: boolean
  savedVersion: number
}

let nextKey = 1
const models = new Map<number, monaco.editor.ITextModel>() // Monaco models are not reactive

export const useEditor = defineStore('editor', () => {
  const ui = useUi()
  const conn = useConnection()
  const panes = usePanes()

  const tabs = ref<Tab[]>([])
  /** 0 = file view, otherwise the key of the active editor tab. */
  const current = ref(0)

  const modelFor = (key: number) => models.get(key) ?? null
  const currentTab = () => tabs.value.find((t) => t.key === current.value)

  async function open(path: string) {
    if (!conn.connected || !conn.active) {
      ui.toast('Nicht verbunden', 'error')
      return
    }
    const profileId = conn.active.id
    const existing = tabs.value.find((t) => t.path === path && t.profileId === profileId)
    if (existing) {
      current.value = existing.key
      return
    }
    try {
      const f = await App.RemoteReadText(path)
      const model = monaco.editor.createModel(f.content, languageFor(baseName(path)))
      const tab = reactive<Tab>({
        key: nextKey++,
        path,
        name: baseName(path),
        profileId,
        profileName: conn.active.name,
        modTime: f.modTime,
        dirty: false,
        saving: false,
        savedVersion: model.getAlternativeVersionId(),
      })
      model.onDidChangeContent(() => {
        tab.dirty = model.getAlternativeVersionId() !== tab.savedVersion
      })
      models.set(tab.key, model)
      tabs.value.push(tab)
      current.value = tab.key
    } catch (e) {
      ui.toast(`„${baseName(path)}“ kann nicht geöffnet werden: ${errMsg(e)}`, 'error', 8000)
    }
  }

  async function save(tab: Tab | undefined = currentTab(), force = false) {
    const model = tab && models.get(tab.key)
    if (!tab || !model || tab.saving) return
    if (!conn.connected || conn.active?.id !== tab.profileId) {
      ui.toast(`Zum Speichern bitte mit „${tab.profileName}“ verbinden`, 'error')
      return
    }
    tab.saving = true
    try {
      const version = model.getAlternativeVersionId()
      tab.modTime = await App.RemoteWriteText(tab.path, model.getValue(), tab.modTime, force)
      tab.savedVersion = version
      tab.dirty = model.getAlternativeVersionId() !== version
      panes.refresh('remote') // size / date changed
    } catch (e) {
      if (errCode(e) === 'E_MODIFIED') {
        tab.saving = false
        const ok = await ui.confirm('Datei auf dem Server geändert', `„${tab.name}“ wurde auf dem Server seit dem Öffnen verändert. Trotzdem überschreiben?`, {
          okLabel: 'Überschreiben',
          danger: true,
        })
        if (ok) await save(tab, true)
        return
      }
      ui.toast(`Speichern fehlgeschlagen: ${errMsg(e)}`, 'error', 8000)
    } finally {
      tab.saving = false
    }
  }

  async function close(key: number) {
    const tab = tabs.value.find((t) => t.key === key)
    if (!tab) return
    if (tab.dirty && !(await ui.confirm('Ungespeicherte Änderungen', `„${tab.name}“ hat ungespeicherte Änderungen. Verwerfen?`, {okLabel: 'Verwerfen', danger: true}))) {
      return
    }
    const i = tabs.value.indexOf(tab)
    models.get(key)?.dispose()
    models.delete(key)
    tabs.value.splice(i, 1)
    if (current.value === key) current.value = tabs.value[Math.min(i, tabs.value.length - 1)]?.key ?? 0
  }

  /** Keeps open tabs pointing at the right file after a rename on the server. */
  function renamed(from: string, to: string) {
    for (const t of tabs.value) {
      if (t.profileId !== conn.active?.id) continue
      if (t.path === from || t.path.startsWith(from + '/')) {
        t.path = to + t.path.slice(from.length)
        t.name = baseName(t.path)
      }
    }
  }

  const hasDirty = () => tabs.value.some((t) => t.dirty)

  return {tabs, current, modelFor, open, save, close, renamed, hasDirty}
})
