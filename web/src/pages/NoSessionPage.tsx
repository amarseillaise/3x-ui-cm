import Message from '../components/Message'
import ContactsFooter from '../components/ContactsFooter'
import { t } from '../i18n/ru'

export default function NoSessionPage() {
  return (
    <Message title={t.noSession.title} body={t.noSession.body}>
      <ContactsFooter className="mt-6 w-full" />
    </Message>
  )
}
