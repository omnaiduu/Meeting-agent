import type { Meeting, Status, Summary } from '@/types'

export function clock(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(s / 3600)
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

export function duration(ms: number) {
  const m = Math.round(ms / 60000)
  if (m < 1) return '<1 min'
  if (m < 60) return `${m} min`
  return `${Math.floor(m / 60)} h ${m % 60} min`
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
export function relativeTime(iso: string, now = Date.now()) {
  const diff = (new Date(iso).getTime() - now) / 1000
  const abs = Math.abs(diff)
  if (abs < 45) return 'just now'
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour')
  if (abs < 7 * 86400) return rtf.format(Math.round(diff / 86400), 'day')
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

export function meetingDuration(m: Meeting, now = Date.now()) {
  if (!m.joined_at) return null
  const end = m.ended_at ? new Date(m.ended_at).getTime() : now
  return end - new Date(m.joined_at).getTime()
}

export function meetingName(m: Meeting) {
  return m.title || m.summary?.title || m.meeting_code
}

export const statusLabel: Record<Status, string> = {
  queued: 'Queued',
  launching: 'Starting',
  joining: 'Joining',
  in_lobby: 'In lobby',
  in_call: 'In call',
  leaving: 'Leaving',
  transcribing: 'Transcribing',
  summarizing: 'Summarizing',
  done: 'Done',
  failed: 'Failed',
}

export const endReasonLabel: Record<string, string> = {
  alone: 'Left after everyone else did',
  meeting_over: 'The meeting ended',
  max_duration: 'Reached the maximum length',
  stopped: 'Stopped from the dashboard',
}

export function summaryMarkdown(m: Meeting, s: Summary) {
  const lines = [`# ${s.title}`, '', `_${new Date(m.created_at).toLocaleString()} · ${m.meeting_code}_`, '', s.overview, '']
  const section = (title: string, items: string[]) => {
    if (!items.length) return
    lines.push(`## ${title}`, ...items.map((i) => `- ${i}`), '')
  }
  section('Key points', s.key_points)
  section('Decisions', s.decisions)
  if (s.action_items.length) {
    lines.push('## Action items')
    for (const a of s.action_items) {
      const meta = [a.owner, a.due && `due ${a.due}`].filter(Boolean).join(', ')
      lines.push(`- [ ] ${a.task}${meta ? ` (${meta})` : ''}`)
    }
    lines.push('')
  }
  section('Open questions', s.open_questions)
  return lines.join('\n')
}
