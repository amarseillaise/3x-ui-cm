export function isIOS(): boolean {
  const ua = navigator.userAgent
  return /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

/** True when the PWA runs from the home screen (required for Web Push on iOS). */
export function isStandalone(): boolean {
  return window.matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true
}

/** How this browser installs a web app; each value has its own instruction text. */
export type InstallPlatform = 'ios' | 'android' | 'chromium' | 'safariMac'

/**
 * Where installing the cabinet as an app is possible, and how. null means it
 * is already installed or this browser cannot install web apps (desktop
 * Firefox, Safari before 17), so no instruction should be shown.
 */
export function installPlatform(): InstallPlatform | null {
  if (isStandalone()) return null
  const ua = navigator.userAgent
  if (isIOS()) return 'ios' // every iOS browser installs through Share since iOS 16.4
  if (/Android/i.test(ua)) return 'android'
  if (/Firefox\//.test(ua)) return null
  if (/Chrome\/|Chromium\/|Edg\//.test(ua)) return 'chromium'
  const safari = /Version\/(\d+)[\d.]* Safari\//.exec(ua)
  if (/Macintosh/.test(ua) && safari && Number(safari[1]) >= 17) return 'safariMac'
  return null
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  }
}
