import { Link } from '@tanstack/react-router'
import { ChevronRight, Video } from 'lucide-react'
import { StatusBadge } from './StatusBadge'
import { duration, meetingDuration, meetingName, relativeTime } from '@/lib/format'
import type { Meeting } from '@/types'

export function MeetingList({ meetings }: { meetings: Meeting[] }) {
  return (
    <ul className="divide-y overflow-hidden rounded-xl border bg-card shadow-sm">
      {meetings.map((m) => {
        const d = meetingDuration(m)
        return (
          <li key={m.id}>
            <Link
              to="/meetings/$id"
              params={{ id: m.id }}
              className="group flex items-center gap-4 px-4 py-3.5 transition-colors hover:bg-muted/60 focus-visible:bg-muted/60 focus-visible:outline-none"
            >
              <span className="hidden size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground sm:flex">
                <Video className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{meetingName(m)}</span>
                  {m.title && <span className="hidden truncate font-mono text-xs text-muted-foreground md:inline">{m.meeting_code}</span>}
                </div>
                <div className="mt-0.5 truncate text-xs text-muted-foreground">
                  {m.status === 'failed' && m.error_message ? (
                    <span className="text-red-600 dark:text-red-400">{m.error_message}</span>
                  ) : (
                    <>
                      {relativeTime(m.created_at)}
                      {d !== null && ` · ${duration(d)}`}
                    </>
                  )}
                </div>
              </div>
              <StatusBadge status={m.status} />
              <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
            </Link>
          </li>
        )
      })}
    </ul>
  )
}
