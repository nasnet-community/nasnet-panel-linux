import { Card } from '../components/ui'
import { useLang } from '../i18n'

/** Stands in for a tab that isn't built yet. */
export function Soon({ title }: { title: string }) {
  const { t } = useLang()
  return (
    <Card className="flex flex-col items-center gap-1 px-6 py-14 text-center">
      <h2 className="text-base font-semibold">{title}</h2>
      <p className="text-[13px] text-muted">{t('soon')}</p>
    </Card>
  )
}
