import { redrawCanvas } from './smooth'

const nextFrame = () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))

function blank(context: CanvasRenderingContext2D, width: number, height: number): boolean {
  const size = 16
  const { data } = context.getImageData(Math.floor((width - size) / 2), Math.floor((height - size) / 2), size, size)
  for (let i = 3; i < data.length; i += 4) if (data[i] !== 0) return false
  return true
}

async function screenshotBlob(video: HTMLVideoElement, picture: HTMLCanvasElement | null): Promise<Blob> {
  const width = picture?.width || video.videoWidth
  const height = picture?.height || video.videoHeight
  if (!width || !height) throw new Error('no picture yet')
  const out = document.createElement('canvas')
  out.width = width
  out.height = height
  const context = out.getContext('2d', { willReadFrequently: true })
  if (!context) throw new Error('no canvas')

  await nextFrame()
  if (picture) {
    redrawCanvas(picture)
    context.drawImage(picture, 0, 0, width, height)
  }
  if (!picture || blank(context, width, height)) context.drawImage(video, 0, 0, width, height)
  const subtitles = video.parentElement?.querySelector<HTMLCanvasElement>('canvas.JASSUB')
  if (subtitles?.width && subtitles.height) context.drawImage(subtitles, 0, 0, width, height)

  return new Promise((resolve, reject) => {
    out.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('no image'))), 'image/png')
  })
}

function screenshotName(title: string, subtitle: string | undefined, seconds: number): string {
  const at = `${Math.floor(seconds / 60)}m${String(Math.floor(seconds % 60)).padStart(2, '0')}s`
  const name = [title, subtitle, at].filter(Boolean).join(' - ')
  return `${name.replace(/[\\/:*?"<>|]/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 150)}.png`
}

export async function saveScreenshot(
  video: HTMLVideoElement,
  picture: HTMLCanvasElement | null,
  title: string,
  subtitle?: string,
): Promise<boolean> {
  const blob = await screenshotBlob(video, picture)
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = screenshotName(title, subtitle, video.currentTime)
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000)
  if (typeof ClipboardItem === 'undefined' || !navigator.clipboard?.write) return false
  return navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]).then(
    () => true,
    () => false,
  )
}
