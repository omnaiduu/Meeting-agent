// Audio capture, injected before any page script runs.
//
// 1. Wrap RTCPeerConnection so we see every remote track the page receives.
// 2. Mix all remote audio tracks into one GainNode with Web Audio.
// 3. An AudioWorklet converts the mix to 16 kHz mono int16 (what Whisper wants).
//    If the worklet can't load (CSP / Trusted Types), fall back to a ScriptProcessorNode.
// 4. ~1 s batches are base64-encoded and sent to Go via window.__meetAgentAudio
//    (created by chromedp.ExposeFunc). Calls carry a sequence number because Go
//    handles each call in its own goroutine, so they can arrive out of order.
(() => {
  if (window.__meetAgent) return;
  const TARGET_RATE = 16000;
  const BATCH_SAMPLES = TARGET_RATE; // 1 s
  const SPEECH_RMS = 0.01; // "someone is making sound" threshold, float scale

  const state = {
    tracks: 0, liveTracks: 0, lastAudioAt: 0, audioSeen: false,
    seq: 0, mode: '', errors: [],
  };
  window.__meetAgent = state;
  const logErr = (where, e) => { state.errors.push(where + ': ' + (e && e.message || e)); if (state.errors.length > 20) state.errors.shift(); };

  // ---------- PCM batching + bridge to Go ----------
  let batch = new Int16Array(BATCH_SAMPLES);
  let fill = 0;
  function toBase64(i16) {
    const bytes = new Uint8Array(i16.buffer, i16.byteOffset, i16.byteLength);
    let bin = '';
    for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    return btoa(bin);
  }
  function flush() {
    if (fill === 0) return;
    const out = batch.slice(0, fill);
    fill = 0;
    const send = window.__meetAgentAudio;
    if (typeof send === 'function') {
      send({ seq: state.seq++, pcm: toBase64(out) }).catch((e) => logErr('send', e));
    }
  }
  function pushPCM(i16, rms) {
    if (rms > SPEECH_RMS) { state.lastAudioAt = Date.now(); state.audioSeen = true; }
    let off = 0;
    while (off < i16.length) {
      const n = Math.min(i16.length - off, BATCH_SAMPLES - fill);
      batch.set(i16.subarray(off, off + n), fill);
      fill += n; off += n;
      if (fill === BATCH_SAMPLES) flush();
    }
  }
  window.__meetAgentFlush = flush;

  // ---------- Resampler shared by worklet and fallback (box filter + linear interp) ----------
  class Resampler {
    constructor(inRate, outRate) { this.step = inRate / outRate; this.pos = 0; this.prev = 0; }
    // Box-average the input over each output period (cheap anti-aliasing), then pick samples.
    process(input) {
      const out = [];
      let sumSq = 0;
      const step = this.step;
      let pos = this.pos;
      while (pos < input.length) {
        const start = Math.floor(pos), end = Math.min(input.length, Math.floor(pos + step));
        let acc = 0, n = 0;
        for (let i = start; i < Math.max(end, start + 1); i++) { acc += input[i]; n++; }
        const v = n ? acc / n : this.prev;
        this.prev = v;
        out.push(v);
        sumSq += v * v;
        pos += step;
      }
      this.pos = pos - input.length;
      const i16 = new Int16Array(out.length);
      for (let i = 0; i < out.length; i++) {
        const s = Math.max(-1, Math.min(1, out[i]));
        i16[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
      }
      return { i16, rms: out.length ? Math.sqrt(sumSq / out.length) : 0 };
    }
  }
  // The worklet runs in its own scope, so it gets the class as source text.
  // (Not new Function/eval: a strict CSP blocks those, which would also break the fallback.)
  const resamplerSrc = Resampler.toString();
  const workletSrc = resamplerSrc + `
    class PcmWorklet extends AudioWorkletProcessor {
      constructor(opts) { super(); this.r = new Resampler(sampleRate, opts.processorOptions.targetRate); }
      process(inputs) {
        // No connected source = silence, not nothing: keep the timeline continuous
        // so transcript timestamps stay aligned with the meeting clock.
        const ch = (inputs[0] && inputs[0][0]) || (this.zeros || (this.zeros = new Float32Array(128)));
        if (ch.length) {
          const { i16, rms } = this.r.process(ch);
          this.port.postMessage({ i16, rms }, [i16.buffer]);
        }
        return true;
      }
    }
    registerProcessor('meet-agent-pcm', PcmWorklet);`;

  // ---------- Web Audio graph (created on the first remote audio track) ----------
  let ctx = null, mix = null, ready = null;
  function graph() {
    if (ready) return ready;
    ctx = new AudioContext();
    mix = ctx.createGain();
    // The processing node must reach the destination to be pulled; keep it silent.
    const silent = ctx.createGain();
    silent.gain.value = 0;
    silent.connect(ctx.destination);
    const useScriptProcessor = () => {
      state.mode = 'scriptprocessor';
      const r = new Resampler(ctx.sampleRate, TARGET_RATE);
      const sp = ctx.createScriptProcessor(4096, 1, 1);
      sp.onaudioprocess = (e) => { const { i16, rms } = r.process(e.inputBuffer.getChannelData(0)); pushPCM(i16, rms); };
      mix.connect(sp); sp.connect(silent);
    };
    const url = URL.createObjectURL(new Blob([workletSrc], { type: 'application/javascript' }));
    ready = ctx.audioWorklet.addModule(url).then(() => {
      state.mode = 'worklet';
      const node = new AudioWorkletNode(ctx, 'meet-agent-pcm', {
        numberOfInputs: 1, numberOfOutputs: 1, channelCount: 1, channelCountMode: 'explicit',
        channelInterpretation: 'speakers', processorOptions: { targetRate: TARGET_RATE },
      });
      node.port.onmessage = (e) => pushPCM(e.data.i16, e.data.rms);
      mix.connect(node); node.connect(silent);
    }).catch((e) => {
      logErr('worklet', e);
      try { useScriptProcessor(); } catch (e2) { logErr('scriptprocessor', e2); }
    }).then(() => ctx.resume());
    // Keep the context running (Chrome may suspend it).
    setInterval(() => { if (ctx.state !== 'running') ctx.resume().catch(() => {}); }, 2000);
    return ready;
  }

  function onRemoteTrack(track) {
    if (!track || track.kind !== 'audio' || track.__meetAgent) return;
    track.__meetAgent = true;
    state.tracks++; state.liveTracks++;
    const stream = new MediaStream([track]);
    // Chrome quirk: remote WebRTC audio may not flow into Web Audio unless the
    // stream is also attached to a media element. Muted, so the bot plays nothing.
    const el = document.createElement('audio');
    el.muted = true; el.autoplay = true; el.srcObject = stream;
    el.play().catch(() => {});
    let src = null;
    graph().then(() => { src = ctx.createMediaStreamSource(stream); src.connect(mix); }).catch((e) => logErr('source', e));
    track.addEventListener('ended', () => {
      state.liveTracks--;
      if (src) src.disconnect();
      el.srcObject = null;
    });
  }

  // ---------- Hook RTCPeerConnection ----------
  const Orig = window.RTCPeerConnection;
  if (Orig) {
    const Hooked = function (...args) {
      const pc = new Orig(...args);
      pc.addEventListener('track', (ev) => { try { onRemoteTrack(ev.track); } catch (e) { logErr('track', e); } });
      return pc;
    };
    Hooked.prototype = Orig.prototype;
    Object.setPrototypeOf(Hooked, Orig);
    Object.defineProperty(Hooked, 'name', { value: 'RTCPeerConnection' });
    window.RTCPeerConnection = Hooked;
    if (window.webkitRTCPeerConnection) window.webkitRTCPeerConnection = Hooked;
  }

  // Flush the partial batch periodically so silence-heavy tails still arrive.
  setInterval(() => { if (fill > 0 && fill >= TARGET_RATE / 4) flush(); }, 3000);
})();
