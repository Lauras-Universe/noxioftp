<script setup lang="ts">
const props = defineProps<{name: string; size?: number}>()

const paths: Record<string, string> = {
  folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  file: 'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z M14 3v5h5',
  filePlus: 'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z M14 3v5h5 M12 11v6 M9 14h6',
  folderPlus: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z M12 10v6 M9 13h6',
  up: 'M12 19V5 M5 12l7-7 7 7',
  refresh: 'M21 12a9 9 0 1 1-3-6.7 M21 4v5h-5',
  home: 'M3 11l9-8 9 8 M5 10v10h14V10',
  plus: 'M12 5v14 M5 12h14',
  close: 'M6 6l12 12 M18 6L6 18',
  upload: 'M12 16V4 M6 10l6-6 6 6 M4 20h16',
  download: 'M12 4v12 M6 10l6 6 6-6 M4 20h16',
  edit: 'M4 20h4l10-10-4-4L4 16z M13 7l4 4',
  trash: 'M4 7h16 M9 7V4h6v3 M6 7l1 13h10l1-13',
  link: 'M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1 M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1',
  plug: 'M9 2v6 M15 2v6 M6 8h12v4a6 6 0 0 1-12 0z M12 18v4',
  save: 'M5 3h11l4 4v14H5z M8 3v5h7V3 M8 21v-7h8v7',
  chevron: 'M6 9l6 6 6-6',
  pencil: 'M4 20h4l10-10-4-4L4 16z',
  lock: 'M6 11h12v10H6z M8 11V7a4 4 0 0 1 8 0v4',
}
</script>

<template>
  <svg
    class="icon"
    :width="props.size ?? 16"
    :height="props.size ?? 16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[props.name] ?? ''" />
  </svg>
</template>
