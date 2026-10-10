export const THEME_PREFERENCE_KEY = 'bolt-dock:theme:v1'

const THEME_MODES = new Set(['light', 'dark', 'system'])

export function readThemePreference(storage) {
  try {
    const value = JSON.parse((storage || globalThis.localStorage).getItem(THEME_PREFERENCE_KEY) || 'null')
    return value?.version === 1 && THEME_MODES.has(value.mode) ? value.mode : 'system'
  } catch { return 'system' }
}

export function writeThemePreference(mode, storage) {
  const value = THEME_MODES.has(mode) ? mode : 'system'
  try { (storage || globalThis.localStorage).setItem(THEME_PREFERENCE_KEY, JSON.stringify({ version: 1, mode: value })) } catch {}
}
