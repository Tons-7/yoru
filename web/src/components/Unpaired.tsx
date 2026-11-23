import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { TOKEN_KEY, UNPAIRED_EVENT } from '../lib/api'
import { usePortalHome } from './keys'
import { cx } from '../lib/format'
import { buttonClass } from '../lib/ui'

export function Unpaired() {
  const [unpaired, setUnpaired] = useState(false)
  const [refused, setRefused] = useState<string | null>(null)
  useEffect(() => {
    const on = () => {
      setUnpaired(true)
      try {
        setRefused(localStorage.getItem(TOKEN_KEY))
      } catch {
      }
    }
    window.addEventListener(UNPAIRED_EVENT, on)
    return () => window.removeEventListener(UNPAIRED_EVENT, on)
  }, [])
  const home = usePortalHome()
  if (!unpaired) return null

  return createPortal(
    <div className="fixed inset-0 z-100 grid place-items-center bg-base-950/95 p-6 text-center backdrop-blur">
      <div role="alertdialog" aria-modal="true" aria-labelledby="unpaired-title" aria-describedby="unpaired-what" className="max-w-sm">
        <p id="unpaired-title" className="text-lg font-semibold text-white">This device needs pairing again</p>
        <p id="unpaired-what" className="mt-2 text-sm text-base-300">
          The link it was using was signed out. On the computer running Yoru, open Settings › Devices
          and scan the code again, then reload here.
        </p>
        <button
          autoFocus
          onClick={() => {
            try {
              if (localStorage.getItem(TOKEN_KEY) === refused) localStorage.removeItem(TOKEN_KEY)
            } catch {
            }
            window.location.replace(window.location.pathname + window.location.hash)
          }}
          className={cx(buttonClass('primary'), 'mt-5')}
        >
          Reload
        </button>
      </div>
    </div>,
    home,
  )
}
