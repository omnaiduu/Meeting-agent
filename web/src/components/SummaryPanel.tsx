import { Check, CircleHelp, ClipboardCopy, Download, Gavel, ListChecks, Sparkles, SquareCheck, User, CalendarClock } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { summaryMarkdown } from '@/lib/format'
import type { Meeting } from '@/types'

export function SummaryPanel({ meeting }: { meeting: Meeting }) {
  const s = meeting.summary
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (!s) return
    await navigator.clipboard.writeText(summaryMarkdown(meeting, s))
    setCopied(true)
    toast.success('Summary copied as Markdown')
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Summary">
      <div className="flex items-center justify-between gap-2 border-b px-5 py-3">
        <h2 className="flex items-center gap-2 font-semibold">
          <Sparkles className="size-4 text-violet-500" /> Summary
        </h2>
        <div className="flex gap-1">
          {meeting.has_audio && (
            <Button variant="ghost" size="sm" asChild>
              <a href={api.audioUrl(meeting.id)} download>
                <Download /> Audio
              </a>
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={copy} disabled={!s}>
            {copied ? <Check /> : <ClipboardCopy />} Markdown
          </Button>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {s ? <SummaryBody meeting={meeting} /> : <Pending meeting={meeting} />}
      </div>
    </section>
  )
}

function SummaryBody({ meeting }: { meeting: Meeting }) {
  const s = meeting.summary!
  return (
    <div className="animate-fade-in space-y-6">
      <p className="text-[15px] leading-relaxed">{s.overview}</p>
      <Section icon={<ListChecks />} title="Key points" items={s.key_points} />
      <Section icon={<Gavel />} title="Decisions" items={s.decisions} />
      {s.action_items.length > 0 && (
        <div>
          <SectionTitle icon={<SquareCheck />} title="Action items" count={s.action_items.length} />
          <ul className="mt-2 space-y-2">
            {s.action_items.map((a, i) => (
              <li key={i} className="flex gap-3 rounded-lg border bg-muted/40 p-3">
                <input type="checkbox" className="mt-1 size-4 shrink-0 accent-[var(--primary)]" aria-label={`Done: ${a.task}`} />
                <div className="min-w-0 space-y-1">
                  <p className="text-sm font-medium">{a.task}</p>
                  {(a.owner || a.due) && (
                    <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
                      {a.owner && <span className="inline-flex items-center gap-1"><User className="size-3.5" />{a.owner}</span>}
                      {a.due && <span className="inline-flex items-center gap-1"><CalendarClock className="size-3.5" />{a.due}</span>}
                    </div>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Section icon={<CircleHelp />} title="Open questions" items={s.open_questions} />
      {meeting.summary_model && <p className="pt-2 text-xs text-muted-foreground">Written by {meeting.summary_model}. Check important details against the transcript.</p>}
    </div>
  )
}

function SectionTitle({ icon, title, count }: { icon: ReactNode; title: string; count: number }) {
  return (
    <h3 className="flex items-center gap-2 text-sm font-semibold [&_svg]:size-4 [&_svg]:text-muted-foreground">
      {icon} {title} <span className="text-xs font-normal text-muted-foreground">{count}</span>
    </h3>
  )
}

function Section({ icon, title, items }: { icon: ReactNode; title: string; items: string[] }) {
  if (!items.length) return null
  return (
    <div>
      <SectionTitle icon={icon} title={title} count={items.length} />
      <ul className="mt-2 space-y-1.5 text-sm leading-relaxed">
        {items.map((t, i) => (
          <li key={i} className="flex gap-2.5">
            <span className="mt-2 size-1.5 shrink-0 rounded-full bg-primary/60" />
            <span>{t}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function Pending({ meeting }: { meeting: Meeting }) {
  if (meeting.status === 'failed') {
    return <p className="text-sm text-muted-foreground">No summary: the bot didn't finish this meeting.</p>
  }
  const summarizing = meeting.status === 'summarizing' || meeting.status === 'transcribing'
  return (
    <div className="space-y-5" aria-busy="true">
      <p className="text-sm text-muted-foreground">
        {summarizing ? 'Writing the summary…' : 'The summary appears here when the meeting ends.'}
      </p>
      <div className={summarizing ? '' : 'opacity-60'}>
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-11/12" />
          <Skeleton className="h-4 w-4/5" />
        </div>
        {[0, 1].map((k) => (
          <div key={k} className="mt-6 space-y-2">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3.5 w-2/3" />
          </div>
        ))}
      </div>
    </div>
  )
}
