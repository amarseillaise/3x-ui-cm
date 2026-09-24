import { isApiError } from './api'
import { t } from './i18n/ru'

/**
 * The single place that turns a thrown error into something a person can act
 * on. Timeouts and dropped connections need their own wording: "что-то пошло
 * не так" tells nobody to check their connection and try again.
 */
export function errorText(err: unknown): string {
  if (!isApiError(err)) return t.errorGeneric
  if (err.status === 502) return t.panelUnavailable
  switch (err.code) {
    case 'timeout':
      return t.errorTimeout
    case 'network':
    case 'aborted':
      return t.errorNetwork
  }
  return err.message || t.errorGeneric
}
