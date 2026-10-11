import { useContacts } from '../hooks/useContacts'
import { t } from '../i18n/ru'

/**
 * Contact links from config.yaml. Renders nothing while loading, on error, or
 * when no contacts are configured: a missing footer is better than a broken one.
 */
export default function ContactsFooter({ className = '' }: { className?: string }) {
  const contacts = useContacts()
  if (!contacts.data?.length) return null
  return (
    <footer className={`border-t border-slate-800 pt-4 text-center text-sm ${className}`}>
      <p className="mb-2 text-slate-500">{t.contacts.title}</p>
      <nav className="flex flex-wrap justify-center gap-x-5 gap-y-2">
        {contacts.data.map((c) => (
          <a key={c.url} href={c.url} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300">
            {c.label}
          </a>
        ))}
      </nav>
    </footer>
  )
}
