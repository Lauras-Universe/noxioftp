<script setup lang="ts">
import {computed} from 'vue'
import {BrowserOpenURL} from '../../wailsjs/runtime/runtime'
import {formatSize} from '../format'
import {useUpdate} from '../stores/update'
import Icon from './Icon.vue'

const up = useUpdate()

const info = computed(() => up.info!)
const percent = computed(() => (up.progress.total > 0 ? Math.min(100, Math.round((up.progress.done / up.progress.total) * 100)) : 0))

function openPage() {
  const url = info.value.pageUrl
  if (url.startsWith('https://')) BrowserOpenURL(url)
}
function close() {
  if (!up.applying) up.open = false
}
</script>

<template>
  <div v-if="up.open && up.info" class="overlay" @mousedown.self="close" @keydown.esc="close">
    <div class="modal update" role="dialog" aria-modal="true" aria-label="Update verfügbar">
      <h2><Icon name="download" :size="18" /> Update verfügbar</h2>
      <p class="versions">
        Version <b>{{ info.latest }}</b> ist erschienen, du nutzt <b>{{ info.current }}</b>.
      </p>
      <pre v-if="info.notes" class="notes">{{ info.notes }}</pre>

      <!-- program can replace itself -->
      <template v-if="info.method === 'self'">
        <p v-if="!up.applying" class="msg">Das Programm lädt die neue Version von GitHub, prüft sie, ersetzt sich selbst und startet neu.</p>
        <div v-else class="progress">
          <div class="bar" :class="{indeterminate: up.restarting || !up.progress.total}"><div :style="{width: percent + '%'}" /></div>
          <span class="muted">
            <template v-if="up.restarting">Wird neu gestartet …</template>
            <template v-else>Lade {{ formatSize(up.progress.done) }}<template v-if="up.progress.total"> von {{ formatSize(up.progress.total) }} ({{ percent }} %)</template></template>
          </span>
        </div>
        <div class="buttons">
          <button :disabled="up.applying" @click="close">Später</button>
          <button :disabled="up.applying" @click="up.skip()">Diese Version überspringen</button>
          <button class="primary" :disabled="up.applying" @click="up.apply()">Jetzt aktualisieren</button>
        </div>
      </template>

      <!-- installed with the install script, or cannot replace itself: run the command again -->
      <template v-else>
        <p class="msg">
          <template v-if="info.method === 'script'">Du hast das Programm über das Terminal installiert. Führe zum Aktualisieren denselben Befehl noch einmal aus:</template>
          <template v-else>Das Programm kann sich hier nicht selbst aktualisieren<template v-if="info.reason"> ({{ info.reason }})</template>. Installiere die neue Version, indem du diesen Befehl im Terminal ausführst:</template>
        </p>
        <div class="command">
          <code>{{ info.command }}</code>
          <button class="icon-btn" title="Befehl kopieren" aria-label="Befehl kopieren" @click="up.copyCommand()"><Icon name="link" /></button>
        </div>
        <p class="hint"><Icon name="lock" :size="12" /> Danach das Programm neu starten. Alternativ die Datei von der Release-Seite laden (der Browser-Download löst die Sicherheitswarnung von macOS bzw. Windows aus).</p>
        <div class="buttons">
          <button @click="openPage">Release-Seite öffnen</button>
          <span class="spacer" />
          <button @click="up.skip()">Diese Version überspringen</button>
          <button class="primary" @click="close">Schließen</button>
        </div>
      </template>
    </div>
  </div>
</template>
