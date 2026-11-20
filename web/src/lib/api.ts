import type { Info, Meeting, Segment } from '@/types'

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message)
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } })
  if (res.status === 204) return undefined as T
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new ApiError(res.status, body?.error?.code ?? 'error', body?.error?.message ?? `Request failed (${res.status})`)
  }
  return body as T
}

export const api = {
  info: () => req<Info>('/api/info'),
  list: () => req<{ meetings: Meeting[] }>('/api/meetings').then((r) => r.meetings),
  get: (id: string) => req<Meeting>(`/api/meetings/${id}`),
  transcript: (id: string) => req<{ segments: Segment[] }>(`/api/meetings/${id}/transcript`).then((r) => r.segments),
  create: (meeting_url: string, bot_name?: string) =>
    req<Meeting>('/api/meetings', { method: 'POST', body: JSON.stringify({ meeting_url, bot_name }) }),
  stop: (id: string) => req<void>(`/api/meetings/${id}/stop`, { method: 'POST' }),
  remove: (id: string) => req<void>(`/api/meetings/${id}`, { method: 'DELETE' }),
  audioUrl: (id: string) => `/api/meetings/${id}/audio`,
}

export const keys = {
  info: ['info'] as const,
  meetings: ['meetings'] as const,
  meeting: (id: string) => ['meeting', id] as const,
  transcript: (id: string) => ['transcript', id] as const,
}
