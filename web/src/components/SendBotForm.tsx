import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowRight, Info, Loader2, Settings2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, keys } from '@/lib/api'

const MEET = /^(https:\/\/)?meet\.google\.com\/[a-z]{3}-[a-z]{4}-[a-z]{3}(\?.*)?$/i

export function SendBotForm({ initialUrl = '' }: { initialUrl?: string }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const info = useQuery({ queryKey: keys.info, queryFn: api.info, staleTime: Infinity })
  const [url, setUrl] = useState(initialUrl)
  const [name, setName] = useState('')
  const [showName, setShowName] = useState(false)
  const [touched, setTouched] = useState(false)

  const looksValid = MEET.test(url.trim())
  const send = useMutation({
    mutationFn: () => api.create(url.trim(), name.trim() || undefined),
    onSuccess: (m) => {
      qc.invalidateQueries({ queryKey: keys.meetings })
      qc.setQueryData(keys.meeting(m.id), m)
      toast.success('Bot is on its way', { description: 'Admit it from the lobby when it asks to join.' })
      navigate({ to: '/meetings/$id', params: { id: m.id } })
    },
    onError: (e: Error) => toast.error('Could not send the bot', { description: e.message }),
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        setTouched(true)
        if (url.trim()) send.mutate()
      }}
      className="space-y-3"
    >
      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="relative flex-1">
          <Label htmlFor="meeting-url" className="sr-only">Google Meet link</Label>
          <Input
            id="meeting-url"
            autoFocus
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="https://meet.google.com/abc-defg-hij"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onBlur={() => setTouched(true)}
            aria-invalid={touched && url !== '' && !looksValid}
            className="h-11 pl-3.5 text-[15px]"
          />
        </div>
        <Button type="submit" size="lg" disabled={send.isPending || !url.trim()} className="sm:w-auto">
          {send.isPending ? <Loader2 className="animate-spin" /> : <ArrowRight />}
          Send bot
        </Button>
      </div>
      {touched && url !== '' && !looksValid && (
        <p className="text-sm text-amber-600 dark:text-amber-400">That doesn't look like a Google Meet link. It should look like meet.google.com/abc-defg-hij.</p>
      )}

      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <Info className="size-4" />
          The host will need to admit the bot from the lobby.
        </span>
        <button type="button" onClick={() => setShowName((v) => !v)} className="inline-flex items-center gap-1.5 hover:text-foreground cursor-pointer">
          <Settings2 className="size-4" />
          Bot name: <span className="font-medium text-foreground">{name || info.data?.bot_name || 'Meeting Agent (Notetaker)'}</span>
        </button>
      </div>
      {showName && (
        <div className="max-w-sm animate-fade-in space-y-1.5">
          <Label htmlFor="bot-name">Display name in the meeting</Label>
          <Input id="bot-name" maxLength={60} placeholder={info.data?.bot_name} value={name} onChange={(e) => setName(e.target.value)} />
        </div>
      )}
    </form>
  )
}
