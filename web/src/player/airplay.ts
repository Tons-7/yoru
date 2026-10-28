import { useCallback, useEffect, useState } from 'react'

// AirPlay is Safari's own: asked of the browser by its API, not by its name.
type AirPlayVideo = HTMLVideoElement & {
  webkitShowPlaybackTargetPicker?: () => void
  webkitCurrentPlaybackTargetIsWireless?: boolean
}

/** Whether this browser can send video to an Apple TV at all. */
export const airplayHere = () => 'WebKitPlaybackTargetAvailabilityEvent' in window

/** An Apple TV about (Safari says so), the video on one, and Safari's own list of them to pick from. */
export function useAirPlay(video: HTMLVideoElement | null) {
  const [available, setAvailable] = useState(false)
  const [active, setActive] = useState(false)
  useEffect(() => {
    if (!video || !airplayHere()) return
    const v = video as AirPlayVideo
    const onAvailable = (e: Event) => setAvailable((e as Event & { availability?: string }).availability === 'available')
    const onWireless = () => setActive(!!v.webkitCurrentPlaybackTargetIsWireless)
    video.addEventListener('webkitplaybacktargetavailabilitychanged', onAvailable)
    video.addEventListener('webkitcurrentplaybacktargetiswirelesschanged', onWireless)
    onWireless()
    return () => {
      video.removeEventListener('webkitplaybacktargetavailabilitychanged', onAvailable)
      video.removeEventListener('webkitcurrentplaybacktargetiswirelesschanged', onWireless)
    }
  }, [video])
  const pick = useCallback(() => (video as AirPlayVideo | null)?.webkitShowPlaybackTargetPicker?.(), [video])
  return { available, active, pick }
}
