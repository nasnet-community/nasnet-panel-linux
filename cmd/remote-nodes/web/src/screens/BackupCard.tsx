import { useState } from 'react'
import { CircleCheck, Download, Upload } from '../components/icons'
import { Head } from '../components/section'
import { Button, Card, Dialog, ErrorBox, Field, Input, Notice, useToast } from '../components/ui'
import { navigate } from '../hooks/useRoute'
import { useErrorText, useLang } from '../i18n'
import { backup, type AppConfig } from '../lib/api'
import { MAX_BACKUP_BYTES, bytesToBase64, saveFile } from '../lib/backup'

type Stage = { at: 'form' } | { at: 'done'; nodes: number }

/** Takes a backup file and its passphrase, and says what came back. Mounted only while open, so nothing typed outlives it. */
function ImportDialog({ nodeCount, onClose, onDone }: { nodeCount: number; onClose: () => void; onDone: () => void }) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [file, setFile] = useState<File | null>(null)
  const [pass, setPass] = useState('')
  const [stage, setStage] = useState<Stage>({ at: 'form' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    if (!file) return setError(t('backup.errFile'))
    if (!pass) return setError(t('backup.errPass'))
    // a real backup is a few KB; this is some other file
    if (file.size > MAX_BACKUP_BYTES) return setError(t('error.backup_bad_file'))
    setBusy(true)
    setError(null)
    try {
      const data = bytesToBase64(new Uint8Array(await file.arrayBuffer()))
      const res = await backup.import(pass, data)
      // the passphrase and the file have done their work
      setPass('')
      setFile(null)
      setStage({ at: 'done', nodes: res.nodes })
      toast(t('backup.imported', { n: res.nodes }))
      onDone()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  if (stage.at === 'done') {
    return (
      <Dialog
        onClose={onClose}
        title={t('backup.imported', { n: stage.nodes })}
        footer={
          <>
            <Button onClick={onClose}>{t('common.close')}</Button>
            <Button
              variant="primary"
              onClick={() => {
                onClose()
                navigate({ page: 'root', tab: 'nodes' })
              }}
            >
              {t('backup.showNodes')}
            </Button>
          </>
        }
      >
        <p className="flex items-start gap-3 text-[13px] text-muted">
          <CircleCheck className="mt-0.5 h-4 w-4 shrink-0 text-ok-text" />
          {t('backup.importedNote')}
        </p>
      </Dialog>
    )
  }

  const blocked = nodeCount > 0
  return (
    <Dialog
      onClose={onClose}
      title={t('backup.importTitle')}
      description={t('backup.importDesc')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="backup-import" busy={busy} disabled={blocked}>
            {t('backup.doImport')}
          </Button>
        </>
      }
    >
      <form
        id="backup-import"
        className="flex flex-col gap-3"
        autoComplete="off"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        {blocked && <Notice tone="warn">{t('backup.notEmpty')}</Notice>}
        <Field label={t('backup.file')}>
          {(p) => (
            <input
              {...p}
              type="file"
              disabled={blocked}
              onChange={(e) => setFile(e.currentTarget.files?.[0] ?? null)}
              className="w-full text-[13px] text-muted file:me-3 file:h-9 file:cursor-pointer file:rounded-md file:border file:border-border file:bg-card2 file:px-3 file:text-[13px] file:font-medium file:text-foreground"
            />
          )}
        </Field>
        <Field label={t('backup.pass')}>
          {(p) => <Input {...p} dir="ltr" type="password" disabled={blocked} value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="new-password" />}
        </Field>
        {error && <ErrorBox>{error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

/**
 * The backup holds the certificate authority every node trusts, so losing it means installing every
 * node again. Export downloads it encrypted under a passphrase; import brings the nodes back on a fresh install.
 */
export function BackupCard({ config, onChanged }: { config: AppConfig; onChanged: () => void }) {
  const { t, fmt } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [pass, setPass] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [importing, setImporting] = useState(false)
  const last = config.backup.lastExportAt

  const exportBackup = async () => {
    if (!pass) return setError(t('backup.errPass'))
    setBusy(true)
    setError(null)
    try {
      const file = await backup.export(pass)
      saveFile(file)
      // it is in the downloads now; the passphrase has no more to do here
      setPass('')
      toast(t('backup.saved', { name: file.name }))
      onChanged()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card className="flex flex-col gap-3 p-4">
      <Head title={t('backup.title')} hint={t('backup.hint')} />
      {last ? (
        <p className="text-[13px] text-muted">{t('backup.last', { ago: fmt.relative(last) })}</p>
      ) : (
        <Notice tone="warn">
          <b>{t('backup.none')}</b> {t('backup.noneNote')}
        </Notice>
      )}
      <form
        className="flex flex-col gap-3"
        autoComplete="off"
        onSubmit={(e) => {
          e.preventDefault()
          void exportBackup()
        }}
      >
        <Field label={t('backup.pass')} hint={t('backup.passHint')} error={error}>
          {(p) => <Input {...p} dir="ltr" type="password" value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="new-password" />}
        </Field>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-3">
          <Button variant="primary" type="submit" busy={busy}>
            {!busy && <Download className="h-4 w-4" />} {t('backup.export')}
          </Button>
          <div className="flex items-center gap-3">
            <span className="text-[13px] text-muted">{t('backup.importNote')}</span>
            <Button size="sm" onClick={() => setImporting(true)}>
              <Upload className="h-3.5 w-3.5" /> {t('backup.import')}
            </Button>
          </div>
        </div>
      </form>
      {importing && <ImportDialog nodeCount={config.nodeCount} onClose={() => setImporting(false)} onDone={onChanged} />}
    </Card>
  )
}
