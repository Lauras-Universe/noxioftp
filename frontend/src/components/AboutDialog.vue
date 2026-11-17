<script setup lang="ts">
import {BrowserOpenURL} from '../../wailsjs/runtime/runtime'
import logo from '../assets/logo.png'
import {useUpdate} from '../stores/update'

const emit = defineEmits<{close: []}>()
const up = useUpdate()

const REPO = 'https://github.com/Lauras-Universe/noxioftp'
const open = (url: string) => BrowserOpenURL(url)
</script>

<template>
  <div class="overlay" @mousedown.self="emit('close')" @keydown.esc="emit('close')">
    <div class="modal about" role="dialog" aria-modal="true" aria-label="Über Noxio FTP">
      <img :src="logo" alt="" width="84" height="84" />
      <h2>Noxio FTP</h2>
      <p class="muted">Version {{ up.version }}</p>
      <p class="copy">© 2026 Lauras-Universe</p>
      <p class="legal">
        Dieses Programm kommt <b>ohne jede Garantie</b>. Es ist freie Software und darf unter den Bedingungen der
        GNU General Public License, Version 3, weitergegeben werden. Name und Logo sind davon ausgenommen (siehe NOTICE).
      </p>
      <p class="legal">
        Es besteht kein Anspruch auf Weiterentwicklung, Support oder Updates, und es wird keine fehlerfreie bzw. vollständige
        Funktion zugesichert. Nutzung auf eigene Verantwortung, wichtige Daten bitte sichern.
      </p>
      <div class="links">
        <button class="ghost" @click="open('https://www.gnu.org/licenses/gpl-3.0.html')">Lizenztext (GPL-3.0)</button>
        <button class="ghost" @click="open(REPO + '/blob/main/NOTICE.md')">Hinweise (NOTICE)</button>
        <button class="ghost" @click="open(REPO)">Quellcode</button>
      </div>
      <div class="buttons"><button class="primary" @click="emit('close')">Schließen</button></div>
    </div>
  </div>
</template>
