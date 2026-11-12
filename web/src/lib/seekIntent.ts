const meant = new WeakMap<HTMLMediaElement, number>()

export function jump(video: HTMLMediaElement, to: number) {
  meant.set(video, performance.now())
  video.currentTime = to
}

const systemControls = (video: HTMLMediaElement) =>
  (video as HTMLMediaElement & { webkitDisplayingFullscreen?: boolean }).webkitDisplayingFullscreen === true

export const jumpMeant = (video: HTMLMediaElement) =>
  systemControls(video) || performance.now() - (meant.get(video) ?? -Infinity) < 1000
