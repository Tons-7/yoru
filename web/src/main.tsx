import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import { App } from './App'
import { Toaster } from './components/Toaster'
import { toast } from './lib/toast'
import { desktopNotifyWanted } from './lib/notifications'
import { registerWorker } from './lib/push'
import { listenForInstall, runningInstalled } from './lib/install'

const client = new QueryClient({
  // A failed action says so, unless meta.inline shows it beside the control instead.
  mutationCache: new MutationCache({
    onError: (error, _vars, _ctx, mutation) => {
      if (mutation.meta?.inline) return
      toast(error instanceof Error ? error.message : 'That did not work')
    },
  }),
  defaultOptions: {
    queries: {
      // Everything upstream is rate limited or slow, so a re-render must not
      // become a re-fetch; window focus still refreshes what has gone stale.
      staleTime: 60_000,
      refetchOnWindowFocus: true,
      retry: (failures, error) => {
        // A rejected token or a bad request will fail identically forever.
        const status = (error as { status?: number })?.status ?? 0
        if (status >= 400 && status < 500) return false
        return failures < 2
      },
    },
  },
})

// An update replaced the build under an open page: its old chunks are gone, so load the new one, once.
window.addEventListener('vite:preloadError', (e) => {
  try {
    if (sessionStorage.getItem('yoru.reloaded-for-update') === location.pathname) return
    sessionStorage.setItem('yoru.reloaded-for-update', location.pathname)
  } catch {
    // Unstorable: no guard against a loop, so leave the error to the page.
    return
  }
  e.preventDefault()
  location.reload()
})

// Registered each start, so an update takes over: for alerts, and the installed app's "can't reach" page.
if (desktopNotifyWanted() || runningInstalled()) void registerWorker()?.catch(() => {})
listenForInstall()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
      <Toaster />
    </QueryClientProvider>
  </StrictMode>,
)
