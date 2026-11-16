import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/editor/editor.worker?worker'
import jsonWorker from 'monaco-editor/language/json/json.worker?worker'
import cssWorker from 'monaco-editor/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/language/html/html.worker?worker'
import tsWorker from 'monaco-editor/language/typescript/ts.worker?worker'

;(self as any).MonacoEnvironment = {
  getWorker(_: string, label: string) {
    switch (label) {
      case 'json':
        return new jsonWorker()
      case 'css':
      case 'scss':
      case 'less':
        return new cssWorker()
      case 'html':
      case 'handlebars':
      case 'razor':
        return new htmlWorker()
      case 'typescript':
      case 'javascript':
        return new tsWorker()
      default:
        return new editorWorker()
    }
  },
}

let byExt: Map<string, string> | null = null

/** Monaco language id for a file name (by extension or well-known name). */
export function languageFor(name: string): string {
  if (!byExt) {
    byExt = new Map()
    for (const l of monaco.languages.getLanguages()) {
      for (const e of l.extensions ?? []) if (!byExt.has(e.toLowerCase())) byExt.set(e.toLowerCase(), l.id)
      for (const f of l.filenames ?? []) byExt.set(f.toLowerCase(), l.id)
    }
  }
  const lower = name.toLowerCase()
  const dot = lower.lastIndexOf('.')
  return byExt.get(lower) ?? (dot >= 0 ? byExt.get(lower.slice(dot)) : undefined) ?? 'plaintext'
}

export function monacoTheme(): string {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'vs-dark' : 'vs'
}

export {monaco}
