// Mirrors the Go JSON types in internal/meeting and internal/summarize.

export type Status =
  | 'queued' | 'launching' | 'joining' | 'in_lobby' | 'in_call'
  | 'leaving' | 'transcribing' | 'summarizing' | 'done' | 'failed'

export interface ActionItem { task: string; owner: string | null; due: string | null }

export interface Summary {
  title: string
  overview: string
  key_points: string[]
  decisions: string[]
  action_items: ActionItem[]
  open_questions: string[]
}

export interface Meeting {
  id: string
  platform: string
  meeting_url: string
  meeting_code: string
  bot_name: string
  title: string | null
  status: Status
  error_code: string | null
  error_message: string | null
  end_reason: 'alone' | 'meeting_over' | 'max_duration' | 'stopped' | null
  created_at: string
  joined_at: string | null
  ended_at: string | null
  has_audio: boolean
  summary?: Summary
  summary_model?: string
}

export interface Segment { id: number; start_ms: number; end_ms: number; text: string }

export interface StatusEvent { status: Status; at: string; error_code?: string; error_message?: string }

export interface Info { bot_name: string; stt_provider: string; summary_provider: string }

export const TERMINAL: Status[] = ['done', 'failed']
export const isTerminal = (s: Status) => TERMINAL.includes(s)
