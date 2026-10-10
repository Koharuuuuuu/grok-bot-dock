export const READER_PREFERENCES_KEY = 'bolt-dock:reader-preferences:v1'
export const READER_FONT_KEY = 'bolt-dock:reader-font-size:v1'

export function readImmersivePreference(storage) {
  try {
    const value = JSON.parse((storage || globalThis.localStorage).getItem(READER_PREFERENCES_KEY) || '{}')
    return value.version === 1 && value.immersive === true
  } catch { return false }
}

export function writeImmersivePreference(immersive, storage) {
  try { (storage || globalThis.localStorage).setItem(READER_PREFERENCES_KEY, JSON.stringify({ version: 1, immersive: Boolean(immersive) })) } catch {}
}

export function readReaderFontSize(storage) {
  try {
    const value = JSON.parse((storage || globalThis.localStorage).getItem(READER_FONT_KEY) || 'null')
    return value?.version === 1 && Number.isInteger(value.px) && value.px >= 13 && value.px <= 24 ? value.px : 16
  } catch { return 16 }
}

export function writeReaderFontSize(px, storage) {
  const size = Number.isInteger(px) && px >= 13 && px <= 24 ? px : 16
  try { (storage || globalThis.localStorage).setItem(READER_FONT_KEY, JSON.stringify({ version: 1, px: size })) } catch {}
}
