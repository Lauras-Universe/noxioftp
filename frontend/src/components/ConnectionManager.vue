<script setup lang="ts">
import {computed, onMounted, reactive, ref, watch} from 'vue'
import {App} from '../api'
import {defaultPort, PROTOCOLS} from '../format'
import {useConnection} from '../stores/connection'
import {useUi} from '../stores/ui'
import type {connections} from '../../wailsjs/go/models'
import Icon from './Icon.vue'

const emit = defineEmits<{close: []}>()
const conn = useConnection()
const ui = useUi()

const blank = (): connections.Profile => ({id: '', name: '', protocol: 'sftp', jumpId: '', host: '', port: 22, user: '', authType: 'password', keyPath: '', remoteDir: '', hasSecret: false})
const form = reactive<connections.Profile>(blank())
const secret = ref('')
const saving = ref(false)

const isNew = computed(() => !form.id)
const isSftp = computed(() => form.protocol === 'sftp')
const jumpCandidates = computed(() => conn.profiles.filter((p) => p.protocol === 'sftp' && p.id !== form.id))

// switching protocol: move the port along if it was still the old default, FTP has no key login / jump host
watch(
  () => form.protocol,
  (now, before) => {
    if (form.port === defaultPort(before)) form.port = defaultPort(now)
    if (now !== 'sftp') {
      form.authType = 'password'
      form.jumpId = ''
    }
  },
)

function select(p: connections.Profile) {
  Object.assign(form, p)
  secret.value = ''
}
function newProfile() {
  Object.assign(form, blank())
  secret.value = ''
}

onMounted(() => {
  if (conn.active) select(conn.active)
  else if (conn.profiles.length) select(conn.profiles[0])
})

async function pickKey() {
  try {
    const p = await App.PickKeyFile()
    if (p) form.keyPath = p
  } catch (e) {
    ui.error(e)
  }
}

async function save(): Promise<connections.Profile | null> {
  saving.value = true
  try {
    const saved = await App.SaveProfile({...form, port: Number(form.port) || 22}, secret.value)
    await conn.load()
    select(saved)
    return saved
  } catch (e) {
    ui.error(e)
    return null
  } finally {
    saving.value = false
  }
}

async function saveAndConnect() {
  const saved = await save()
  if (!saved) return
  emit('close')
  await conn.connect(saved.id)
}

async function remove() {
  if (!form.id) return
  const ok = await ui.confirm('Profil löschen?', `„${form.name}“ und das gespeicherte Passwort entfernen?`, {okLabel: 'Löschen', danger: true})
  if (!ok) return
  try {
    await App.DeleteProfile(form.id)
    await conn.load()
    if (conn.profiles.length) select(conn.profiles[0])
    else newProfile()
  } catch (e) {
    ui.error(e)
  }
}
</script>

<template>
  <div class="overlay" @mousedown.self="emit('close')" @keydown.esc="emit('close')">
    <div class="modal manager" role="dialog" aria-modal="true" aria-label="Verbindungsmanager">
      <aside class="list">
        <header>
          <strong>Verbindungen</strong>
          <button class="icon-btn" title="Neues Profil" @click="newProfile"><Icon name="plus" /></button>
        </header>
        <ul>
          <li v-for="p in conn.profiles" :key="p.id" :class="{active: p.id === form.id}" @click="select(p)" @dblclick="emit('close'); conn.connect(p.id)">
            <span class="n">{{ p.name }}</span>
            <span class="muted">{{ p.user }}@{{ p.host }}</span>
          </li>
          <li v-if="!conn.profiles.length" class="muted empty">Noch keine Profile</li>
        </ul>
      </aside>

      <form class="edit" @submit.prevent="saveAndConnect">
        <h2>{{ isNew ? 'Neues Profil' : form.name }}</h2>

        <label>Name<input v-model="form.name" placeholder="z. B. Webserver" /></label>
        <label>
          Protokoll
          <select v-model="form.protocol">
            <option v-for="p in PROTOCOLS" :key="p.value" :value="p.value">{{ p.label }}</option>
          </select>
        </label>
        <p v-if="form.protocol === 'ftp'" class="hint warn">Unverschlüsselt: Passwort und Dateien werden im Klartext übertragen. Nur in vertrauenswürdigen Netzen verwenden.</p>
        <div class="row">
          <label class="grow">Host<input v-model="form.host" required placeholder="example.org" spellcheck="false" /></label>
          <label class="port">Port<input v-model.number="form.port" type="number" min="1" max="65535" /></label>
        </div>
        <label>Benutzer<input v-model="form.user" required autocomplete="off" spellcheck="false" /></label>

        <fieldset v-if="isSftp">
          <legend>Anmeldung</legend>
          <label class="inline"><input v-model="form.authType" type="radio" value="password" /> Passwort</label>
          <label class="inline"><input v-model="form.authType" type="radio" value="key" /> SSH-Schlüssel</label>
        </fieldset>

        <template v-if="form.authType === 'password'">
          <label>
            Passwort
            <input v-model="secret" type="password" autocomplete="new-password" :placeholder="form.hasSecret ? '••••••• (gespeichert – leer lassen zum Beibehalten)' : 'Leer = bei jedem Verbinden fragen'" />
          </label>
        </template>
        <template v-else>
          <label>
            Schlüsseldatei
            <span class="row">
              <input v-model="form.keyPath" class="grow" placeholder="~/.ssh/id_ed25519" spellcheck="false" />
              <button type="button" @click="pickKey">Durchsuchen …</button>
            </span>
          </label>
          <label>
            Passphrase (optional)
            <input v-model="secret" type="password" autocomplete="new-password" :placeholder="form.hasSecret ? '••••••• (gespeichert)' : 'Leer = nur falls nötig fragen'" />
          </label>
        </template>
        <p class="hint"><Icon name="lock" :size="12" /> Passwörter werden im Schlüsselbund des Betriebssystems gespeichert, nie in einer Datei.</p>

        <label v-if="isSftp">
          Über Jump-Host verbinden (optional)
          <select v-model="form.jumpId">
            <option value="">Direkt verbinden</option>
            <option v-for="p in jumpCandidates" :key="p.id" :value="p.id">{{ p.name }} ({{ p.user }}@{{ p.host }})</option>
          </select>
        </label>
        <p v-if="isSftp && form.jumpId" class="hint">Das Passwort bzw. die Passphrase des Jump-Hosts muss in dessen Profil gespeichert sein.</p>

        <label>Startverzeichnis (optional)<input v-model="form.remoteDir" placeholder="/var/www" spellcheck="false" /></label>

        <div class="buttons">
          <button v-if="!isNew" type="button" class="danger-text" @click="remove">Löschen</button>
          <span class="spacer" />
          <button type="button" @click="emit('close')">Schließen</button>
          <button type="button" :disabled="saving" @click="save">Speichern</button>
          <button type="submit" class="primary" :disabled="saving">Speichern &amp; Verbinden</button>
        </div>
      </form>
    </div>
  </div>
</template>
