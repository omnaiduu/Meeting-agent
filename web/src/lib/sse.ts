import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { keys } from './api'
import { isTerminal, type Meeting, type Segment, type StatusEvent, type Summary } from '@/types'

export type Connection = 'connecting' | 'live' | 'reconnecting' | 'closed'

/**
 * Subscribes to /api/meetings/:id/events and writes every event into the
 * TanStack Query cache, so components just read the meeting and transcript
 * queries. EventSource reconnects by itself and sends Last-Event-ID, so the
 * server replays only the transcript lines we missed.
 */
export function useMeetingEvents(id: string, enabled: boolean): Connection {
  const qc = useQueryClient()
  const [conn, setConn] = useState<Connection>('connecting')

  useEffect(() => {
    if (!enabled) {
      setConn('closed')
      return
    }
    const es = new EventSource(`/api/meetings/${id}/events`)
    setConn('connecting')
    es.onopen = () => setConn('live')
    es.onerror = () => setConn(es.readyState === EventSource.CLOSED ? 'closed' : 'reconnecting')

    es.addEventListener('status', (e) => {
      const s: StatusEvent = JSON.parse((e as MessageEvent).data)
      qc.setQueryData<Meeting>(keys.meeting(id), (m) =>
        m ? { ...m, status: s.status, error_code: s.error_code ?? m.error_code, error_message: s.error_message ?? m.error_message } : m,
      )
      if (s.status === 'in_call' || isTerminal(s.status)) {
        // Fetch fields the event doesn't carry (joined_at, end_reason, has_audio).
        qc.invalidateQueries({ queryKey: keys.meeting(id) })
        qc.invalidateQueries({ queryKey: keys.meetings })
      }
      if (isTerminal(s.status)) {
        es.close()
        setConn('closed')
      }
    })
    es.addEventListener('transcript', (e) => {
      const seg: Segment = JSON.parse((e as MessageEvent).data)
      qc.setQueryData<Segment[]>(keys.transcript(id), (old = []) =>
        old.some((s) => s.id === seg.id) ? old : [...old, seg].sort((a, b) => a.start_ms - b.start_ms || a.id - b.id),
      )
    })
    es.addEventListener('summary', (e) => {
      const summary: Summary = JSON.parse((e as MessageEvent).data)
      qc.setQueryData<Meeting>(keys.meeting(id), (m) => (m ? { ...m, summary, title: summary.title } : m))
    })
    es.addEventListener('error', (e) => {
      const data = (e as MessageEvent).data
      if (!data) return // connection error, handled by onerror
      const w = JSON.parse(data) as { message: string }
      toast.warning(w.message)
    })
    return () => es.close()
  }, [id, enabled, qc])

  return conn
}
