// A subtitle file from this device: ASS as it is, SRT and WebVTT made ASS, which the renderer draws.

const HEAD = `[Script Info]
ScriptType: v4.00+
PlayResX: 1920
PlayResY: 1080
WrapStyle: 0

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,64,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,3,1,2,80,80,54,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

// 00:01:02,345 (SRT) or 01:02.345 (WebVTT, hours optional).
const STAMP = /(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})/

function seconds(stamp: string): number | null {
  const m = STAMP.exec(stamp)
  if (!m) return null
  return Number(m[1] ?? 0) * 3600 + Number(m[2]) * 60 + Number(m[3]) + Number(m[4].padEnd(3, '0')) / 1000
}

function assTime(t: number): string {
  const cs = Math.round(t * 100)
  const h = Math.floor(cs / 360000)
  const m = Math.floor((cs % 360000) / 6000)
  const s = Math.floor((cs % 6000) / 100)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${h}:${pad(m)}:${pad(s)}.${pad(cs % 100)}`
}

// Italic and bold carried over; other markup (voices, colours, positions) dropped.
function assText(lines: string[]): string {
  return (
    lines
      .join('\\N')
      // A brace in the text would read as an ASS tag.
      .replace(/[{}]/g, '')
      .replace(/<\/?i>/g, (t) => (t[1] === '/' ? '{\\i0}' : '{\\i1}'))
      .replace(/<\/?b>/g, (t) => (t[1] === '/' ? '{\\b0}' : '{\\b1}'))
      .replace(/<[^>]*>/g, '')
  )
}

/** SRT or WebVTT as ASS: each cue's times, then its lines. */
export function textToASS(text: string): string {
  const events: string[] = []
  for (const block of text.replace(/\r/g, '').split(/\n{2,}/)) {
    const lines = block.split('\n')
    const at = lines.findIndex((l) => l.includes('-->'))
    if (at < 0) continue
    const [from, to] = lines[at].split('-->')
    const start = seconds(from)
    const end = seconds(to)
    const body = lines.slice(at + 1).filter((l) => l.trim())
    if (start === null || end === null || body.length === 0) continue
    events.push(`Dialogue: 0,${assTime(start)},${assTime(end)},Default,,0,0,0,,${assText(body)}`)
  }
  return HEAD + events.join('\n') + '\n'
}

/** The file as ASS, or why it can't be used. */
export async function readSubtitleFile(file: File): Promise<string> {
  const text = await file.text()
  if (/^\s*\[Script Info\]/i.test(text)) return text
  const ass = textToASS(text)
  if (!/^Dialogue:/m.test(ass)) throw new Error(`No subtitle lines in ${file.name}; Yoru reads .ass, .ssa, .srt and .vtt`)
  return ass
}
