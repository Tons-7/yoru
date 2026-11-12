import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import { App } from './App'
import { Toaster } from './components/Toaster'
import { toast } from './lib/toast'
import { watchRefreshes } from './lib/freshness'
import { watchVersion } from './lib/version'
import { desktopNotifyWanted } from './lib/notifications'
import { registerWorker } from './lib/push'
import { listenForInstall, runningInstalled } from './lib/install'

const client = new QueryClient({
  mutationCache: new MutationCache({
    onError: (error, _vars, _ctx, mutation) => {
      if (mutation.meta?.inline) return
      toast(error instanceof Error ? error.message : 'That did not work')
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      refetchOnWindowFocus: true,
      retry: (failures, error) => {
        const status = (error as { status?: number })?.status ?? 0
        if (status >= 400 && status < 500) return false
        return failures < 2
      },
    },
  },
})

watchRefreshes(client)
watchVersion()

window.addEventListener('vite:preloadError', (e) => {
  try {
    if (sessionStorage.getItem('yoru.reloaded-for-update') === location.pathname) return
    sessionStorage.setItem('yoru.reloaded-for-update', location.pathname)
  } catch {
    return
  }
  e.preventDefault()
  location.reload()
})

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
