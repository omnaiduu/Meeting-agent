# Engineering blog notes

Raw notes for writing the engineering blog post about this project. Each entry is something interesting we found, a problem we hit, or a decision worth explaining. Newest at the bottom. Not polished prose: draft from these.

Tags: **[finding]** something we learned · **[problem]** something that broke and how we fixed it · **[decision]** a choice and why · **[side note]** fun detail worth a sidebar

---

## Before writing code (research)

- **[finding] Meet blocks plain headless Chrome.** Every open-source Meet bot we looked at runs a *headed* Chrome on Xvfb (an in-memory fake screen). That's why the bot needs a "real" browser on a server, not a scraping engine.
- **[finding] "Lightweight browsers" can't do this.** Lightpanda and similar engines skip WebRTC and audio, which is the one thing a meeting bot needs. The browser *is* the heavy part, and you can't avoid it.
- **[finding] Meet does not give one audio track per person.** It sends a few shared audio channels that carry whoever is speaking now. So "who said what" can't come from tracks alone. This is why speaker identification is deferred.
- **[finding] Zoom changed the rules on March 2, 2026.** Apps joining meetings outside their own account must be attributed to a user (OBF/ZAK token or RTMS). A guest-browser bot like ours can't just join Zoom anymore. Meet first, Teams (guest web join still works) second.
- **[side note] Vendor prices.** Recall.ai, MeetingBaaS, Skribby, Attendee and Vexa charge roughly $0.30–0.65 per bot-hour. Building our own costs only compute plus speech-to-text.
- **[decision] Go + chromedp instead of Python/Node + Playwright.** One small binary, goroutine per meeting, and chromedp's `ExposeFunc` lets in-page JavaScript call Go directly. That's our audio pipe. go-rod hasn't released since July 2024, and playwright-go needs a Node driver.
- **[decision] Capture audio inside the page instead of at the OS level.** Most bots route audio through PulseAudio and record with ffmpeg. We hook `RTCPeerConnection` in the page, mix the remote tracks with Web Audio, downsample to 16 kHz in an AudioWorklet, and stream PCM to Go. That means no PulseAudio, no ffmpeg, and fewer moving parts.
- **[side note] Groq bills speech-to-text in 10-second minimums**, so we send ~20 s chunks rather than tiny ones. That's cheaper, and Whisper gets more context per chunk.

## Building

- **[problem] Our cloud dev box can't reach Google Meet.** `meet.google.com` is blocked by the sandbox network, and WebRTC needs UDP anyway. So the real join test has to happen on a laptop. Inside the sandbox we test the bot against a **fake Meet page** we serve locally: same buttons and aria-labels, plus a WebRTC loopback that plays a known audio clip. That makes the bot testable in CI without Google. (Good blog section: "How do you test a bot for a service you can't reach?")
- **[side note]** The sandbox's Chrome is "Chrome for Testing" 153, the same build Google publishes for automation. Handy, because it matches what we'd pin in Docker.
- **[finding] "Leave call" is not proof that you're in the call.** The obvious signal for "the host admitted us" is the Leave call button. But Meet also renders it in the lobby, and Vexa hit exactly this bug: their bot thought it was admitted while it was still waiting. Better signals are things that only exist in the call: participant tiles (`[data-participant-id]`), the "Present now" / "Share screen" button, or remote audio tracks actually arriving over WebRTC. Our bot treats "remote audio track arrived" or a participant tile as admission.
- **[finding] Typographic apostrophes break text matching.** Meet's live copy says "You’ll join the call…" and "can’t join" with a curly ’, not '. Text matchers need both forms. We normalize ’ → ' before matching.
- **[finding] "Return to home screen" appears in the lobby too.** Vexa once treated it as "denied" and gave up after 4 seconds while the host hadn't decided yet. Only "denied your request" / "weren't allowed to join" mean denial.
- **[finding] Detecting "I'm alone in the meeting" is harder than it sounds.** Counting `[data-participant-id]` tiles can drop to 0 during a screen share or a layout change. Vexa's bot left healthy meetings after 60 s because of this (issue #285, April 2026). Their principled fix: only declare "alone" when two independent signals agree. We do the same: alone = people count ≤ 1 **and** no remote audio for a while.
- **[decision] Mark-then-click instead of hard-coded selectors in Go.** A small in-page "probe" script scans the DOM with all our text/aria patterns, decides what phase we're in (pre-join, lobby, in call, denied, ended), and tags the element to act on with `data-meet-agent="join-button"`. Go then clicks that tag with a real CDP mouse event. The fuzzy matching stays in one JS function (easy to unit-test against fake pages), and the actions stay real input events.
