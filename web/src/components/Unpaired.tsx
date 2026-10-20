import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { TOKEN_KEY, UNPAIRED_EVENT } from '../lib/api'
import { usePortalHome } from './keys'
import { cx } from '../lib/format'
import { buttonClass } from '../lib/ui'

/** Over everything once the server refuses this device's token. */
export function Unpaired() {
  const [unpaired, setUnpaired] = useState(false)
  // The token refused; a scan in another tab may since have stored a good one, which must stay.
  const [refused, setRefused] = useState<string | null>(null)
  useEffect(() => {
    const on = () => {
      setUnpaired(true)
      try {
        setRefused(localStorage.getItem(TOKEN_KEY))
      } catch {
        // Unreadable storage: nothing to clear later either.
      }
    }
    window.addEventListener(UNPAIRED_EVENT, on)
    return () => window.removeEventListener(UNPAIRED_EVENT, on)
  }, [])
  // Inside the fullscreen player when there is one: outside it a phone watching never sees why it stopped.
  const home = usePortalHome()
  if (!unpaired) return null

  return createPortal(
    <div className="fixed inset-0 z-[100] grid place-items-center bg-base-950/95 p-6 text-center backdrop-blur">
      <div className="max-w-sm">
        <p className="text-lg font-semibold text-white">This device needs pairing again</p>
        <p className="mt-2 text-sm text-base-300">
          The link it was using was signed out. On the computer running Yoru, open Settings › Devices
          and scan the code again.
        </p>
        <button
          onClick={() => {
            try {
              if (localStorage.getItem(TOKEN_KEY) === refused) localStorage.removeItem(TOKEN_KEY)
            } catch {
              // Nothing stored is fine.
            }
            // Without the query: a ?token= still in the URL would bring the refused one back.
            window.location.replace(window.location.pathname + window.location.hash)
          }}
          className={cx(buttonClass('primary'), 'mt-5')}
        >
          Scanned it, reload
        </button>
      </div>
    </div>,
    home,
  )
}
