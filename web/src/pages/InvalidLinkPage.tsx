import { useSearchParams } from 'react-router'
import Message from '../components/Message'
import { t } from '../i18n/ru'

export default function InvalidLinkPage() {
  const [params] = useSearchParams()
  if (params.get('reason') === 'unavailable') {
    return <Message title={t.invalidLink.unavailableTitle} body={t.invalidLink.unavailableBody} />
  }
  return <Message title={t.invalidLink.title} body={t.invalidLink.body} />
}
