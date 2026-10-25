import { NavLink } from 'react-router-dom'
import { cx } from '../lib/format'

// A phone's main places within thumb's reach; Recently released is a tab of Schedule.
const PLACES = [
  { to: '/', label: 'Home', end: true, icon: HomeIcon },
  { to: '/browse', label: 'Browse', icon: BrowseIcon },
  { to: '/schedule', label: 'Schedule', icon: ScheduleIcon },
  { to: '/library', label: 'Library', icon: LibraryIcon },
  { to: '/downloads', label: 'Downloads', icon: DownloadsIcon },
]

export function BottomTabs() {
  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-white/5 bg-base-950/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl backdrop-saturate-150"
    >
      <div className="flex">
        {PLACES.map(({ to, label, end, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cx(
                'flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 text-[11px] font-medium transition-colors',
                isActive ? 'text-white' : 'text-base-400 active:text-base-100',
              )
            }
          >
            {({ isActive }) => (
              <>
                <span className={cx('grid h-7 w-12 place-items-center rounded-full transition-colors', isActive && 'bg-accent-500/25')}>
                  <Icon />
                </span>
                {label}
              </>
            )}
          </NavLink>
        ))}
      </div>
    </nav>
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

function BrowseIcon() {
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

function DownloadsIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M12 4v11m0 0 4.5-4.5M12 15l-4.5-4.5M5 20h14" {...stroke} />
    </svg>
  )
}
