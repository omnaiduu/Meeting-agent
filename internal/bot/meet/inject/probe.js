// Probe: evaluated by Go every poll as `(probe)(patterns, wasInCall)`.
// Reads the DOM, decides which phase the page is in, and tags elements Go
// should act on with data-ma-<name> attributes (Go clicks them with real
// CDP mouse events). Returns a plain JSON object.
(function (P, wasInCall) {
  const norm = (s) => (s || '').replace(/[‘’ʼ]/g, "'").replace(/\s+/g, ' ').trim().toLowerCase();
  const visible = (el) => {
    if (!el || !el.isConnected) return false;
    const r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    const st = getComputedStyle(el);
    return st.visibility !== 'hidden' && st.display !== 'none' && st.opacity !== '0';
  };
  const text = norm(document.body ? document.body.innerText : '');
  const findText = (list) => (list || []).find((t) => text.includes(norm(t))) || null;
  const firstCss = (list) => {
    for (const s of list || []) {
      let els = [];
      try { els = document.querySelectorAll(s); } catch (e) { continue; }
      for (const el of els) if (visible(el)) return el;
    }
    return null;
  };
  const buttons = [...document.querySelectorAll('button, [role="button"]')].filter(visible);
  const label = (b) => norm(b.innerText || b.getAttribute('aria-label') || '');
  const buttonByText = (list) => {
    for (const t of list || []) { // exact first, in priority order
      const n = norm(t);
      const b = buttons.find((b) => label(b) === n || norm(b.getAttribute('aria-label')) === n);
      if (b) return b;
    }
    return null;
  };
  const mark = (name, el) => {
    const attr = 'data-ma-' + name;
    document.querySelectorAll('[' + attr + ']').forEach((e) => e.removeAttribute(attr));
    if (el) el.setAttribute(attr, '');
    return !!el;
  };

  const st = window.__meetAgent || {};
  const out = {
    phase: 'unknown',
    match: null,
    participants: -1,
    name: false, join: false, dismiss: false, leave: false,
    audio: { tracks: st.tracks || 0, live: st.liveTracks || 0, msSinceAudio: st.lastAudioAt ? Date.now() - st.lastAudioAt : -1, mode: st.mode || '' },
    title: document.title,
    url: location.href,
  };

  const nameInput = firstCss(P.nameInput);
  const joinBtn = buttonByText(P.joinTexts);
  out.name = mark('name', nameInput);
  out.join = mark('join', joinBtn);
  out.dismiss = mark('dismiss', buttonByText(P.dismissTexts));
  out.leave = mark('leave', firstCss(P.leaveCss) || buttonByText(P.leaveTexts));

  // People count: the People button carries a number ("People 3"); tiles carry participant ids.
  let people = -1;
  for (const b of buttons) {
    const all = (b.innerText || '') + ' ' + (b.getAttribute('aria-label') || '');
    if ((P.peopleWords || []).some((w) => norm(all).startsWith(norm(w)) || norm(b.getAttribute('aria-label')).startsWith(norm(w)))) {
      const m = all.match(/(\d+)/);
      if (m) people = Math.max(people, parseInt(m[1], 10));
    }
  }
  const ids = new Set([...document.querySelectorAll('[data-participant-id]')].map((e) => e.getAttribute('data-participant-id')));
  out.participants = Math.max(people, ids.size > 0 ? ids.size : -1);

  // Phase, most decisive first. "Return to home screen" also appears in the
  // lobby, so it only counts as "ended" once we have been in the call.
  let m;
  const ended = (P.endedTexts || []).filter((t) => wasInCall || norm(t) !== 'return to home screen');
  if ((m = findText(P.deniedTexts))) { out.phase = 'denied'; out.match = m; }
  else if ((m = findText(ended)) && (wasInCall || !firstCss(P.inCall))) { out.phase = 'ended'; out.match = m; }
  else if ((m = findText(P.blockedTexts))) { out.phase = 'blocked'; out.match = m; }
  else if (firstCss(P.inCall) || (st.audioSeen && !findText(P.lobbyTexts))) { out.phase = 'in_call'; }
  else if ((m = findText(P.lobbyTexts))) { out.phase = 'lobby'; out.match = m; }
  else if (nameInput || joinBtn) { out.phase = 'prejoin'; }
  return out;
})
