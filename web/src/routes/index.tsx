import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { AudioLines, FileText, Sparkles, Video } from 'lucide-react'
import { MeetingList } from '@/components/MeetingList'
import { SendBotForm } from '@/components/SendBotForm'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { api, keys } from '@/lib/api'
import { isTerminal } from '@/types'

type Search = { url?: string }

export const Route = createFileRoute('/')({
  validateSearch: (s: Record<string, unknown>): Search => (typeof s.url === 'string' ? { url: s.url } : {}),
  component: Dashboard,
})

function Dashboard() {
  const { url } = Route.useSearch()
  const meetings = useQuery({
    queryKey: keys.meetings,
    queryFn: api.list,
    // Poll only while some bot is active, so list badges stay current.
    refetchInterval: (q) => (q.state.data?.some((m) => !isTerminal(m.status)) ? 3000 : false),
  })

  return (
    <div className="mx-auto max-w-4xl px-4 py-10 sm:px-6 sm:py-14">
      <section className="animate-fade-in">
        <h1 className="text-2xl font-semibold tracking-tight text-balance sm:text-3xl">Send a notetaker to your meeting</h1>
        <p className="mt-2 max-w-2xl text-muted-foreground">
          Paste a Google Meet link. The bot joins as a guest, transcribes the call live, and writes a summary with decisions and action items when it ends.
        </p>
        <Card className="mt-6 p-4 sm:p-5">
          <SendBotForm key={url} initialUrl={url} />
        </Card>
        <ol className="mt-5 grid gap-3 text-sm text-muted-foreground sm:grid-cols-3">
          {[
            { icon: Video, text: 'Joins as a guest; the host admits it' },
            { icon: AudioLines, text: 'Live transcript, every ~20 s' },
            { icon: Sparkles, text: 'Summary, decisions, action items' },
          ].map(({ icon: Icon, text }) => (
            <li key={text} className="flex items-center gap-2.5">
              <Icon className="size-4 shrink-0 text-primary" />
              {text}
            </li>
          ))}
        </ol>
      </section>

      <section className="mt-12">
        <h2 className="mb-3 text-sm font-semibold tracking-wide text-muted-foreground uppercase">Recent meetings</h2>
        {meetings.isPending ? (
          <div className="space-y-px overflow-hidden rounded-xl border">
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex items-center gap-4 bg-card px-4 py-4">
                <Skeleton className="size-9 rounded-lg" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-24" />
                </div>
                <Skeleton className="h-5 w-20 rounded-full" />
              </div>
            ))}
          </div>
        ) : meetings.isError ? (
          <Card className="p-6 text-sm text-red-600 dark:text-red-400">Could not load meetings: {meetings.error.message}</Card>
        ) : meetings.data.length === 0 ? (
          <Card className="flex flex-col items-center gap-3 border-dashed px-6 py-12 text-center">
            <span className="flex size-12 items-center justify-center rounded-full bg-muted">
              <FileText className="size-5 text-muted-foreground" />
            </span>
            <p className="font-medium">No meetings yet</p>
            <p className="max-w-sm text-sm text-muted-foreground">Send the bot to a meeting above. Its transcript and summary will show up here.</p>
          </Card>
        ) : (
          <MeetingList meetings={meetings.data} />
        )}
      </section>
    </div>
  )
}
