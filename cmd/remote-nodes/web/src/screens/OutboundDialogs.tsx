import { useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { Info } from '../components/icons'
import { Button, Dialog, ErrorBox, Field, Input, Notice, Switch, Textarea, useToast } from '../components/ui'
import { useErrorText, useLang } from '../i18n'
import { ApiError, MAX_OUTBOUND_NAME, outbounds as outboundsApi, type Outbound, type OutboundPatch, type OutboundSettings, type TestResult } from '../lib/api'
import { isWebAddress, parseOutbound, type Parsed } from '../lib/link-parser'
import { kindText, noteText, restartsBridge, serverText, transportText } from '../lib/outbounds'

// --- pieces both dialogs use --------------------------------------------------------------------

/** The paste box, with the proxy question for an http(s) address, what was read, or why it can't be used. */
function PasteField({
  label,
  hint,
  text,
  onText,
  httpProxy,
  onHttpProxy,
  parsed,
  children,
}: {
  label: string
  hint: string
  text: string
  onText: (text: string) => void
  httpProxy: boolean
  onHttpProxy: (on: boolean) => void
  parsed: Parsed
  /** under what was read: the Test button and its answer */
  children?: ReactNode
}) {
  const { t } = useLang()
  const refused = !parsed.ok && parsed.reason.code !== 'empty' ? noteText(t, parsed.reason) : null
  return (
    <div className="flex flex-col gap-3">
      <Field label={label} hint={hint} error={refused}>
        {(p) => (
          <Textarea
            {...p}
            dir="ltr"
            rows={4}
            className="min-h-[96px] break-all"
            value={text}
            onChange={(e) => onText(e.target.value)}
            spellCheck={false}
            autoCapitalize="off"
            autoComplete="off"
            autoCorrect="off"
            placeholder={'vless://…  ·  { "protocol": "vless", … }'}
          />
        )}
      </Field>
      {isWebAddress(text) && (
        <label className="flex min-h-[44px] items-center gap-2 text-[13px] sm:min-h-0">
          <input type="checkbox" className="h-4 w-4" checked={httpProxy} onChange={(e) => onHttpProxy(e.target.checked)} />
          {t('ob.form.httpProxy')}
        </label>
      )}
      {parsed.ok && (
        <ReadSummary settings={parsed.settings} notices={parsed.notices.map((n) => noteText(t, n))}>
          {children}
        </ReadSummary>
      )}
    </div>
  )
}

/** A value read from a link: technical, so it reads left to right inside a Persian sentence. */
const Ltr = ({ children }: { children: ReactNode }) => (
  <dd className="min-w-0 break-words">
    <span dir="ltr">{children}</span>
  </dd>
)

/** What a paste said, without its secrets: kind, server, transport and security. */
export function ReadSummary({ settings: s, notices = [], title, children }: { settings: Pick<OutboundSettings, 'protocol' | 'host' | 'port' | 'network' | 'mode' | 'path' | 'security' | 'serverName'>; notices?: string[]; title?: string; children?: ReactNode }) {
  const { t } = useLang()
  const security = s.security === 'none' ? t('ob.form.none') : `${s.security === 'tls' ? 'TLS' : 'REALITY'} · ${s.serverName}`
  return (
    <div data-read className="flex flex-col gap-2 rounded-md border border-border bg-card2 p-3 text-[13px]">
      <div className="text-xs font-medium text-muted">{title ?? t('ob.form.read')}</div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
        <dt className="text-muted">{t('ob.form.kind')}</dt>
        <Ltr>{kindText(s)}</Ltr>
        <dt className="text-muted">{t('ob.col.server')}</dt>
        <Ltr>
          <span className="font-mono text-xs">{serverText(s.host, s.port)}</span>
        </Ltr>
        <dt className="text-muted">{t('ob.form.transport')}</dt>
        <Ltr>{transportText(s)}</Ltr>
        <dt className="text-muted">{t('ob.form.security')}</dt>
        <Ltr>{security}</Ltr>
      </dl>
      {notices.map((n) => (
        <p key={n} className="flex items-start gap-2 text-xs text-warn-text">
          <Info className="mt-px h-3.5 w-3.5 shrink-0" />
          {n}
        </p>
      ))}
      {children}
    </div>
  )
}

/**
 * A test's answer: the delay, or that it didn't answer and why in the core's words. Plain coloured text, not a
 * tinted badge: it sits on the grey of the summary box too, where the tint would take the green below 4.5.
 */
export function TestAnswer({ result }: { result: TestResult }) {
  const { t } = useLang()
  if (result.ok) {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-ok-text">
        <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-ok" />
        {t('ob.test.ok', { delay: t('unit.ms', { n: result.delayMs ?? 0 }) })}
      </span>
    )
  }
  return (
    <span className="flex flex-col gap-0.5 text-xs text-bad-text">
      <span className="inline-flex items-center gap-1.5 font-medium">
        <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-bad" />
        {t('ob.test.failed')}
      </span>
      {result.error && (
        <span dir="auto" className="break-words">
          {result.error}
        </span>
      )}
    </span>
  )
}

/** Runs a test and keeps its answer for the settings it was run on, so an answer never stands for other settings. */
function useTest() {
  const errorText = useErrorText()
  const [answer, setAnswer] = useState<{ key: string; result: TestResult } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<{ key: string; text: string } | null>(null)
  return {
    busy,
    answerFor: (key: string) => (answer?.key === key ? answer.result : null),
    errorFor: (key: string) => (error?.key === key ? error.text : null),
    run: async (key: string, call: () => Promise<TestResult>) => {
      setBusy(true)
      setError(null)
      try {
        setAnswer({ key, result: await call() })
      } catch (e) {
        setAnswer(null)
        setError({ key, text: errorText(e) })
      } finally {
        setBusy(false)
      }
    },
  }
}

/** The Test button and what came back, as one live region. */
function TestRow({ busy, disabled, onTest, result, error }: { busy: boolean; disabled: boolean; onTest: () => void; result: TestResult | null; error: string | null }) {
  const { t } = useLang()
  return (
    <div className="flex flex-wrap items-start gap-3 border-t border-border pt-2">
      <Button size="sm" busy={busy} disabled={disabled} onClick={onTest}>
        {t('ob.test')}
      </Button>
      <div role="status" className="min-w-0 flex-1 self-center">
        {result && <TestAnswer result={result} />}
        {error && (
          <span dir="auto" className="break-words text-xs text-bad-text">
            {error}
          </span>
        )}
      </div>
    </div>
  )
}

/** The fallback switch with its words beside it, the way a setting reads. */
function FallbackSwitch({ checked, onChange }: { checked: boolean; onChange: (on: boolean) => void }) {
  const { t } = useLang()
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0 text-[13px]">
        <div className="font-medium">{t('ob.form.fallback')}</div>
        <p className="text-xs text-faint">{t('ob.form.fallbackHint')}</p>
      </div>
      <Switch checked={checked} onChange={onChange} label={t('ob.form.fallback')} />
    </div>
  )
}

const nameOk = (name: string) => name.trim() !== '' && [...name.trim()].length <= MAX_OUTBOUND_NAME

/** A name already used belongs under the name field; anything else goes in a box. */
function useSaveError() {
  const errorText = useErrorText()
  const [nameError, setNameError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  return {
    nameError,
    error,
    clear: () => {
      setNameError(null)
      setError(null)
    },
    set: (e: unknown) => (e instanceof ApiError && e.code === 'name_taken' ? setNameError(errorText(e)) : setError(errorText(e))),
  }
}

// --- add ------------------------------------------------------------------------------------------

/** Add an outbound from a pasted link or JSON. Nothing typed here is kept once the dialog closes. */
export function AddOutboundDialog({ onClose, onAdded }: { onClose: () => void; onAdded: (o: Outbound) => void }) {
  const { t } = useLang()
  const toast = useToast()
  const [text, setText] = useState('')
  const [httpProxy, setHttpProxy] = useState(false)
  // the name follows the remark until it is typed in
  const [ownName, setOwnName] = useState<string | null>(null)
  const [fallback, setFallback] = useState(false)
  const [tried, setTried] = useState(false)
  const [busy, setBusy] = useState(false)
  const errors = useSaveError()
  const test = useTest()
  const parsed = useMemo(() => parseOutbound(text, { httpProxy }), [text, httpProxy])
  const name = ownName ?? (parsed.ok ? parsed.name : '')
  const key = parsed.ok ? JSON.stringify(parsed.settings) : ''
  const nameProblem = tried && !nameOk(name) ? t('ob.form.errName') : errors.nameError

  const save = async () => {
    setTried(true)
    if (!parsed.ok || !nameOk(name) || busy) return
    setBusy(true)
    errors.clear()
    try {
      const o = await outboundsApi.create({ name: name.trim(), fallback, settings: parsed.settings })
      toast(t('ob.toast.added', { name: o.name }))
      onAdded(o)
      onClose()
    } catch (e) {
      errors.set(e)
      setBusy(false)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('ob.add.title')}
      description={t('ob.add.desc')}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="add-outbound" busy={busy} disabled={!parsed.ok}>
            {t('ob.add')}
          </Button>
        </>
      }
    >
      <form
        id="add-outbound"
        className="flex flex-col gap-4"
        autoComplete="off"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          void save()
        }}
      >
        <PasteField
          label={t('ob.form.paste')}
          hint={t('ob.form.pasteHint')}
          text={text}
          onText={setText}
          httpProxy={httpProxy}
          onHttpProxy={setHttpProxy}
          parsed={parsed}
        >
          {parsed.ok && (
            <TestRow
              busy={test.busy}
              disabled={busy}
              onTest={() => void test.run(key, () => outboundsApi.testSettings(parsed.settings))}
              result={test.answerFor(key)}
              error={test.errorFor(key)}
            />
          )}
        </PasteField>
        <Field label={t('ob.form.name')} hint={t('ob.form.nameHint', { n: MAX_OUTBOUND_NAME })} error={nameProblem}>
          {(p) => <Input {...p} dir="auto" value={name} maxLength={MAX_OUTBOUND_NAME} onChange={(e) => setOwnName(e.target.value)} />}
        </Field>
        <FallbackSwitch checked={fallback} onChange={setFallback} />
        {errors.error && <ErrorBox>{errors.error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

// --- edit -----------------------------------------------------------------------------------------

/** What an outbound has now, without its secrets: a hint of the user id, or dots for a password. */
function NowSummary({ o }: { o: Outbound }) {
  const { t } = useLang()
  // SOCKS and HTTP have a password only with a user name
  const password = o.protocol === 'trojan' || o.protocol === 'shadowsocks' || !!o.username
  return (
    <ReadSummary settings={o} title={t('ob.form.now')}>
      {(o.secretHint || password) && (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 border-t border-border pt-2">
          {o.username && (
            <>
              <dt className="text-muted">{t('login.username')}</dt>
              <Ltr>{o.username}</Ltr>
            </>
          )}
          <dt className="text-muted">{o.secretHint ? t('ob.form.secretId') : t('ob.form.secretPassword')}</dt>
          <Ltr>
            <span className="font-mono text-xs">{o.secretHint || '••••••••'}</span>
          </Ltr>
        </dl>
      )}
    </ReadSummary>
  )
}

/**
 * Edit an outbound: its name, the fallback switch, and, if a new link or JSON is pasted, all of its settings.
 * Says first when saving restarts the bridge, which drops open connections.
 */
export function EditOutboundDialog({ outbound: o, onClose, onSaved }: { outbound: Outbound; onClose: () => void; onSaved: (o: Outbound) => void }) {
  const { t } = useLang()
  const toast = useToast()
  const [name, setName] = useState(o.name)
  const [fallback, setFallback] = useState(o.fallback)
  const [text, setText] = useState('')
  const [httpProxy, setHttpProxy] = useState(false)
  const [busy, setBusy] = useState(false)
  const errors = useSaveError()
  const test = useTest()
  const parsed = useMemo(() => parseOutbound(text, { httpProxy }), [text, httpProxy])
  const replacing = text.trim() !== ''

  const patch: OutboundPatch = {}
  if (name.trim() !== o.name) patch.name = name.trim()
  if (fallback !== o.fallback) patch.fallback = fallback
  if (parsed.ok) patch.settings = parsed.settings
  const ready = Object.keys(patch).length > 0 && nameOk(name) && (!replacing || parsed.ok)
  const restart = restartsBridge(o, patch)
  // the test is of what would be saved: the pasted settings, else the saved ones
  const key = parsed.ok ? JSON.stringify(parsed.settings) : `saved-${o.id}`

  const save = async () => {
    if (!ready || busy) return
    setBusy(true)
    errors.clear()
    try {
      const fresh = await outboundsApi.update(o.id, patch)
      toast(t('ob.toast.saved', { name: fresh.name }))
      onSaved(fresh)
      onClose()
    } catch (e) {
      errors.set(e)
      setBusy(false)
    }
  }

  return (
    <Dialog
      onClose={onClose}
      title={t('ob.edit.label', { name: o.name })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" type="submit" form="edit-outbound" busy={busy} disabled={!ready}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <form
        id="edit-outbound"
        className="flex flex-col gap-4"
        autoComplete="off"
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          void save()
        }}
      >
        <Field label={t('ob.form.name')} hint={t('ob.form.nameHint', { n: MAX_OUTBOUND_NAME })} error={!nameOk(name) ? t('ob.form.errName') : errors.nameError}>
          {(p) => <Input {...p} dir="auto" value={name} maxLength={MAX_OUTBOUND_NAME} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <FallbackSwitch checked={fallback} onChange={setFallback} />
        {!parsed.ok && <NowSummary o={o} />}
        <PasteField
          label={t('ob.form.replace')}
          hint={t('ob.form.replaceHint')}
          text={text}
          onText={setText}
          httpProxy={httpProxy}
          onHttpProxy={setHttpProxy}
          parsed={parsed}
        />
        {(!replacing || parsed.ok) && (
          <TestRow
            busy={test.busy}
            disabled={busy}
            onTest={() => void test.run(key, () => (parsed.ok ? outboundsApi.testSettings(parsed.settings) : outboundsApi.test(o.id)))}
            result={test.answerFor(key)}
            error={test.errorFor(key)}
          />
        )}
        {restart && <Notice tone="warn">{t('ob.form.restart', { name: o.name })}</Notice>}
        {errors.error && <ErrorBox>{errors.error}</ErrorBox>}
      </form>
    </Dialog>
  )
}

// --- remove ---------------------------------------------------------------------------------------

/**
 * Remove an outbound, after asking. One in use can't go: the dialog says who leaves through it instead,
 * and does the same when the hub refuses because something started using it meanwhile.
 */
export function RemoveOutboundDialog({
  outbound: o,
  users,
  usersText,
  onClose,
  onRemoved,
  onRefused,
}: {
  outbound: Outbound
  /** who leaves through it, by name */
  users: string[]
  /** the same, as one phrase in the page's language */
  usersText: string
  onClose: () => void
  onRemoved: () => void
  /** the hub said it is in use: the list is loaded again */
  onRefused: () => void
}) {
  const { t } = useLang()
  const errorText = useErrorText()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // the hub's own words, when it knew of a user the page didn't
  const [refusal, setRefusal] = useState<string | null>(null)

  if (users.length > 0 || refusal) {
    return (
      <Dialog alert onClose={onClose} title={t('ob.inUse.title', { name: o.name })} footer={<Button onClick={onClose}>{t('common.close')}</Button>}>
        <p className="text-[13px] text-muted" dir={users.length ? undefined : 'auto'}>
          {users.length ? t('ob.inUse.text', { users: usersText, n: users.length }) : refusal}
        </p>
      </Dialog>
    )
  }

  const remove = async () => {
    setBusy(true)
    setError(null)
    try {
      await outboundsApi.remove(o.id)
      toast(t('ob.toast.removed', { name: o.name }))
      onRemoved()
      onClose()
    } catch (e) {
      setBusy(false)
      if (e instanceof ApiError && e.code === 'outbound_in_use') {
        setRefusal(e.message)
        onRefused()
      } else setError(errorText(e))
    }
  }

  return (
    <Dialog
      alert
      onClose={onClose}
      title={t('ob.remove.title', { name: o.name })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="danger" busy={busy} onClick={() => void remove()}>
            {t('ob.remove.action')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-[13px] text-muted">
        <p>{t('ob.remove.text')}</p>
        {error && <ErrorBox>{error}</ErrorBox>}
      </div>
    </Dialog>
  )
}
