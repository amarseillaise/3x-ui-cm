import { useSearchParams } from 'react-router'
import Message from '../components/Message'
import ContactsFooter from '../components/ContactsFooter'
import { t } from '../i18n/ru'

export default function InvalidLinkPage() {
  const [params] = useSearchParams()
  const unavailable = params.get('reason') === 'unavailable'
  return (
    <Message
      title={unavailable ? t.invalidLink.unavailableTitle : t.invalidLink.title}
      body={unavailable ? t.invalidLink.unavailableBody : t.invalidLink.body}
    >
      <ContactsFooter className="mt-6 w-full" />
    </Message>
  )
}
