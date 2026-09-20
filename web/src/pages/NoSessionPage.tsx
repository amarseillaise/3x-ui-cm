import Message from '../components/Message'
import { t } from '../i18n/ru'

export default function NoSessionPage() {
  return <Message title={t.noSession.title} body={t.noSession.body} />
}
