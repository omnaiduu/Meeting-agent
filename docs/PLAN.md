# Meeting Agent — Project Plan

> A self-built bot that **joins a Google Meet, captures the call audio, transcribes it live, and writes a summary** when the meeting ends — with a clean web dashboard to send bots and watch results.
>
> Written in **Go** (backend + bot) and **React** (dashboard). This document is the source of truth for design decisions and the build order. Any agent or contributor picking up the work should read it end to end first.

---

## Table of contents

1. [Goals and non-goals](#1-goals-and-non-goals)
2. [Decisions at a glance](#2-decisions-at-a-glance)
3. [Architecture](#3-architecture)
4. [How the bot joins Google Meet](#4-how-the-bot-joins-google-meet)
5. [How audio capture works](#5-how-audio-capture-works)
6. [Transcription and summary](#6-transcription-and-summary)
7. [Backend design (Go)](#7-backend-design-go)
8. [Data model (SQLite)](#8-data-model-sqlite)
9. [HTTP API and live updates](#9-http-api-and-live-updates)
10. [Frontend design (React)](#10-frontend-design-react)
11. [Repository layout](#11-repository-layout)
12. [Configuration](#12-configuration)
13. [Running locally and containerizing](#13-running-locally-and-containerizing)
14. [Milestones (build order)](#14-milestones-build-order)
15. [Risks and open questions](#15-risks-and-open-questions)
16. [Background research](#16-background-research)

---

## 1. Goals and non-goals

### Goals (v1)
- Paste a **Google Meet** link in the dashboard → a bot joins as a **guest** named e.g. "Meeting Agent (Notetaker)".
- The host admits it from the lobby; the bot records the call audio.
- A **live transcript** appears in the dashboard while the meeting runs.
- When the meeting ends, the dashboard shows a **summary, key decisions, and action items**.
- Runs on a **laptop** (single Go binary + Chrome), and also as a **Docker container**.
- Portfolio quality: clean code, tests, README, architecture diagram, demo video.

### Non-goals (for now)
- ❌ Speaker identification ("who said what") — explicitly deferred.
- ❌ Zoom and Microsoft Teams — Meet only; the code is shaped so they can be added as new adapters.
- ❌ Calendar auto-join, multi-user accounts/auth, Notion/email export.
- ❌ Bot-detection evasion. The bot uses an honest name, joins as a normal guest, and must be admitted by the host.

---

## 2. Decisions at a glance

| Area | Decision | Why |
|---|---|---|
| Platform | **Google Meet only** | Most common for demos; others later via adapter interface |
| Approach | **Own bot**: real Chrome joins the call | Full control, no per-hour vendor fees, strong portfolio piece |
| Backend language | **Go** (1.26) | Single small binary, low memory, goroutines for concurrent bots |
| Browser control | **chromedp** | Pure Go (no Node), actively maintained (v0.24.0, Nov 2026), has `ExposeFunc` to call Go from the page |
| Browser | **Real Google Chrome, headed, on Xvfb** (pinned version in Docker) | Meet blocks headless Chrome; Xvfb is an in-memory fake screen. Lightweight engines (Lightpanda) have no WebRTC |
| Audio capture | **Injected JS** hooks `RTCPeerConnection`, mixes remote audio, sends PCM to Go | No PulseAudio/ffmpeg needed; lighter |
| Speech-to-text | **Groq Whisper** (`whisper-large-v3-turbo`) | Fast, cheap |
| Summary LLM | **Groq** (model set via env, e.g. `openai/gpt-oss-120b` or `llama-3.3-70b-versatile`) | Fast, cheap; behind an interface so it can be swapped |
| HTTP router | **Go standard library** `net/http` (1.22+ patterns) | `GET /api/meetings/{id}` works natively, no dependency |
| Live updates | **SSE** (Server-Sent Events) | Updates only flow server → browser; auto-reconnect; simpler than WebSocket |
| Database | **SQLite** via `modernc.org/sqlite` (pure Go, no CGO) | Zero setup, one file; easy move to Postgres later |
| DB code | **sqlc** (typed Go generated from SQL) + **goose** migrations | Plain SQL, type-safe, idiomatic Go |
| Logging | **log/slog** | Built in, structured |
| Bot execution | **In-process: one goroutine per meeting**, each with its own Chrome, behind a `Runner` interface | Simplest for now; can swap to container-per-meeting later without rewrites |
| Frontend | **Vite + React + TypeScript + TanStack Router (file-based) + TanStack Query** | SPA served by Go; type-safe routing |
| UI | **Tailwind CSS v4 + shadcn/ui** + lucide icons | Clean, modern, widely used |
| Frontend delivery | Built files **embedded into the Go binary** (`embed`) | Ship one binary |
| Bot login | **Guest first**; signed-in Google account is a fallback if guest join gets blocked | Decide later once we see how guest join behaves |
| Runs on | **Laptop first**, then Docker container (Linux) | Develop fast, then package |

> **Why TanStack Router and not TanStack Start?** Start is a full-stack framework with its own Node server (SSR, server functions). Our server is Go, so we only need the client-side router. TanStack Router gives the same type-safe, file-based routing as a pure SPA that Go can embed and serve. If SSR is ever wanted, the routes migrate to Start with little change.

---

## 3. Architecture

```
┌──────────────────────────────── single Go binary ────────────────────────────────┐
│                                                                                   │
│   React SPA (embedded)  ◄── GET /*                                                │
│                                                                                   │
│   HTTP API (net/http) ── REST /api/... ──► Meeting service ──► SQLite (sqlc)      │
│        │                                        │                                 │
│        └── SSE /api/meetings/{id}/events ◄── Event hub (pub/sub per meeting)      │
│                                                 ▲                                 │
│                                                 │ status, transcript lines        │
│                                    ┌────────────┴────────────┐                    │
│                                    │   Bot Manager (Runner)  │                    │
│                                    │  goroutine per meeting  │                    │
│                                    └────────────┬────────────┘                    │
│                                                 │ chromedp (CDP)                  │
└─────────────────────────────────────────────────┼─────────────────────────────────┘
                                                  ▼
                         Google Chrome (headed, on Xvfb display :99)
                           └─ meet.google.com tab
                                └─ injected JS: hook RTCPeerConnection → mix audio
                                   → AudioWorklet → 16 kHz PCM → ExposeFunc → Go
                                                  │
                     Go audio buffer (~20 s) → WAV → Groq Whisper → transcript segment
                     Meeting ends → full transcript → Groq LLM → summary JSON
```

### Request flow (happy path)
1. User submits a Meet URL → `POST /api/meetings` → row created with status `queued`.
2. Meeting service asks the **Bot Manager** to start → goroutine launches Chrome → status `launching`.
3. Bot opens the Meet URL, enters a name, clicks **Ask to join** → `joining` → `in_lobby`.
4. Host admits → `in_call`. Audio capture starts; every ~20 s a chunk is transcribed and a `transcript` event is pushed over SSE.
5. Meeting ends (bot alone / removed / max duration) → `leaving` → Chrome closes.
6. Remaining audio is flushed → `transcribing` (final chunk) → `summarizing` → `done`.
7. On any failure → `failed` with an `error_code` (`denied`, `lobby_timeout`, `join_blocked`, `chrome_crash`, `stt_error`, …).

---

## 4. How the bot joins Google Meet

### Browser launch (chromedp `ExecAllocator`)
- Use the system/pinned **Google Chrome** binary (`CHROME_PATH`), **headed** (`headless=false`), on an X display (`DISPLAY=:99` via Xvfb on Linux; on macOS/Windows laptop dev, it simply opens a real window).
- Start from `chromedp.DefaultExecAllocatorOptions` but **remove `headless`**.
- Flags to add:
  - `--autoplay-policy=no-user-gesture-required` (AudioContext can start without a click)
  - `--window-size=1280,720`
  - `--lang=en-US` (stable button labels for selectors)
  - A **fresh temp `--user-data-dir` per meeting** (isolation; deleted after)
- Deny camera/mic permissions (the bot never sends media). Do **not** use `--use-fake-device-for-media-stream`.
- Keep the flag set minimal and honest; no fingerprint spoofing.

### Join flow (state machine steps)
| Step | Action | Detection (prefer `aria-label`/role/text over CSS class names — Meet's class names change often) |
|---|---|---|
| 1. Open | Navigate to the Meet URL | Page loaded |
| 2. Pre-join | Dismiss any "Got it" / sign-in prompts; ensure mic & camera are **off** | Mic/camera toggle `aria-label` |
| 3. Name | Type bot name into the **"Your name"** input | Input with placeholder/aria "Your name" |
| 4. Ask | Click **"Ask to join"** | Button text |
| 5. Lobby | Wait (default max 10 min) | Text like "Asking to be let in…" → status `in_lobby` |
| 6. Admitted | — | **"Leave call"** button appears → status `in_call` |
| 7. Denied | — | Text like "You can't join this call" / "denied your request" → `failed: denied` |
| 8. Blocked | — | "You can't join this video call" before lobby → `failed: join_blocked` |

All selectors live in **one file** (`internal/bot/meet/selectors.go`) so they're easy to update when Meet changes.

### Leave conditions (checked every 5 s while `in_call`)
- **Alone**: participant count (from the people/participant indicator) is 1 for `ALONE_TIMEOUT` (default 2 min).
- **Removed / meeting ended**: "You've been removed" / "The call has ended" / "Leave call" button disappears.
- **Max duration**: `MAX_MEETING_DURATION` (default 2 h).
- **User stop**: `POST /api/meetings/{id}/stop`.

On leave: click **Leave call** (best effort), cancel the chromedp context, kill Chrome, delete the temp profile.

---

## 5. How audio capture works

Injected with `page.AddScriptToEvaluateOnNewDocument` **before** Meet's own scripts run (`internal/bot/meet/inject/capture.js`, embedded with `//go:embed`).

1. **Hook WebRTC**: wrap `window.RTCPeerConnection`; for every new connection, listen to `track` events.
2. **Collect remote audio**: for each `track.kind === "audio"`, wrap it in a `MediaStream` and connect it via `AudioContext.createMediaStreamSource` into one shared **GainNode** (the mix).
   - Chrome quirk: remote WebRTC audio may not flow into Web Audio unless the stream is also attached to a (muted) `<audio>` element. Attach one per stream.
3. **Downsample**: mix → **AudioWorkletNode** (`pcm-worklet`) converts float32 @ 48 kHz → **Int16 mono @ 16 kHz** (what Whisper wants). The worklet code is loaded from a Blob URL so it doesn't need a network fetch.
4. **Batch**: main thread collects ~1 s of PCM (32 KB), base64-encodes it, and calls `window.__meetAgentAudio(b64, seq)`.
5. **Bridge to Go**: `__meetAgentAudio` is created by chromedp **`ExposeFunc`**; Go decodes and appends to the meeting's audio buffer. (~43 KB/s of base64 — trivial.)
6. **Track lifecycle**: when tracks end (people leave), disconnect their source nodes. Meet reuses a small number of audio channels for the current speakers, so new tracks can appear at any time — the hook must handle late tracks.

Go side (`internal/audio`):
- `Buffer` accumulates PCM; every `CHUNK_SECONDS` (default 20 s, minimum ~10 s because Groq bills at least 10 s per request) it emits a chunk with its **start offset** in the meeting.
- `wav.Encode(pcm, 16000, 1)` → WAV bytes for upload.
- The full recording is also appended to `data/meetings/{id}/audio.wav` (header patched on close) so a meeting can be re-transcribed later.
- Silence detection (RMS threshold): skip sending all-silent chunks to save cost.

---

## 6. Transcription and summary

### Speech-to-text (`internal/transcribe`)
```go
type Transcriber interface {
    Transcribe(ctx context.Context, wav []byte, lang string) (Result, error) // text + segments with timestamps
}
```
- Implementation: **Groq** OpenAI-compatible endpoint `POST https://api.groq.com/openai/v1/audio/transcriptions`, multipart, `model=whisper-large-v3-turbo`, `response_format=verbose_json` (gives segment timestamps).
- Segment times are shifted by the chunk's start offset so the transcript has meeting-relative timestamps.
- A **worker pool** (small, e.g. 2 goroutines per meeting) processes chunks in order; retries with backoff on 429/5xx.
- Each finished segment → saved to DB → published on the event hub → SSE `transcript` event.

### Summary (`internal/summarize`)
```go
type Summarizer interface {
    Summarize(ctx context.Context, transcript string, meta MeetingMeta) (Summary, error)
}
```
- Implementation: **Groq** chat completions (`/openai/v1/chat/completions`), model from `GROQ_SUMMARY_MODEL`, **JSON mode** with this shape:
```json
{
  "title": "Q4 launch sync",
  "overview": "3–5 sentence summary",
  "key_points": ["..."],
  "decisions": ["..."],
  "action_items": [{ "task": "...", "owner": "name or null", "due": "text or null" }],
  "open_questions": ["..."]
}
```
- Long meetings: if the transcript exceeds the model's context budget, summarize in sections (map) then merge (reduce).
- Use plain `net/http` for both clients (small, no SDK needed). Keep a `fake` implementation of each interface for tests and for running the UI without API keys.

---

## 7. Backend design (Go)

### Core types
```go
type Status string // queued, launching, joining, in_lobby, in_call, leaving,
                   // transcribing, summarizing, done, failed

// Runner starts and stops bots. In-process today; container-per-meeting later.
type Runner interface {
    Start(ctx context.Context, m Meeting) error
    Stop(meetingID string) error
}

// Platform adapter — Meet now, Zoom/Teams later.
type Platform interface {
    Join(ctx context.Context, page Page, opts JoinOptions) error   // returns when admitted
    WaitUntilEnded(ctx context.Context, page Page) (EndReason, error)
    Leave(ctx context.Context, page Page) error
}
```

### Bot Manager (in-process runner)
- Map of `meetingID → *botSession` guarded by a mutex; each session runs in its **own goroutine** with its own `context.Context` (cancel = stop).
- **Concurrency limit** (`MAX_CONCURRENT_BOTS`, default 2 on a laptop) via a semaphore; extra meetings stay `queued`.
- Each session: launch Chrome → `Platform.Join` → start capture → `WaitUntilEnded` → `Leave` → flush audio → summarize. Every status change goes through one function that **writes to DB and publishes to the event hub**.
- `recover()` in each goroutine so one crash marks that meeting `failed` without taking down the server.
- On server start: any meeting left in a non-terminal status (from a crash) is marked `failed: interrupted`.
- Graceful shutdown (SIGINT/SIGTERM): stop accepting work, signal all bots to leave, wait up to 30 s.

### Event hub
- In-memory pub/sub: `Subscribe(meetingID) (<-chan Event, unsubscribe)`; `Publish(meetingID, Event)`.
- Non-blocking sends with a buffered channel per subscriber; drop slow subscribers rather than block bots.
- Events: `status`, `transcript`, `summary`, `error`.

### Conventions
- Standard library `net/http` + small hand-written middleware (request logging with slog, panic recovery, CORS only in dev).
- Errors wrapped with `%w`; API returns `{ "error": { "code": "...", "message": "..." } }`.
- Table-driven unit tests; `go vet` + `staticcheck` (or `golangci-lint`) in CI.

---

## 8. Data model (SQLite)

Migrations in `internal/store/migrations/*.sql` (goose), queries in `internal/store/queries/*.sql` (sqlc). Enable `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`.

```sql
CREATE TABLE meetings (
  id            TEXT PRIMARY KEY,          -- ULID
  platform      TEXT NOT NULL DEFAULT 'google_meet',
  meeting_url   TEXT NOT NULL,
  meeting_code  TEXT NOT NULL,             -- e.g. abc-defg-hij
  bot_name      TEXT NOT NULL,
  status        TEXT NOT NULL,
  error_code    TEXT,
  error_message TEXT,
  created_at    DATETIME NOT NULL,
  joined_at     DATETIME,
  ended_at      DATETIME,
  audio_path    TEXT
);

CREATE TABLE transcript_segments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  meeting_id  TEXT NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
  start_ms    INTEGER NOT NULL,            -- relative to joined_at
  end_ms      INTEGER NOT NULL,
  text        TEXT NOT NULL,
  created_at  DATETIME NOT NULL
);
CREATE INDEX idx_segments_meeting ON transcript_segments(meeting_id, start_ms);

CREATE TABLE summaries (
  meeting_id  TEXT PRIMARY KEY REFERENCES meetings(id) ON DELETE CASCADE,
  model       TEXT NOT NULL,
  content     TEXT NOT NULL,               -- JSON (shape in §6)
  created_at  DATETIME NOT NULL
);
```

---

## 9. HTTP API and live updates

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/meetings` | Body `{ "meeting_url": "...", "bot_name": "optional" }` → validate it's a Meet URL → create + start bot → `201` with meeting |
| `GET` | `/api/meetings` | List meetings (newest first) |
| `GET` | `/api/meetings/{id}` | Meeting + summary (if any) |
| `GET` | `/api/meetings/{id}/transcript` | All segments |
| `POST` | `/api/meetings/{id}/stop` | Ask the bot to leave now |
| `DELETE` | `/api/meetings/{id}` | Delete meeting, transcript, audio |
| `GET` | `/api/meetings/{id}/audio` | Download the WAV |
| `GET` | `/api/meetings/{id}/events` | **SSE** stream |
| `GET` | `/api/health` | Liveness |
| `GET` | `/*` | Embedded SPA (fallback to `index.html`) |

### SSE stream
```
event: status
data: {"status":"in_lobby","at":"2026-11-20T10:00:05Z"}

event: transcript
data: {"id":42,"start_ms":61000,"end_ms":64800,"text":"Let's ship on Friday."}

event: summary
data: { ...summary JSON... }
```
- On connect, the server first sends the **current status** (and supports `Last-Event-ID` = last segment id to replay missed transcript lines), then live events.
- Heartbeat comment (`: ping`) every 15 s to keep proxies from closing the stream.
- Set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, and flush after every event (`http.ResponseController`).

---

## 10. Frontend design (React)

### Stack
- **Vite + React 19 + TypeScript**
- **TanStack Router** (file-based routes, type-safe params) + **TanStack Query** (fetching/caching; SSE events update the query cache)
- **Tailwind CSS v4 + shadcn/ui** components, **lucide-react** icons, **sonner** toasts
- Font: **Inter** (UI) + **JetBrains Mono** (timestamps)
- Light + dark mode (follows system; toggle in header)
- Dev: Vite dev server proxies `/api` → Go on `:8080`. Prod: `vite build` → `web/dist` → embedded in Go.

### Routes
| Route | Page |
|---|---|
| `/` | Dashboard: "Send a bot" card + recent meetings |
| `/meetings/$id` | Meeting detail: status, live transcript, summary |

### Layout — Dashboard (`/`)
```
┌──────────────────────────────────────────────────────────────────┐
│  ◉ Meeting Agent                                   [☀/☾]          │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│   Send a notetaker to your meeting                               │
│   ┌──────────────────────────────────────────────┐ ┌──────────┐  │
│   │ https://meet.google.com/abc-defg-hij         │ │ Send bot │  │
│   └──────────────────────────────────────────────┘ └──────────┘  │
│   Bot name: [ Meeting Agent (Notetaker) ]                        │
│   ℹ The host will need to admit the bot from the lobby.          │
│                                                                  │
│   Recent meetings                                                │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ ● In call     abc-defg-hij   Started 10:02   12 min    →  │   │
│   │ ✓ Done        Q4 launch sync  Yesterday       45 min   →  │   │
│   │ ✕ Failed      xyz-abcd-efg   Denied by host            →  │   │
│   └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

### Layout — Meeting detail (`/meetings/$id`)
```
┌──────────────────────────────────────────────────────────────────┐
│  ← Back   Q4 launch sync        ● In call · 12:41    [Stop bot]  │
│  Queued ─ Joining ─ Lobby ─ ●In call ─ Summarizing ─ Done        │  ← status stepper
├──────────────────────────────────────┬───────────────────────────┤
│  Live transcript         [⤓ Copy]    │  Summary                  │
│                                      │  (skeleton until done)    │
│  00:01:01  Let's ship on Friday.     │  Overview                 │
│  00:01:24  We still need QA sign-off │  ...                      │
│  00:01:40  ...                       │  Decisions                │
│                                      │  • ...                    │
│  ▼ auto-scroll (pauses when user     │  Action items             │
│    scrolls up; "Jump to live" pill)  │  ☐ task — owner — due     │
│                                      │  Open questions           │
│                                      │  [Copy as Markdown]       │
└──────────────────────────────────────┴───────────────────────────┘
```
- On mobile, the two columns become **tabs** (Transcript | Summary).
- Status badge colors: neutral (queued), blue (joining/lobby, pulsing dot), green (in call, pulsing), violet (transcribing/summarizing), green check (done), red (failed + human-readable reason and "Try again").
- Empty, loading (skeletons), and error states for every view.

### Frontend structure
```
web/
  src/
    routes/            __root.tsx, index.tsx, meetings.$id.tsx
    components/        StatusBadge, StatusStepper, TranscriptView, SummaryPanel,
                       SendBotForm, MeetingList, ThemeToggle, ui/ (shadcn)
    lib/               api.ts (typed fetch), sse.ts (useMeetingEvents hook), format.ts
    types.ts           Meeting, Segment, Summary, Status (mirror Go JSON)
```

---

## 11. Repository layout

```
Meeting-agent/
├── cmd/
│   └── server/main.go            # wires config, store, hub, bot manager, HTTP; graceful shutdown
├── internal/
│   ├── config/                   # env parsing + defaults
│   ├── api/                      # handlers, SSE, middleware, SPA serving
│   ├── meeting/                  # meeting service: create/stop/list, status transitions
│   ├── events/                   # in-memory pub/sub hub
│   ├── bot/
│   │   ├── manager.go            # Runner impl: goroutine per meeting, semaphore
│   │   ├── browser.go            # chromedp allocator / Chrome launch
│   │   └── meet/
│   │       ├── meet.go           # Platform impl: Join, WaitUntilEnded, Leave
│   │       ├── selectors.go      # ALL Meet selectors/texts in one place
│   │       └── inject/
│   │           ├── capture.js    # RTCPeerConnection hook + mixing
│   │           └── pcm-worklet.js
│   ├── audio/                    # PCM buffer, chunking, WAV encode, silence check
│   ├── transcribe/               # Transcriber interface, groq.go, fake.go
│   ├── summarize/                # Summarizer interface, groq.go, fake.go, prompt.go
│   └── store/
│       ├── migrations/           # goose .sql
│       ├── queries/              # sqlc .sql
│       └── db/                   # sqlc-generated code
├── web/                          # Vite React app (see §10)
├── deploy/
│   ├── Dockerfile                # multi-stage: build web → build Go → runtime w/ Chrome + Xvfb
│   └── entrypoint.sh             # start Xvfb :99, then exec server
├── docs/
│   └── PLAN.md                   # this file
├── sqlc.yaml
├── Makefile                      # dev, build, test, lint, sqlc, migrate, docker
├── .env.example
├── go.mod
└── README.md
```

---

## 12. Configuration

All via environment variables (load `.env` in dev).

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `:8080` | |
| `DATA_DIR` | `./data` | SQLite file + audio recordings |
| `GROQ_API_KEY` | — | Required for real STT/summary (use `fake` providers without it) |
| `GROQ_STT_MODEL` | `whisper-large-v3-turbo` | Verify current model IDs on Groq's models list |
| `GROQ_SUMMARY_MODEL` | `openai/gpt-oss-120b` | Any Groq chat model with JSON mode |
| `STT_PROVIDER` / `SUMMARY_PROVIDER` | `groq` | `fake` for offline dev/tests |
| `CHROME_PATH` | auto-detect | Pin in Docker |
| `BOT_NAME` | `Meeting Agent (Notetaker)` | Default display name |
| `MAX_CONCURRENT_BOTS` | `2` | |
| `LOBBY_TIMEOUT` | `10m` | |
| `ALONE_TIMEOUT` | `2m` | |
| `MAX_MEETING_DURATION` | `2h` | |
| `CHUNK_SECONDS` | `20` | ≥10 |
| `LOG_LEVEL` | `info` | |

---

## 13. Running locally and containerizing

### Laptop (dev)
```bash
make dev        # runs Go server (air or go run) + Vite dev server
```
- macOS/Windows: Chrome opens as a visible window — handy for debugging the join flow.
- Linux without a desktop: run under `xvfb-run -a go run ./cmd/server`.

### Docker
Multi-stage `deploy/Dockerfile`:
1. `node:22` → `npm ci && npm run build` (web)
2. `golang:1.26` → `CGO_ENABLED=0 go build` (embeds `web/dist`)
3. Runtime `debian:bookworm-slim` + **pinned Google Chrome stable** + `xvfb` + fonts (`fonts-noto`, `fonts-liberation`) → entrypoint starts `Xvfb :99` then the server.
- Run with `--shm-size=1g` (Chrome needs shared memory).
- Volume for `/data`.
- Expect **~0.5–1 GB RAM and ~1 vCPU per active bot**.

---

## 14. Milestones (build order)

Each milestone ends with something runnable and a commit.

### M0 — Skeleton
- [x] `go mod init`, folders per §11, Makefile, `.env.example`, slog setup, config parsing
- [x] `GET /api/health`
- [x] GitHub Actions: `go build`, `go test`, `go vet`
- **Done when:** `make dev` starts the server and health returns 200.

### M1 — Bot joins a Meet (the riskiest part — do it first)
- [ ] `bot/browser.go`: launch headed Chrome with chromedp (temp profile, flags from §4)
- [ ] `bot/meet`: join flow + lobby wait + admitted/denied/blocked detection; selectors in one file
- [ ] Leave conditions (alone / removed / max duration / stop)
- [ ] A tiny CLI for manual testing: `go run ./cmd/joinmeet -url https://meet.google.com/...`
- [ ] Screenshot on failure saved to `data/debug/` for diagnosis
- **Done when:** the bot appears in a real Meet as a guest, is admitted, stays, and leaves when everyone else leaves.
- **Learn here:** whether guest join is accepted from a laptop and from Docker/Xvfb. If blocked → evaluate the signed-in-account fallback (§15).

### M2 — Audio capture
- [ ] `inject/capture.js` + `pcm-worklet.js`, injected before page load
- [ ] `ExposeFunc` bridge → `audio.Buffer` → `audio.wav` on disk
- **Done when:** after a test call, `data/meetings/{id}/audio.wav` plays back the other participants clearly.

### M3 — Transcription
- [ ] `transcribe/groq.go` + `fake.go`, chunking every 20 s, ordered worker, retries
- [ ] Segments saved to SQLite (sqlc + goose set up here)
- **Done when:** transcript segments with correct meeting-relative timestamps are in the DB during a live call.

### M4 — API, SSE, meeting service
- [ ] Meeting service + Bot Manager (goroutine per meeting, semaphore, recover, startup cleanup)
- [ ] REST endpoints + SSE stream + event hub (§9)
- **Done when:** `curl -N /api/meetings/{id}/events` shows status changes and live transcript lines.

### M5 — Summary
- [ ] `summarize/groq.go` with JSON mode + map/reduce for long transcripts
- [ ] `summarizing → done` transition, `summary` SSE event
- **Done when:** finished meetings have a structured summary in the DB.

### M6 — Dashboard
- [ ] Vite + React + TS + TanStack Router/Query + Tailwind v4 + shadcn/ui
- [ ] Dashboard page, meeting detail page, SSE hook, dark mode, responsive layout (§10)
- [ ] Embed `web/dist` in the Go binary
- **Done when:** the whole flow works from the browser: paste link → watch live transcript → read summary.

### M7 — Containerize and polish
- [ ] Dockerfile + entrypoint (Xvfb), `docker compose up`
- [ ] README: what it is, architecture diagram, setup, screenshots/GIF, design decisions (link to this file)
- [ ] Unit tests: state machine, audio buffer/WAV, SSE hub, API handlers (with fake providers)
- [ ] Record a 2-minute demo video
- **Done when:** a fresh machine with Docker can run it with one command plus a `GROQ_API_KEY`.

### Later (not now)
- Speaker identification (active-speaker DOM indicator + timestamps; Meet captions as backup)
- Signed-in bot account, Zoom (requires Zoom app + OBF token since March 2026), Teams (guest web join)
- Container-per-meeting `Runner`, Postgres, auth, calendar auto-join, Notion/email export, "ask this meeting" chat

---

## 15. Risks and open questions

| Risk | Impact | Mitigation |
|---|---|---|
| **Meet blocks the guest join** ("You can't join this video call") | Bot can't enter | Headed real Chrome (not headless), minimal flags, honest name. Fallback: sign the bot into a dedicated Google account (saved profile). Decide after M1. |
| **Meet UI/selector changes** | Join or end detection breaks | All selectors in `selectors.go`; prefer aria-labels/text; failure screenshots |
| **Org-only meetings** (Workspace "only people in org") | Guests can't join at all | Document as a known limitation |
| **No audio output device in container** — Web Audio may not process without one | Silent recordings | Verify in M2 under Docker; fallback: install PulseAudio with a null sink in the image |
| **Remote audio not flowing into Web Audio** | Silent recordings | Attach each remote stream to a muted `<audio>` element (known Chrome quirk) |
| **Groq rate limits / model ID changes** | STT/summary failures | Retries + backoff, models via env; check Groq's current models list. (Note: the Groq key in the current cloud environment returned "expired API key" — use your own key locally.) |
| **Chrome memory** | Few concurrent bots on a laptop | `MAX_CONCURRENT_BOTS`; small window; no video sent |
| **Consent / legal** | Recording without consent is illegal in many places | Clear bot name; README tells users to inform participants; host must admit the bot |

Open questions (decide later):
1. Guest vs signed-in Google account for the bot (after M1 results).
2. Hosting for the public demo (VPS vs Fly.io/Railway).
3. Whether to keep full audio recordings or delete after transcription (privacy).

---

## 16. Background research

Summary of research done before writing this plan (Nov 2026):

- **Meeting-bot vendors** (Recall.ai, MeetingBaaS, Skribby, Attendee, Vexa) charge ~$0.30–0.65 per bot-hour. We build our own instead.
- **Open-source references**: Vexa (Apache-2.0, Meet/Teams/Zoom bots), meeto (Python Meet bot; notes Meet blocks headless and recommends Xvfb), TeamsRecorder (Teams guest-join bot), various Meet bots using an `RTCPeerConnection` hook to capture remote tracks.
- **Google Meet**: vanilla headless automation is caught by bot detection; working bots use headed real Chrome on Xvfb. The host must admit guests. Meet uses a small number of shared audio channels (not one per participant).
- **Zoom**: since **March 2, 2026**, apps joining meetings outside their own account must be attributed to a user (OBF or ZAK token, or RTMS). Out of scope for v1.
- **Teams**: guests can join via the web client without an account (unless the tenant disables it). Good second platform.
- **Lightweight browsers**: Lightpanda has **no WebRTC/audio** → can't join calls. Vercel's agent-browser is a CLI that drives Chrome (not lighter). Cloud browsers (Browser Use, Browserbase, Steel) run full Chrome remotely — a possible later option.
- **Go CDP libraries**: chromedp is actively maintained (v0.21.1, Nov 2026; added `ExposeFunc`); go-rod has had no release since July 2024; playwright-go needs a Node.js driver.
