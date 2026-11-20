import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ArrowLeft, Copy, ExternalLink, LogOut, MessageSquareText, RotateCcw, Sparkles, Trash2, WifiOff } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { StatusBadge } from '@/components/StatusBadge'
import { StatusStepper } from '@/components/StatusStepper'
import { SummaryPanel } from '@/components/SummaryPanel'
import { TranscriptView } from '@/components/TranscriptView'
import { Confirm } from '@/components/ui/alert-dialog'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip } from '@/components/ui/tooltip'
import { api, ApiError, keys } from '@/lib/api'
import { clock, endReasonLabel, meetingDuration, meetingName, relativeTime } from '@/lib/format'
import { useMeetingEvents } from '@/lib/sse'
import { useMediaQuery } from '@/lib/useMediaQuery'
import { isTerminal, type Meeting, type Status } from '@/types'

export const Route = createFileRoute('/meetings/$id')({
  component: MeetingPage,
})

function MeetingPage() {
  const { id } = Route.useParams()
  const meeting = useQuery({ queryKey: keys.meeting(id), queryFn: () => api.get(id) })
  const transcript = useQuery({ queryKey: keys.transcript(id), queryFn: () => api.transcript(id) })
  const live = !!meeting.data && !isTerminal(meeting.data.status)
  const conn = useMeetingEvents(id, live)
  const wide = useMediaQuery('(min-width: 1024px)')

  if (meeting.isPending) return <PageSkeleton />
  if (meeting.isError) {
    const notFound = meeting.error instanceof ApiError && meeting.error.status === 404
    return (
      <div className="mx-auto max-w-md px-4 py-24 text-center">
        <h1 className="text-xl font-semibold">{notFound ? 'Meeting not found' : 'Something went wrong'}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{notFound ? 'It may have been deleted.' : meeting.error.message}</p>
        <Link to="/" className={buttonVariants({ variant: 'outline', className: 'mt-6' })}>Back to dashboard</Link>
      </div>
    )
  }
  const m = meeting.data
  const segments = transcript.data ?? []

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-5 px-4 py-6 sm:px-6 lg:h-[calc(100dvh-3.5rem)]">
      <Header meeting={m} reconnecting={live && conn === 'reconnecting'} />
      <Card className="px-4 py-4 sm:px-6">
        <StatusStepper status={m.status} failedAt={failedStep(m)} />
      </Card>
      {m.status === 'failed' && <FailureCard meeting={m} />}

      {/* Desktop: two columns. Mobile: tabs. Only one layout is mounted. */}
      {wide ? (
        <div className="grid min-h-0 flex-1 grid-cols-[1.25fr_1fr] gap-5">
          <Card className="min-h-0 overflow-hidden"><TranscriptView segments={segments} status={m.status} /></Card>
          <Card className="min-h-0 overflow-hidden"><SummaryPanel meeting={m} /></Card>
        </div>
      ) : (
        <Tabs defaultValue={m.summary ? 'summary' : 'transcript'}>
          <TabsList className="w-full">
            <TabsTrigger value="transcript"><MessageSquareText className="size-4" /> Transcript</TabsTrigger>
            <TabsTrigger value="summary"><Sparkles className="size-4" /> Summary</TabsTrigger>
          </TabsList>
          <TabsContent value="transcript"><Card className="mt-3 h-[70dvh] overflow-hidden"><TranscriptView segments={segments} status={m.status} /></Card></TabsContent>
          <TabsContent value="summary"><Card className="mt-3 h-[70dvh] overflow-hidden"><SummaryPanel meeting={m} /></Card></TabsContent>
        </Tabs>
      )}
    </div>
  )
}

function Header({ meeting: m, reconnecting }: { meeting: Meeting; reconnecting: boolean }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const stop = useMutation({
    mutationFn: () => api.stop(m.id),
    onSuccess: () => toast('Asking the bot to leave…', { description: 'The summary will be ready in a moment.' }),
    onError: (e: Error) => toast.error(e.message),
  })
  const remove = useMutation({
    mutationFn: () => api.remove(m.id),
    onSuccess: () => {
      qc.removeQueries({ queryKey: keys.meeting(m.id) })
      qc.invalidateQueries({ queryKey: keys.meetings })
      toast.success('Meeting deleted')
      navigate({ to: '/' })
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const active = !isTerminal(m.status)
  const canStop = active && !['leaving', 'transcribing', 'summarizing'].includes(m.status)

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-center gap-3">
        <Tooltip content="All meetings">
          <Link to="/" className={buttonVariants({ variant: 'ghost', size: 'icon', className: 'shrink-0' })} aria-label="Back to all meetings">
            <ArrowLeft />
          </Link>
        </Tooltip>
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold tracking-tight sm:text-xl">{meetingName(m)}</h1>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <a href={m.meeting_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-foreground">
              {meetingName(m) === m.meeting_code ? 'Google Meet' : <span className="font-mono">{m.meeting_code}</span>} <ExternalLink className="size-3" />
            </a>
            <span>·</span>
            <span>{relativeTime(m.created_at)}</span>
            {m.end_reason && m.status === 'done' && (<><span>·</span><span>{endReasonLabel[m.end_reason]}</span></>)}
          </div>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 pl-12 sm:pl-0">
        {reconnecting && (
          <Tooltip content="Lost the live connection; reconnecting…">
            <span className="inline-flex items-center gap-1 text-xs text-amber-600"><WifiOff className="size-3.5" /> Reconnecting</span>
          </Tooltip>
        )}
        <StatusBadge status={m.status} />
        <Elapsed meeting={m} />
        {canStop && (
          <Confirm
            title="Stop the bot?"
            description="The bot leaves the meeting now. You still get the transcript and summary of everything so far."
            confirmLabel="Stop bot"
            onConfirm={() => stop.mutate()}
          >
            <Button variant="outline" size="sm" disabled={stop.isPending}><LogOut /> Stop bot</Button>
          </Confirm>
        )}
        {!active && (
          <Confirm
            title="Delete this meeting?"
            description="The transcript, summary and audio recording are deleted for good."
            confirmLabel="Delete"
            destructive
            onConfirm={() => remove.mutate()}
          >
            <Button variant="ghost" size="icon" aria-label="Delete meeting" disabled={remove.isPending}><Trash2 /></Button>
          </Confirm>
        )}
      </div>
    </div>
  )
}

/** Call length, ticking every second while the bot is in the call. */
function Elapsed({ meeting }: { meeting: Meeting }) {
  const [now, setNow] = useState(Date.now())
  const ticking = meeting.status === 'in_call'
  useEffect(() => {
    if (!ticking) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [ticking])
  const d = meetingDuration(meeting, now)
  if (d === null) return null
  return <span className="font-mono text-sm tabular-nums text-muted-foreground">{clock(d)}</span>
}

const failureHelp: Record<string, string> = {
  denied: 'Ask the host to admit "Meeting Agent" from the lobby next time.',
  lobby_timeout: 'Nobody admitted the bot. Make sure someone in the call can see the lobby request.',
  join_blocked: 'This meeting may only allow people from its organization. Try a meeting that allows guests.',
  meeting_not_found: 'Check the link, and that the meeting is still running.',
  join_timeout: 'Google Meet’s page may have changed. A screenshot was saved in data/debug.',
  interrupted: 'The server restarted during the meeting.',
}

function FailureCard({ meeting: m }: { meeting: Meeting }) {
  const navigate = useNavigate()
  const help = (m.error_code && failureHelp[m.error_code]) || null
  return (
    <Card className="flex animate-fade-in flex-col gap-3 border-red-500/30 bg-red-500/5 p-4 sm:flex-row sm:items-center">
      <AlertTriangle className="size-5 shrink-0 text-red-600 dark:text-red-400" />
      <div className="flex-1 text-sm">
        <p className="font-medium">{m.error_message ?? 'The bot could not finish this meeting.'}</p>
        {help && <p className="mt-0.5 text-muted-foreground">{help}</p>}
      </div>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" onClick={() => navigator.clipboard.writeText(m.meeting_url).then(() => toast.success('Link copied'))}>
          <Copy /> Copy link
        </Button>
        <Button size="sm" onClick={() => navigate({ to: '/', search: { url: m.meeting_url } })}>
          <RotateCcw /> Try again
        </Button>
      </div>
    </Card>
  )
}

// Which step a failed meeting stopped at, for the stepper.
function failedStep(m: Meeting): Status {
  if (m.joined_at) return 'in_call'
  if (m.error_code === 'denied' || m.error_code === 'lobby_timeout') return 'in_lobby'
  if (m.error_code === 'chrome_launch' || m.error_code === 'start_failed') return 'queued'
  return 'joining'
}

function PageSkeleton() {
  return (
    <div className="mx-auto max-w-6xl space-y-5 px-4 py-6 sm:px-6">
      <div className="flex items-center gap-3"><Skeleton className="size-9" /><Skeleton className="h-6 w-56" /></div>
      <Skeleton className="h-16 w-full rounded-xl" />
      <div className="grid gap-5 lg:grid-cols-[1.25fr_1fr]"><Skeleton className="h-[60vh] rounded-xl" /><Skeleton className="h-[60vh] rounded-xl" /></div>
    </div>
  )
}
