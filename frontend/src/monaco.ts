// Monaco is imported piece by piece instead of via `import 'monaco-editor'`:
// the full bundle (all editor features, the TypeScript language service, LSP client)
// is ~4 MB and needs several GB of RAM to build. This keeps what a remote file
// editor needs: core editor, find/replace, folding, multi-cursor, comments, suggestions
// and syntax highlighting for common server-side file types.
import * as monaco from 'monaco-editor/editor/editor.api'

// editor features
import 'monaco-editor/editor/browser/coreCommands'
import 'monaco-editor/editor/browser/widget/codeEditor/codeEditorWidget'
import '../node_modules/monaco-editor/esm/vs/base/browser/ui/codicons/codicon/codicon.css' // not covered by the package exports map
import '../node_modules/monaco-editor/esm/vs/base/browser/ui/codicons/codicon/codicon-modifiers.css'
import 'monaco-editor/editor/common/standaloneStrings'
import 'monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching'
import 'monaco-editor/editor/contrib/caretOperations/browser/caretOperations'
import 'monaco-editor/editor/contrib/caretOperations/browser/transpose'
import 'monaco-editor/editor/contrib/clipboard/browser/clipboard'
import 'monaco-editor/editor/contrib/comment/browser/comment'
import 'monaco-editor/editor/contrib/contextmenu/browser/contextmenu'
import 'monaco-editor/editor/contrib/cursorUndo/browser/cursorUndo'
import 'monaco-editor/editor/contrib/dnd/browser/dnd'
import 'monaco-editor/editor/contrib/find/browser/findController'
import 'monaco-editor/editor/contrib/folding/browser/folding'
import 'monaco-editor/editor/contrib/fontZoom/browser/fontZoom'
import 'monaco-editor/editor/contrib/hover/browser/hoverContribution'
import 'monaco-editor/editor/contrib/indentation/browser/indentation'
import 'monaco-editor/editor/contrib/insertFinalNewLine/browser/insertFinalNewLine'
import 'monaco-editor/editor/contrib/lineSelection/browser/lineSelection'
import 'monaco-editor/editor/contrib/linesOperations/browser/linesOperations'
import 'monaco-editor/editor/contrib/linkedEditing/browser/linkedEditing'
import 'monaco-editor/editor/contrib/links/browser/links'
import 'monaco-editor/editor/contrib/multicursor/browser/multicursor'
import 'monaco-editor/editor/contrib/readOnlyMessage/browser/contribution'
import 'monaco-editor/editor/contrib/smartSelect/browser/smartSelect'
import 'monaco-editor/editor/contrib/snippet/browser/snippetController2'
import 'monaco-editor/editor/contrib/stickyScroll/browser/stickyScrollContribution'
import 'monaco-editor/editor/contrib/suggest/browser/suggestController'
import 'monaco-editor/editor/contrib/tokenization/browser/tokenization'
import 'monaco-editor/editor/contrib/unicodeHighlighter/browser/unicodeHighlighter'
import 'monaco-editor/editor/contrib/unusualLineTerminators/browser/unusualLineTerminators'
import 'monaco-editor/editor/contrib/wordHighlighter/browser/wordHighlighter'
import 'monaco-editor/editor/contrib/wordOperations/browser/wordOperations'
import 'monaco-editor/editor/contrib/wordPartOperations/browser/wordPartOperations'
import 'monaco-editor/editor/standalone/browser/quickAccess/standaloneCommandsQuickAccess'
import 'monaco-editor/editor/standalone/browser/quickAccess/standaloneGotoLineQuickAccess'

// language services (validation / completion for these; no TypeScript service, it is huge)
import 'monaco-editor/languages/features/css/register'
import 'monaco-editor/languages/features/html/register'
import 'monaco-editor/languages/features/json/register'

// syntax highlighting (each language is loaded lazily when a file of that type is opened)
import 'monaco-editor/languages/definitions/bat/register'
import 'monaco-editor/languages/definitions/cpp/register'
import 'monaco-editor/languages/definitions/csharp/register'
import 'monaco-editor/languages/definitions/css/register'
import 'monaco-editor/languages/definitions/dart/register'
import 'monaco-editor/languages/definitions/dockerfile/register'
import 'monaco-editor/languages/definitions/go/register'
import 'monaco-editor/languages/definitions/graphql/register'
import 'monaco-editor/languages/definitions/handlebars/register'
import 'monaco-editor/languages/definitions/hcl/register'
import 'monaco-editor/languages/definitions/html/register'
import 'monaco-editor/languages/definitions/ini/register'
import 'monaco-editor/languages/definitions/java/register'
import 'monaco-editor/languages/definitions/javascript/register'
import 'monaco-editor/languages/definitions/kotlin/register'
import 'monaco-editor/languages/definitions/less/register'
import 'monaco-editor/languages/definitions/lua/register'
import 'monaco-editor/languages/definitions/markdown/register'
import 'monaco-editor/languages/definitions/perl/register'
import 'monaco-editor/languages/definitions/php/register'
import 'monaco-editor/languages/definitions/powershell/register'
import 'monaco-editor/languages/definitions/protobuf/register'
import 'monaco-editor/languages/definitions/python/register'
import 'monaco-editor/languages/definitions/restructuredtext/register'
import 'monaco-editor/languages/definitions/ruby/register'
import 'monaco-editor/languages/definitions/rust/register'
import 'monaco-editor/languages/definitions/scss/register'
import 'monaco-editor/languages/definitions/shell/register'
import 'monaco-editor/languages/definitions/sql/register'
import 'monaco-editor/languages/definitions/swift/register'
import 'monaco-editor/languages/definitions/twig/register'
import 'monaco-editor/languages/definitions/typescript/register'
import 'monaco-editor/languages/definitions/xml/register'
import 'monaco-editor/languages/definitions/yaml/register'
import editorWorker from 'monaco-editor/editor/editor.worker?worker'
import jsonWorker from 'monaco-editor/language/json/json.worker?worker'
import cssWorker from 'monaco-editor/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/language/html/html.worker?worker'

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
