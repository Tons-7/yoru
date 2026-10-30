import { lazy, Suspense, useEffect } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Centered, LinkButton, Spinner } from './components/ui'
import { resumeSaving } from './lib/offline'
import { Home } from './pages/Home'

const Anime = lazy(() => import('./pages/Anime').then((m) => ({ default: m.Anime })))
const Browse = lazy(() => import('./pages/Browse').then((m) => ({ default: m.Browse })))
const Library = lazy(() => import('./pages/Library').then((m) => ({ default: m.Library })))
const History = lazy(() => import('./pages/History').then((m) => ({ default: m.History })))
const SchedulePage = lazy(() =>
  import('./pages/Schedule').then((m) => ({ default: m.SchedulePage })),
)
const RecentPage = lazy(() => import('./pages/Recent').then((m) => ({ default: m.RecentPage })))
const Settings = lazy(() => import('./pages/Settings').then((m) => ({ default: m.Settings })))
const Notifications = lazy(() =>
  import('./pages/Manage').then((m) => ({ default: m.Notifications })),
)
const Downloads = lazy(() => import('./pages/Manage').then((m) => ({ default: m.Downloads })))
const LocalFiles = lazy(() => import('./pages/Manage').then((m) => ({ default: m.LocalFiles })))
const SetupPage = lazy(() => import('./pages/Setup').then((m) => ({ default: m.SetupPage })))
const Open = lazy(() => import('./pages/Open').then((m) => ({ default: m.Open })))
const Person = lazy(() => import('./pages/Person').then((m) => ({ default: m.Person })))
const CharacterPage = lazy(() => import('./pages/Character').then((m) => ({ default: m.CharacterPage })))
const SavedWatch = lazy(() => import('./pages/SavedWatch').then((m) => ({ default: m.SavedWatch })))
const You = lazy(() => import('./pages/You').then((m) => ({ default: m.You })))

function Loading() {
  return (
    <Centered>
      <Spinner />
    </Centered>
  )
}

const offline = window.location.pathname.startsWith('/saved') && new URLSearchParams(window.location.search).has('offline')

export function App() {
  useEffect(() => {
    if (!offline) resumeSaving()
  }, [])
  if (offline) {
    return (
      <Suspense fallback={<Loading />}>
        <Routes>
          <Route path="saved/:key" element={<SavedWatch offline />} />
          <Route path="*" element={<Downloads offline />} />
        </Routes>
      </Suspense>
    )
  }
  return (
    <Suspense fallback={<Loading />}>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Home />} />
          <Route path="watch/:animeId/:episode" element={null} />
          <Route path="anime/:animeId" element={<Anime />} />
          <Route path="person/:personId" element={<Person />} />
          <Route path="character/:characterId" element={<CharacterPage />} />
          <Route path="browse" element={<Browse />} />
          <Route path="search" element={<ToSearch />} />
          <Route path="schedule" element={<SchedulePage />} />
          <Route path="recent" element={<RecentPage />} />
          <Route path="library" element={<Library />} />
          <Route path="history" element={<History />} />
          <Route path="you" element={<You />} />
          <Route path="settings" element={<Settings />} />
          <Route path="notifications" element={<Notifications />} />
          <Route path="downloads" element={<Downloads />} />
          <Route path="saved" element={<Downloads phone />} />
          <Route path="saved/:key" element={<SavedWatch />} />
          <Route path="local" element={<LocalFiles />} />
          <Route path="setup" element={<SetupPage />} />
          <Route path="open" element={<Open />} />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </Suspense>
  )
}

function ToSearch() {
  const { search, hash } = useLocation()
  return <Navigate to={`/browse${search}${hash}`} replace />
}

function NotFound() {
  return (
    <Centered>
      <div className="text-center">
        <p className="bg-gradient-to-b from-accent-400 to-accent-600/40 bg-clip-text text-7xl font-bold tracking-tight text-transparent">
          404
        </p>
        <p className="mt-2 text-base-200">That page does not exist.</p>
        <p className="mt-1 text-sm text-base-500">The link may be old, or the show was removed.</p>
        <div className="mt-5 flex justify-center gap-2">
          <LinkButton to="/" variant="primary">
            Go home
          </LinkButton>
          <LinkButton to="/browse">Search anime</LinkButton>
        </div>
      </div>
    </Centered>
  )
}
