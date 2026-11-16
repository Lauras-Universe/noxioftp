<script setup lang="ts">
import {useUi} from '../stores/ui'

const ui = useUi()
</script>

<template>
  <div class="toasts" aria-live="polite">
    <div v-for="t in ui.toasts" :key="t.id" class="toast" :class="t.kind" role="status" @click="ui.dismiss(t.id)">
      {{ t.text }}
    </div>
  </div>
</template>
