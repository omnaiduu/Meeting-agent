# Limitations and fragile parts

An honest list of what can break and what's approximate. Read this before trusting the bot in a meeting that matters.

---

## 1. The bot reads Google Meet's screen (hard-coded rules)

Google offers no official way for a bot to join a Meet call as a participant. So this bot, like every bot of its kind (including the paid services), works by **reading Meet's web page**: the visible text and the accessibility labels on buttons. If Google changes those words, the matching rule stops working.

All of these rules live in **one file**: [`internal/bot/meet/selectors.go`](../internal/bot/meet/selectors.go). Chrome is forced to English (`--lang=en-US` plus `?hl=en` on the link), so the English texts below are what Meet should show.

> ⚠️ **Not verified against live Google Meet yet.** These rules come from open-source bots (Vexa's selectors, Nov 2026) and are tested against our own fake Meet page, which copies them. The first real-Meet test may need edits here.

| Rule | What the bot looks for | Used for | If Google changes it |
|---|---|---|---|
| Name box | `input[aria-label="Your name"]`, or any text input whose placeholder or label contains "name" | Typing the bot's name | Bot can't type its name. Join fails after 90 s with `join_timeout` and a screenshot. |
| Join button | Button text "Ask to join", "Join now", "Join anyway", "Join" | Asking to join | Same as above: `join_timeout`. |
| Pop-ups | "Continue without microphone and camera", "Got it", "Dismiss", "No thanks", "Not now" | Closing dialogs that cover the join button | Pop-up stays open and blocks the join button. |
| Lobby | "Asking to be let in", "You'll join the call when someone lets you in", "waiting room", … | Knowing we're waiting | Mostly harmless: the bot keeps waiting, but the dashboard doesn't show "In lobby". |
| Denied | "denied your request", "weren't allowed to join", "Ask to join again" | Stopping with `denied` | Bot waits until the 10-minute lobby timeout instead of failing at once. |
| Blocked | "You can't join this video call", "Meeting not found", "Check your meeting code" | Stopping with `join_blocked` | Bot waits, then times out. |
| **In call** | Participant tiles `[data-participant-id]`, a "Present now" or "Share screen" button, `[data-self-name]`, **or real audio heard** | Knowing we were admitted | The audio signal still works, so a call where people talk is still detected. A silent call might not be. |
| Call ended | "The call has ended", "You've been removed from the meeting", "Meeting ended", "Call ended", "Return to home screen" (only after being in the call) | Leaving when it's over | The bot stays until the "alone" rule or the 2-hour limit fires. |
| Leave button | `button[aria-label="Leave call"]`, then any button labeled "Leave…" | Leaving politely | Bot closes Chrome anyway, so it drops out of the call; it just doesn't click Leave first. |
| People count | A button whose label starts with "People", "Show everyone" or "Participants" and contains a number; or the count of distinct participant tiles | The "alone" rule | Count reads as unknown, so the "alone" rule never fires. Bot leaves at "call ended" or the 2-hour limit. |

**Known traps** (built into the rules on purpose, each copied from a real bug another bot shipped):
- "Leave call" also appears **in the lobby**, so it is never used as proof of admission.
- "Return to home screen" also appears **in the lobby**, so it only means "ended" after the bot was in the call.
- Meet uses curly apostrophes (’). All text is normalized before matching.

**What does *not* depend on Meet's screen:** audio capture (it hooks WebRTC, a web standard), chunking, transcription, summary, database, API, live updates and dashboard.

**How a break shows up:** the meeting fails with a clear code (`join_timeout`, `lobby_timeout`, …), a screenshot is saved in `data/debug/`, and the dashboard shows a plain message.

### Idea: keep the rules fresh automatically (not built)

Meet's UI will change. Instead of waiting for a break, a scheduled agent could keep the rules current:

1. **Daily canary.** A real bot joins a test meeting we control (its own Google account hosts it, and a helper admits it) and records which rules matched at each step: name box, join, lobby, in call, ended.
2. **On a miss or a failure,** save the page's DOM and a screenshot, then start an AI coding agent (for example Claude Code) with them. Its task: update `selectors.go`, run the fake-Meet tests, and open a pull request.
3. **A human reviews and merges.** No automatic changes in production.

Open questions: hosting a test meeting without a human (it needs a Google account and an "admit" step), how often to run, and cost. To be discussed before building.

---

## 2. Transcript timing

**Timestamps are accurate.** Each line's time = the chunk's start + Whisper's time inside the chunk. The chunk's start comes from counting audio samples (16,000 per second), not the wall clock, and silence is recorded too, so time keeps counting while nobody talks.

What is approximate:

| Limitation | Example | Why |
|---|---|---|
| **Lines appear late** (up to about 20 s + 1 s) | Said at 3:04, shown at about 3:21, labeled **03:04** | Audio is sent in about 20-second chunks; Whisper isn't a streaming model, and Groq bills at least 10 s per request. |
| **00:00 is when the bot started hearing audio**, roughly when it was admitted, not when the meeting began | Admitted 10 minutes into the meeting, so the meeting's minute 10 is shown as 00:00 | The bot can't hear anything before it's admitted. |
| **One time per sentence, not per word** | "Okay, decision. Public beta ships Friday." has one start time | Whisper returns segments, not word timings (word timings are possible later). |
| **A lost 1-second batch shifts later times by about 1 s** | Very rare; not seen in tests | If an audio batch from the page never arrives, the bot skips ahead instead of waiting forever. |
| **A chunk can be dropped under heavy backlog** | Only if Groq is down or slow for many minutes | The queue holds 64 chunks (about 20 minutes). After that, chunks are skipped with a warning; later times stay correct. |

---

## 3. Other limitations

- **No speaker names.** Meet mixes voices into a few shared audio channels, so the audio alone can't say who spoke.
- **Organization-only meetings** (Workspace meetings that block guests) can't be joined.
- **Headless Chrome is blocked by Meet,** so production needs a real (headed) Chrome on a screen or on Xvfb. The `CHROME_HEADLESS` option is only for the fake Meet.
- **Datacenter IPs:** Google may treat bots on cloud servers differently from a home connection. Not tested yet.
- **Memory:** about 0.5–1 GB of RAM per active bot (one Chrome each, for isolation). Default limit: 2 at once.
- **Whisper can mishear** names and unclear speech; it may invent phrases on noise (silence is skipped and known phrases like "Thank you." are filtered out, which reduces this).
- **The summary is written by an LLM.** The dashboard says so and points to the transcript for checking.
- **Consent:** recording people without consent is illegal in many places. The bot uses an obvious name and must be admitted by the host.
