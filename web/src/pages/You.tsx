import { useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { cx } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { install, useInstall, type InstallState } from '../lib/install'
import { useAccess, useNotifications } from '../lib/queries'
import { useNewSettings } from '../lib/whatsNew'
import { ExitDialog } from '../components/ExitDialog'
import { useExitCheck, type ExitStops } from '../components/exit'
import { Badge } from '../components/ui'
import { useAccount, useDeviceLine, useNoLine, usePcLine, useTitleLanguage, type DownloadsLine } from '../components/you'
import { Avatar, Chevron, MiniRing } from '../components/YouBits'

export function You() {
  const access = useAccess()
  const [exitAsk, setExitAsk] = useState<ExitStops | null>(null)
  const version = useNewSettings().current

  return (
    <div className="mx-auto w-full max-w-xl space-y-3 sm:space-y-4">
      <h1 className="page-title">You</h1>
      <AccountRow />

      <ul className="surface divide-y divide-white/5 overflow-hidden">
        {access.data?.host ? (
          <DownloadsRow key="pc" useLine={usePcLine} />
        ) : access.data ? (
          <DownloadsRow key="device" useLine={useDeviceLine} />
        ) : (
          <DownloadsRow key="none" useLine={useNoLine} />
        )}
        <NotificationsRow />
        <SettingsRow />
      </ul>

      <ul className="surface divide-y divide-white/5 overflow-hidden">
        <TitlesRow />
        <li>
          <Row to="/local" title="Local files" sub="Episodes from your own folders" />
        </li>
        <InstallRow />
        {access.data?.host && <ExitRow onAsk={setExitAsk} />}
      </ul>

      {version && <p className="px-1 text-xs text-base-500">Yoru {version}</p>}
      {exitAsk && <ExitDialog stops={exitAsk} onClose={() => setExitAsk(null)} />}
    </div>
  )
}

const rowClass =
  'flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left transition-colors hover:bg-base-800 active:bg-base-800 focus-visible:bg-base-800 focus-visible:outline-hidden disabled:opacity-60'

function RowBody({ icon, title, sub, subTone, end }: { icon?: ReactNode; title: ReactNode; sub?: ReactNode; subTone?: string; end?: ReactNode }) {
  return (
    <>
      {icon}
      <span className="min-w-0 flex-1">
        <span className="block text-[15px] font-medium text-base-100">{title}</span>
        {sub && <span className={cx('block truncate text-sm', subTone ?? 'text-base-400')}>{sub}</span>}
      </span>
      {end}
    </>
  )
}

function Row({ to, icon, title, sub, end }: { to: string; icon?: ReactNode; title: ReactNode; sub?: ReactNode; end?: ReactNode }) {
  return (
    <Link to={to} className={rowClass}>
      <RowBody
        icon={icon}
        title={title}
        sub={sub}
        end={
          <>
            {end}
            <Chevron />
          </>
        }
      />
    </Link>
  )
}

function IconChip({ color, children }: { color: string; children: ReactNode }) {
  return <span className={cx('grid size-8 shrink-0 place-items-center rounded-lg text-white', color)}>{children}</span>
}

function AccountRow() {
  const { user, signedIn, attention } = useAccount()
  return (
    <Link
      to="/settings?tab=Accounts"
      className="-mx-1 flex items-center gap-3 rounded-card px-1 py-2 transition-colors hover:bg-base-900 focus-visible:bg-base-900 focus-visible:outline-hidden"
    >
      <Avatar name={user} className="size-14" letter="text-2xl" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-lg font-semibold text-base-100">
          {user ?? (signedIn.length > 0 ? 'Signed in' : 'Not signed in')}
        </span>
        {attention ? (
          <span className="block truncate text-sm text-amber-300">Reconnect to resume syncing</span>
        ) : (
          <span className="block truncate text-sm text-base-400">
            {signedIn.length > 0 ? signedIn.map((t) => t.name).join(' · ') : 'Connect AniList to sync your list'}
          </span>
        )}
      </span>
      <Chevron />
    </Link>
  )
}

function DownloadsRow({ useLine }: { useLine: () => DownloadsLine }) {
  const { activity, words } = useLine()
  return (
    <li>
      <Row
        to="/downloads"
        icon={
          <IconChip color="bg-[#2f7d5b]">
            <DownloadIcon />
          </IconChip>
        }
        title="Downloads"
        sub={words || undefined}
        end={<MiniRing activity={activity} />}
      />
    </li>
  )
}

function NotificationsRow() {
  const unread = useNotifications().data?.unread ?? 0
  return (
    <li>
      <Row
        to="/notifications"
        icon={
          <IconChip color="bg-[#c0623a]">
            <BellIcon />
          </IconChip>
        }
        title="Notifications"
        sub={unread > 0 ? `${unread} new` : 'Nothing new'}
        end={unread > 0 && <Badge tone="sky">{unread}</Badge>}
      />
    </li>
  )
}

function SettingsRow() {
  const fresh = useNewSettings().unseen
  return (
    <li>
      <Row
        to={fresh ? '/settings?tab=New' : '/settings'}
        icon={
          <IconChip color="bg-[#5d6475]">
            <GearIcon />
          </IconChip>
        }
        title="Settings"
        end={fresh && <span className="rounded bg-accent-500/20 px-1.5 py-0.5 text-xs font-medium text-accent-200">New</span>}
      />
    </li>
  )
}

function TitlesRow() {
  const { japanese, settling, choose } = useTitleLanguage()
  return (
    <li className="flex min-h-14 items-center gap-3 px-4 py-2">
      <span className="min-w-0 flex-1">
        <span id="you-titles" className="block text-[15px] font-medium text-base-100">
          Titles
        </span>
        <span className="block text-sm text-base-400">How show names are written</span>
      </span>
      <span role="radiogroup" aria-labelledby="you-titles" className="grid w-32 shrink-0 grid-cols-2 gap-0.5 rounded-xl bg-base-800 p-1">
        {(['EN', 'JP'] as const).map((label) => {
          const on = (label === 'JP') === japanese
          return (
            <button
              key={label}
              type="button"
              role="radio"
              aria-checked={on}
              aria-label={label === 'EN' ? 'English' : 'Japanese (romaji)'}
              disabled={settling}
              onClick={() => !on && choose(label === 'JP')}
              className={cx(
                'tap relative h-8 rounded-lg text-sm font-semibold transition-colors pointer-coarse:h-9',
                on ? 'bg-base-750 text-white' : 'text-base-400 hover:text-base-100',
              )}
            >
              {label}
            </button>
          )
        })}
      </span>
    </li>
  )
}

function installNote(state: InstallState, wide: boolean): string {
  switch (state) {
    case 'ready':
      return wide ? 'Opens in its own window, like an app' : 'Opens from your home screen, like an app'
    case 'share-sheet':
      return 'In Safari, tap Share, then Add to Home Screen'
    case 'https':
      return "Needs Yoru's secure address first"
    default:
      return "From your browser's menu: Install app"
  }
}

function InstallRow() {
  const state = useInstall()
  const navigate = useNavigate()
  const wide = useMediaQuery('(min-width: 40rem)')
  if (state === 'installed') return null
  return (
    <li>
      <button
        type="button"
        onClick={() => {
          if (state === 'ready') void install()
          else navigate('/settings?tab=Devices')
        }}
        className={rowClass}
      >
        <RowBody title="Install Yoru" sub={installNote(state, wide)} end={<Chevron />} />
      </button>
    </li>
  )
}

function ExitRow({ onAsk }: { onAsk: (stops: ExitStops) => void }) {
  const { summary, choose, exiting, checking, failed } = useExitCheck(onAsk)
  return (
    <li>
      <button type="button" onClick={() => void choose()} disabled={exiting || checking} className={rowClass}>
        <RowBody
          title={exiting ? 'Yoru is exiting…' : 'Exit Yoru'}
          sub={failed || (!exiting && summary) || undefined}
          subTone={failed ? 'text-recap' : undefined}
        />
      </button>
    </li>
  )
}

const stroke = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.9, strokeLinecap: 'round', strokeLinejoin: 'round' } as const

function DownloadIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4.5" aria-hidden>
      <path d="M12 4v11m0 0 4-4m-4 4-4-4M5 19h14" {...stroke} />
    </svg>
  )
}

function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4.5" aria-hidden>
      <path d="M6 16V11a6 6 0 0 1 12 0v5l1.5 2h-15L6 16ZM10 20.5a2 2 0 0 0 4 0" {...stroke} />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4.5" aria-hidden>
      <circle cx="12" cy="12" r="3" {...stroke} />
      <path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1" {...stroke} />
    </svg>
  )
}
