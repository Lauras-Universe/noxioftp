import {defineStore} from 'pinia'
import {ref} from 'vue'
import {App, errCode, errMsg} from '../api'
import type {connections} from '../../wailsjs/go/models'
import {useUi} from './ui'
import {usePanes} from './panes'

export const useConnection = defineStore('connection', () => {
  const ui = useUi()
  const panes = usePanes()

  const profiles = ref<connections.Profile[]>([])
  const active = ref<connections.Profile | null>(null)
  const connected = ref(false)
  const lost = ref(false)
  const busy = ref(false)

  async function load() {
    profiles.value = (await App.ListProfiles()) ?? []
  }

  /** Connects to a saved profile, asking for a password / passphrase if needed. */
  async function connect(id: string) {
    const p = profiles.value.find((x) => x.id === id)
    if (!p || busy.value) return
    let secret = ''
    if (p.authType === 'password' && !p.hasSecret) {
      const pw = await ui.prompt(`Passwort für ${p.user}@${p.host}`, '', {secret: true, okLabel: 'Verbinden'})
      if (pw === null) return
      secret = pw
    }
    busy.value = true
    try {
      for (;;) {
        try {
          const res = await App.Connect(id, secret)
          if (connected.value && active.value?.id !== id) panes.clearRemote()
          active.value = p
          connected.value = true
          lost.value = false
          panes.go('remote', res.home)
          return
        } catch (e) {
          if (errCode(e) === 'E_PASSPHRASE') {
            const pw = await ui.prompt(`Passphrase für den Schlüssel von „${p.name}“`, '', {secret: true, okLabel: 'Verbinden'})
            if (pw === null) return
            secret = pw
            continue
          }
          ui.toast(`Verbindung fehlgeschlagen: ${errMsg(e)}`, 'error', 10000)
          return
        }
      }
    } finally {
      busy.value = false
    }
  }

  async function disconnect() {
    await App.Disconnect()
    connected.value = false
    lost.value = false
    panes.clearRemote()
  }

  function markLost() {
    if (connected.value) {
      lost.value = true
      ui.toast('Verbindung zum Server verloren. Beim nächsten Aktualisieren wird neu verbunden.', 'error', 8000)
    }
  }

  return {profiles, active, connected, lost, busy, load, connect, disconnect, markLost}
})
