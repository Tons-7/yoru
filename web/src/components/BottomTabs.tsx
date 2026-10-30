import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { activityWords, IDLE, useDeviceActivity, useDoneFlash, usePcActivity, type Activity } from '../lib/activity'
import { cx } from '../lib/format'
import { useAccess, useNotifications } from '../lib/queries'
import { useNewSettings } from '../lib/whatsNew'
import { ActivityMark } from './ActivityRing'
import { placeOf, type Place } from './places'
import { useAccount } from './you'
import { Avatar } from './YouBits'

const PLACES: { place: Place; to: string; label: string; icon: () => ReactNode }[] = [
  { place: 'home', to: '/', label: 'Home', icon: HomeIcon },
  { place: 'search', to: '/browse', label: 'Search', icon: SearchIcon },
  { place: 'schedule', to: '/schedule', label: 'Schedule', icon: ScheduleIcon },
  { place: 'library', to: '/library', label: 'Library', icon: LibraryIcon },
]

const useIdle = () => IDLE

export function BottomTabs() {
  const { pathname } = useLocation()
  const here = placeOf(pathname)
  const access = useAccess()
  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-white/5 bg-base-950/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl backdrop-saturate-150"
    >
      <div className="flex">
        {PLACES.map(({ place, to, label, icon: Icon }) => (
          <Tab key={to} to={to} label={label} active={here === place}>
            <Icon />
          </Tab>
        ))}
        {access.data?.host ? (
          <YouTab key="pc" active={here === 'you'} useActivity={usePcActivity} />
        ) : access.data ? (
          <YouTab key="device" active={here === 'you'} useActivity={useDeviceActivity} />
        ) : (
          <YouTab key="none" active={here === 'you'} useActivity={useIdle} />
        )}
      </div>
    </nav>
  )
}

function YouTab({ active, useActivity }: { active: boolean; useActivity: () => Activity }) {
  const activity = useActivity()
  const done = useDoneFlash(activity)
  const { user, attention } = useAccount()
  const unread = useNotifications().data?.unread ?? 0
  const fresh = useNewSettings().unseen
  const busy = activity.count > 0
  const cornerTaken = busy || activity.trouble > 0
  const dot = attention ? 'bg-amber-400' : unread > 0 || done || fresh ? 'bg-sky-300' : null
  const words = [
    activityWords(activity, done),
    unread > 0 && `${unread} unread ${unread === 1 ? 'notification' : 'notifications'}`,
    fresh && 'new settings since your update',
    attention && 'reconnect to resume syncing',
  ]
    .filter(Boolean)
    .join(', ')
  return (
    <Tab to="/you" label="You" active={active} name={words ? `You, ${words}` : undefined} pill={false}>
      <ActivityMark activity={activity} done={false}>
        <span
          className={cx(
            'relative grid size-5 place-items-center rounded-full',
            active && !busy && 'ring-[1.5px] ring-base-100 ring-offset-2 ring-offset-base-950',
          )}
        >
          <Avatar name={user} className="size-5" letter="text-xs" />
          {dot && (
            <span
              aria-hidden
              className={cx(
                'absolute size-2 rounded-full ring-2 ring-base-950',
                cornerTaken ? '-top-1 -left-1' : '-top-0.5 -right-0.5',
                dot,
              )}
            />
          )}
        </span>
      </ActivityMark>
    </Tab>
  )
}

function Tab({
  to,
  label,
  active,
  name,
  pill = true,
  children,
}: {
  to: string
  label: string
  active: boolean
  name?: string
  pill?: boolean
  children: ReactNode
}) {
  return (
    <Link
      to={to}
      aria-current={active ? 'page' : undefined}
      aria-label={name}
      className={cx(
        'flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 text-xs font-medium transition-colors',
        active ? 'text-white' : 'text-base-400 active:text-base-100',
      )}
    >
      <span className={cx('grid h-7 w-12 place-items-center rounded-full transition-colors', pill && active && 'bg-accent-500/25')}>
        {children}
      </span>
      {label}
    </Link>
  )
}

const stroke = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round', strokeLinejoin: 'round' } as const

function HomeIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M4 10.5 12 4l8 6.5V20a1 1 0 0 1-1 1h-4.5v-6h-5v6H5a1 1 0 0 1-1-1z" {...stroke} />
    </svg>
  )
}

function SearchIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <circle cx="11" cy="11" r="6.5" {...stroke} />
      <path d="m16 16 4.5 4.5" {...stroke} />
    </svg>
  )
}

function ScheduleIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <rect x="4" y="5.5" width="16" height="15" rx="2" {...stroke} />
      <path d="M8 3.5v4M16 3.5v4M4 10h16" {...stroke} />
    </svg>
  )
}

function LibraryIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <rect x="4" y="4" width="6" height="16" rx="1.5" {...stroke} />
      <rect x="12.5" y="4" width="6" height="16" rx="1.5" transform="rotate(-8 15.5 12)" {...stroke} />
    </svg>
  )
}
