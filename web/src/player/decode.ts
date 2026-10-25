// What this device decodes smoothly, sent with each stream open: Yoru copies those and converts the rest.

// Tokens as the server reads them (transcode.ParseCaps): ffprobe's codec names, "-10" for 10-bit.
const VIDEO: [token: string, codec: string][] = [
  ['hevc', 'hvc1.1.6.L120.90'],
  ['hevc-10', 'hvc1.2.4.L120.90'],
  ['h264-10', 'avc1.6e0028'],
  ['av1', 'av01.0.08M.08'],
  ['av1-10', 'av01.0.08M.10'],
  ['vp9', 'vp09.00.40.08'],
  ['vp9-10', 'vp09.02.40.10'],
]
const AUDIO: [token: string, codec: string][] = [
  ['flac', 'flac'],
  ['opus', 'opus'],
  ['ac3', 'ac-3'],
  ['eac3', 'ec-3'],
]

type MediaSourceLike = { isTypeSupported(type: string): boolean }

// hls.js plays through (Managed)MediaSource; without either the browser plays the playlist itself.
function mediaSource(): MediaSourceLike | undefined {
  const w = window as unknown as { ManagedMediaSource?: MediaSourceLike; MediaSource?: MediaSourceLike }
  return w.ManagedMediaSource ?? w.MediaSource
}

async function decodes(kind: 'video' | 'audio', codec: string): Promise<boolean> {
  const contentType = `${kind}/mp4; codecs="${codec}"`
  const source = mediaSource()
  const plays = source ? source.isTypeSupported(contentType) : document.createElement('video').canPlayType(contentType) === 'probably'
  if (!plays || !navigator.mediaCapabilities?.decodingInfo) return plays
  // Smooth as well: a decoder that can't keep up at 1080p stutters where converting wouldn't.
  const info = await navigator.mediaCapabilities
    .decodingInfo({
      type: source ? 'media-source' : 'file',
      ...(kind === 'video'
        ? { video: { contentType, width: 1920, height: 1080, bitrate: 8_000_000, framerate: 24 } }
        : { audio: { contentType, channels: '6', bitrate: 640_000, samplerate: 48_000 } }),
    })
    .catch(() => null)
  return !info || (info.supported && info.smooth)
}

let codecs: Promise<string[]> | undefined

/** The device's list for ?caps=; strict leaves codecs out, so all past H.264 and AAC is converted. */
export async function deviceCaps(strict = false): Promise<string> {
  codecs ??= Promise.all([
    ...VIDEO.map(async ([token, codec]) => ((await decodes('video', codec)) ? token : '')),
    ...AUDIO.map(async ([token, codec]) => ((await decodes('audio', codec)) ? token : '')),
  ]).then((tokens) => tokens.filter(Boolean))
  const list = strict ? [] : await codecs
  // Read on each open, as speakers are: the window may have moved to another screen.
  const hdr = matchMedia('(dynamic-range: high)').matches ? ['hdr'] : []
  return [...list, ...hdr, `ch${speakers()}`].join(',')
}

// Read on each open: a receiver or headphones may have been plugged in since.
function speakers(): number {
  try {
    const context = new AudioContext()
    const n = context.destination.maxChannelCount
    void context.close()
    return n
  } catch {
    return 0
  }
}
