export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do {
    n /= 1024
    i++
  } while (n >= 1024 && i < units.length - 1)
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`
}

export function formatDate(sec: number): string {
  if (!sec) return ''
  return new Date(sec * 1000).toLocaleString('de-DE', {dateStyle: 'short', timeStyle: 'short'})
}

/** Joins a directory and a name using the separator the directory already uses. */
export function joinPath(base: string, name: string): string {
  const sep = base.includes('\\') && !base.includes('/') ? '\\' : '/'
  return base.endsWith(sep) ? base + name : base + sep + name
}

export function baseName(p: string): string {
  const parts = p.split(/[\\/]/).filter(Boolean)
  return parts[parts.length - 1] ?? p
}

/** Returns an error message for an invalid file name, or null. */
export function validateName(name: string): string | null {
  if (!name.trim()) return 'Der Name darf nicht leer sein'
  if (/[\\/]/.test(name)) return 'Der Name darf kein „/“ oder „\\“ enthalten'
  if (name === '.' || name === '..') return 'Ungültiger Name'
  return null
}

export function octal(perm: number): string {
  return (perm & 0o7777).toString(8)
}
