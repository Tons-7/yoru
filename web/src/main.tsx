import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import './index.css'
import { App } from './App'
import { Toaster } from './components/Toaster'
import { toast } from './lib/toast'
import { watchRefreshes } from './lib/freshness'
import { watchVersion } from './lib/version'
import { desktopNotifyWanted } from './lib/notifications'
import { beacon } from './lib/api'
import { registerWorker, resyncPush } from './lib/push'
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
window.setTimeout(() => {
  try {
    sessionStorage.removeItem('yoru.reloaded-for-update')
  } catch {
  }
}, 30_000)

if (desktopNotifyWanted() || runningInstalled()) {
  void registerWorker()
    ?.then(() => resyncPush())
    .catch(() => {})
}
listenForInstall()
navigator.serviceWorker?.addEventListener('message', (e: MessageEvent<{ type?: string; url?: string }>) => {
  if (e.data?.type !== 'open' || typeof e.data.url !== 'string') return
  const to = new URL(e.data.url, location.origin)
  if (to.origin === location.origin && to.href !== location.href) location.assign(to.href)
})

const reported = new Set<string>()
const reportCrash = (error: unknown, info: { componentStack?: string }) => {
  console.error(error)
  const e = error instanceof Error ? error : new Error(String(error))
  if (reported.has(e.message) || reported.size > 20) return
  reported.add(e.message)
  beacon('/api/client-error', {
    message: e.message,
    stack: e.stack ?? '',
    componentStack: info.componentStack ?? '',
    path: location.pathname,
  })
}

const router = createBrowserRouter([{ path: '*', element: <App /> }])

createRoot(document.getElementById('root')!, { onCaughtError: reportCrash, onUncaughtError: reportCrash }).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  </StrictMode>,
)
