import { ArrowDown, Check, Copy, MessageSquareText } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { clock } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { Segment, Status } from '@/types'

/** Live transcript. Follows new lines unless the reader has scrolled up. */
export function TranscriptView({ segments, status }: { segments: Segment[]; status: Status }) {
  const box = useRef<HTMLDivElement>(null)
  const [follow, setFollow] = useState(true)
  const [copied, setCopied] = useState(false)
  const listening = status === 'in_call'

  const onScroll = () => {
    const el = box.current
    if (!el) return
    setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
  }
  useLayoutEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [segments.length, follow])
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(t)
  }, [copied])

  const copy = async () => {
    await navigator.clipboard.writeText(segments.map((s) => `[${clock(s.start_ms)}] ${s.text}`).join('\n'))
    setCopied(true)
  }

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label="Transcript">
      <div className="flex items-center justify-between gap-2 border-b px-5 py-3">
        <h2 className="flex items-center gap-2 font-semibold">
          {listening ? <Listening /> : <MessageSquareText className="size-4 text-muted-foreground" />}
          {listening ? 'Live transcript' : 'Transcript'}
        </h2>
        <Button variant="ghost" size="sm" onClick={copy} disabled={!segments.length}>
          {copied ? <Check /> : <Copy />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
      </div>
      <div className="relative min-h-0 flex-1">
        <div ref={box} onScroll={onScroll} className="absolute inset-0 overflow-y-auto px-5 py-4" aria-live="polite">
          {segments.length === 0 ? (
            <EmptyTranscript status={status} />
          ) : (
            <ol className="space-y-3">
              {segments.map((s) => (
                <li key={s.id} className="grid animate-fade-in grid-cols-[3.75rem_1fr] gap-3 text-[15px] leading-relaxed">
                  <time className="pt-0.5 font-mono text-xs text-muted-foreground tabular-nums">{clock(s.start_ms)}</time>
                  <p>{s.text}</p>
                </li>
              ))}
            </ol>
          )}
        </div>
        {!follow && segments.length > 0 && (
          <button
            onClick={() => setFollow(true)}
            className="absolute bottom-4 left-1/2 inline-flex -translate-x-1/2 animate-fade-in items-center gap-1.5 rounded-full bg-foreground px-3.5 py-1.5 text-xs font-medium text-background shadow-lg cursor-pointer"
          >
            <ArrowDown className="size-3.5" /> {listening ? 'Jump to live' : 'Jump to end'}
          </button>
        )}
      </div>
    </section>
  )
}

function Listening() {
  return (
    <span className="flex h-4 items-end gap-[3px]" aria-hidden="true">
      {[0, 1, 2, 3].map((i) => (
        <span key={i} className="w-[3px] animate-pulse rounded-full bg-emerald-500" style={{ height: `${[60, 100, 45, 80][i]}%`, animationDelay: `${i * 150}ms` }} />
      ))}
    </span>
  )
}

function EmptyTranscript({ status }: { status: Status }) {
  const text: Partial<Record<Status, string>> = {
    queued: 'Waiting for a free bot…',
    launching: 'Starting the browser…',
    joining: 'Joining the meeting…',
    in_lobby: 'Waiting for the host to admit the bot. Look for “Meeting Agent” in Meet’s lobby.',
    in_call: 'Listening. Lines appear here every ~20 seconds as people speak.',
    leaving: 'Wrapping up…',
    transcribing: 'Transcribing the last few seconds…',
    summarizing: 'No speech was captured.',
    done: 'No speech was captured in this meeting.',
    failed: 'No transcript.',
  }
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      <span className={cn('flex size-12 items-center justify-center rounded-full bg-muted', status === 'in_call' && 'bg-emerald-500/10')}>
        <MessageSquareText className={cn('size-5 text-muted-foreground', status === 'in_call' && 'text-emerald-600')} />
      </span>
      <p className="max-w-xs text-sm text-muted-foreground">{text[status]}</p>
    </div>
  )
}
