export const meta = {
  name: 'yoru-audit',
  description: 'Audit all of Yoru (backend, BitTorrent engine, frontend, scripts) for bugs, modernization, UI/UX and suggestions; adversarially verify each finding',
  phases: [
    { title: 'Find', detail: 'one auditor per subsystem, plus UI/UX, modernization, test and product reviewers' },
    { title: 'Verify', detail: 'skeptics try to refute every finding; high/critical bugs get two lenses' },
    { title: 'Critic', detail: 'look for under-covered areas and audit them too' },
  ],
}

const CONTEXT = `Yoru (called kuro until October 2026) is a self-hosted anime streamer, Windows first with macOS/Linux builds (not yet run on those OSes); this repository is its source. Go 1.26 backend (internal/, cmd/yoru) serving a React 19 + TypeScript 7 + Tailwind 4 SPA (web/src, built by Vite and embedded into yoru.exe). It searches public torrent indexers (internal/indexer), scores releases (internal/score), and downloads through its OWN BitTorrent engine: internal/bt (+ bt/dht, bt/storage and the protocol packages) began as a fork of anacrolix/torrent and is now Yoru's code with no upstream; Yoru changed its request scheduling for streaming (queues refill at half, speed-sized queues, speed-aware steals, urgent reader-first requests, cancel-to-front, FIFO uploads). internal/engine runs it in process behind a loopback HTTP API (session in cache/engine-session, an old rqbit session imported once, optional binding to a VPN network). Playback: ffmpeg -> HLS in the browser (internal/transcode; copied video is cut on the Matroska Cues keyframes and served straight from per-keyframe parts), or mpv/VLC (internal/player). Syncs progress to AniList/MAL; Discord Rich Presence; an Up next list; device access for phones/TVs with pairing tokens and optional per-device approval; HTTPS for the PWA via a built-in DuckDNS + Let's Encrypt client (internal/certs); a signed self-updater (internal/update, scripts/package.ps1, scripts/signsums). SQLite store in internal/store; migrations in internal/db/migrations (applied migrations are never edited, fixes go in new files). Owner's design rules: highest quality always (resolution outranks every other release key); a labelled single episode beats a season pack; per-show preferences override the global ones; code comments must be terse; UI rows are "name · details", 44px touch targets, nothing hover-only, no em dashes in interface text. Past incident: a second instance shared the torrent session and its cache sweeper deleted the user's downloaded files, so anything that can delete or orphan user files deserves extra scrutiny.`

const RULES = `Hard rules:
- READ-ONLY. Do not create, modify or delete any file in this repository. No git commit/push/checkout/stash/reset.
- Do NOT run yoru.exe or kuro.exe, npm build, the e2e harness (web/scripts/*.mjs) or live tests (YORU_LIVE). Never contact 127.0.0.1:4321: that is the user's live app.
- Allowed: reading files, grep, git log/blame/show, go vet, running EXISTING unit tests for one package (go test ./internal/<pkg> -run <Name>, or -cover), and web research on public docs and release notes. Never send repository content or user data anywhere.
- Be concrete. Every item needs the exact file and line, a concrete trigger -> outcome, and evidence quoted or traced from the current code. No speculative "might" items without a traced path.`

const BUG_LENS = 'Real bugs (wrong result, crash, race, leak, data loss), missing error handling that causes a user-visible failure, security holes, performance problems with a concrete cost, and simplifications that remove real complexity. Not style nits. kind is "bug" for defects, "improvement" for performance/simplification/robustness.'
const BT_LENS = BUG_LENS + ' This is a long-running streaming client: look hard at lock ordering and deadlocks, goroutine and connection leaks, unbounded maps/queues fed by peers, timers that never stop, request bookkeeping that can strand a piece (the stuck-at-99% class), and anything that slows the piece the player is waiting for.'
const INPUT_LENS = BUG_LENS + ' Everything parsed here comes from strangers on the internet (peers, trackers, DHT nodes, .torrent files, magnet links): find panics, unbounded allocations, integer overflows, slices indexed from untrusted lengths, and loops a hostile peer can drive. State concretely what the attacker sends and what happens to Yoru.'
const FE_BUG_LENS = 'Real frontend bugs: stale or wrong data shown, state that desyncs from the server, effects that leak or double-fire, races between requests, broken navigation or URL state, unhandled errors that leave the UI stuck, and performance issues with a concrete cost. kind is "bug" or "improvement".'
const UI_LENS = 'UI/UX improvements a user would notice: missing or confusing feedback (loading, empty, error, success states), unclear labels or affordances, hierarchy/layout problems, narrow-width breakage (<=400px), accessibility (keyboard access, focus visibility, labels, contrast), destructive actions without confirmation or undo, features that are hard to discover. You are reading code, not seeing the rendered page: ground every claim in the markup/classes you read. Each item names the exact component file and line and the concrete change. kind must be "ui". Severity: high = broken or confusing for most users, medium = noticeable friction, low = polish.'
const MODERN_LENS = 'Modernization with a concrete payoff: code that a newer language or library feature makes shorter or safer (Go 1.22-1.26: range-over-func iterators, slices/maps/cmp, min/max, sync.OnceValue, context.AfterFunc, errors.Join, os.Root, testing/synctest, structured logging; React 19: use, actions, useOptimistic, ref as prop; TanStack Query 5, React Router 7, Tailwind 4, TypeScript 7), deprecated or soon-removed APIs still in use, dead code, and dependencies in go.mod / web/package.json that are behind with a fix or feature Yoru would use (check their release notes). kind is "modernization". Not churn for its own sake: each item says what gets better.'
const TEST_LENS = 'Test gaps that matter: risky code paths (deleting files, money-like counters such as progress and watched state, tracker sync, the updater, request scheduling, segment planning, access control) with no test or only a happy-path test, tests that cannot fail (no assertions, asserting on mocks), and flaky timing in tests. Use `go test -cover` on a package if useful. kind is "test"; name the exact function to cover and the case to add.'
const SUGGEST_LENS = 'Product suggestions grounded in the code: features a self-hosted anime streamer user would expect that Yoru lacks or half-has (compare with what Jellyfin, Stremio, Hayase/Miru and Plex do), rough edges in existing flows, and settings that should exist or should not. Read the relevant pages and handlers first so you never suggest something that is already there. kind is "suggestion"; say what the user gets, where it would live, and roughly how big it is. Severity: high = most users would want it, medium = many, low = some.'

const AREAS = [
  // Backend: the library and its flows.
  { name: 'playback', files: 'internal/library/play.go, prefetch.go, queue.go, watcher.go, stage.go, prefs.go; internal/server/play.go, watch.go, upnext.go; internal/store/upnext.go, queue.go, playback.go', focus: 'starting playback, trying release candidates in turn, prefetching the next episode, the download queue, Up next, the watcher that auto-downloads new episodes', lens: BUG_LENS },
  { name: 'release-finding', files: 'internal/library/episode.go, identity.go, corpus.go, filmseries.go, relations.go; internal/film/; internal/score/; internal/parse/; internal/match/; internal/corpus/; internal/indexer/', focus: 'searching indexers for an episode, parsing release names, matching releases to the right show/cour/episode/film, ranking them, and indexer HTTP/RSS parsing', lens: BUG_LENS + ' Include misparses and misranks: think of real release naming shapes that would parse or rank wrong.' },
  { name: 'downloads-lifecycle', files: 'internal/library/cache.go, diskusage_*.go, scan.go; internal/store/cache.go, checkpoint.go, local.go; internal/torrent/; internal/server/download.go, local.go and the downloads/cache handlers', focus: 'download records, the kept tier, the cache-budget sweeper, auto-delete, orphan cleanup, local library folders, reconciling with the engine across restarts', lens: BUG_LENS + ' Anything that can delete or orphan a user file, or lose a download record, is at least high severity.' },
  { name: 'engine', files: 'internal/engine/ (api.go, engine.go, session.go, storage.go, sparse_*.go, inuse_*.go, layout.go, regroup.go, census.go, network.go, peers.go, torrents.go, watched.go)', focus: 'the in-process engine: its loopback API, session persistence and the rqbit import, file layout and regrouping, sparse files and in-use checks, VPN network binding, peer limits, what happens on restart, crash or a full disk', lens: BUG_LENS + ' Anything that can delete or orphan a user file is at least high severity; check the VPN binding never leaks traffic onto the normal network.' },

  // Backend: Yoru's own BitTorrent engine.
  { name: 'bt-requests-reader', files: 'internal/bt/ requesting.go, request-strategy-impls.go, request-queue.go, client-piece-request-order.go, torrent-piece-request-order.go, reader.go, ratelimitreader.go, piece.go, piecestate.go, undirtied-chunks-iter.go, write-status.go, storage.go, bad-storage.go, smartban.go, smartban/, segments/, webseed-*.go, webseed/', focus: 'how pieces are chosen and requested, reader priorities for streaming and seeks, piece hashing and failure handling, smart bans, webseeds', lens: BT_LENS },
  { name: 'bt-peers-client', files: 'internal/bt/ client.go, torrent.go, peerconn.go, peer.go, peer-impl.go, peer-conn-msg-writer.go, handshake.go, dial-pool.go, dialer.go, dialer/, listen.go, socket.go, sockopts*.go, utp*.go, pex.go, pexconn.go, ltep.go, ut-holepunching.go, worse-conns.go, prioritized-peers.go, portfwd.go, client-tracker-announcer.go, torrent-regular-tracker-announcer.go, tracker_scraper.go, wstracker.go, webrtc.go, webtorrent/, config.go, rate.go, stats files', focus: 'client and torrent lifecycle, connecting, choking and dropping peers, upload limits, announces, port forwarding, shutdown', lens: BT_LENS },
  { name: 'bt-storage', files: 'internal/bt/storage/, internal/bt/mmap-span/, internal/bt/file.go, internal/bt/storage.go', focus: 'how pieces reach disk: file handles, partial pieces, completion state, mmap, sparse files, renames and deletes, Windows file sharing, behaviour on a full disk or a file removed underneath', lens: BUG_LENS + ' Data loss, corrupted completion state and leaked handles that block deletes are at least high severity.' },
  { name: 'bt-untrusted-input', files: 'internal/bt/bencode/, metainfo/, peer_protocol/, mse/, dht/, tracker/, iplist/, merkle/, typed-roaring/, types/, and the message handling in internal/bt/peerconn.go and ltep.go', focus: 'parsing everything that comes off the network: bencode, .torrent and magnet metadata, the peer wire protocol and extensions, message stream encryption, DHT KRPC, tracker responses, block lists', lens: INPUT_LENS },

  // Backend: data, API, streaming, trackers, platform.
  { name: 'store-sql', files: 'internal/store/ (all files); internal/db/ and internal/db/migrations/', focus: 'SQL correctness, transactions, NULL handling, pagination totals, indexes, migration safety, connection-pool use', lens: BUG_LENS },
  { name: 'http-api-security', files: 'internal/server/ (server.go, access.go, approval.go, auth.go, tls.go, network.go, served.go, spa.go, setup.go, prefs.go, local.go, external.go, export.go, browse.go, discover.go, random.go, schedule.go, studio.go, history.go, trackers.go, mal.go, malextra.go, corpus.go, update.go); internal/certs/; internal/firewall/', focus: 'HTTP handlers: input validation, error handling, the LAN/remote access model, pairing and approval tokens, the open-in-app links, TLS and the DuckDNS/ACME client, firewall rules, path handling for local files, requests forged by other websites against the local server (CSRF, DNS rebinding), information leaks', lens: BUG_LENS + ' For security, state concretely who can reach the endpoint and what they can do with it. Treat the DuckDNS token and tracker tokens as secrets.' },
  { name: 'streaming-transcode', files: 'internal/server/stream.go; internal/transcode/ (especially keyframes.go, fmp4.go, sliced.go, segment_reader.go, hls.go, args.go, probe.go, livesubs.go, audio.go, voice.go, encoder.go, thumbs.go, subtitles.go, assmerge.go); internal/sound/; internal/player/', focus: 'ffmpeg HLS sessions, keyframe-planned segments and the parts they are served from, seek restarts, subtitle and font extraction, sound options, thumbnails, external players (mpv, VLC) and their control channels', lens: BUG_LENS + ' Look hard at process and goroutine lifecycles, leaks, races between concurrent requests, and files removed while a reader still has them open.' },
  { name: 'trackers-metadata', files: 'internal/anilist/; internal/mal/; internal/metadata/; internal/library/sync.go, enrich.go, import.go, mal.go, remember.go, presence.go; internal/store/tracker.go, sync.go, answers.go, freshness.go, malonly.go, title.go; internal/discord/', focus: 'AniList/MAL auth and token refresh, list import and two-way sync, rate limiting and the saved copy used during outages, metadata enrichment and air dates, Discord presence', lens: BUG_LENS },
  { name: 'platform-startup', files: 'cmd/yoru/main.go; internal/config/; internal/deps/; internal/update/; internal/proc/; internal/jobs/; internal/shortcut/; every *_windows.go / *_other.go / *_unix.go file; scripts/package.ps1, build.ps1, build.sh, fetch-deps.ps1, signsums/', focus: 'startup and shutdown order, config loading, first-run dependency downloads, the self-updater (download, signature, swap, the kuro-to-Yoru migration), background jobs, the release packaging and signing, and whether the macOS/Linux paths would actually work', lens: BUG_LENS + ' For the updater, dependency installer and packaging, check integrity verification and failure recovery.' },

  // Frontend.
  { name: 'player-ui-logic', files: 'web/src/player/ (Player.tsx, hooks.ts, anime4k.ts, subtitle-canvas.ts, subtitle-worker.ts); web/src/pages/Watch.tsx', focus: 'the browser player: HLS lifecycle, effects and listeners, keyboard shortcuts, fullscreen and PiP, auto-next, subtitles and their worker, Anime4K, progress reporting', lens: FE_BUG_LENS },
  { name: 'pages-logic', files: 'web/src/pages/ (everything except Watch.tsx); web/src/App.tsx, main.tsx', focus: 'page logic: data fetching, query keys and invalidation, URL state, mutations, error and loading handling', lens: FE_BUG_LENS },
  { name: 'components-lib', files: 'web/src/components/; web/src/lib/', focus: 'shared components (UpNext, ReleasePicker, DownloadsPanel, NotificationPanel, AccessRequests, OpenInApp, Layout and the rest) and the API/query layer', lens: FE_BUG_LENS },
  { name: 'ui-discovery', files: 'web/src/pages/Home.tsx, Browse.tsx, Anime.tsx, Library.tsx, Schedule.tsx, Recent.tsx; web/src/components/ PosterCard, HoverInfo, Rail, CharacterRail, Hero, Sidebar, EpisodeList, ShowExtra, StudioFilter, FilterMenu, TrailerOverlay, the search in Layout.tsx', focus: 'browsing and discovery screens', lens: UI_LENS },
  { name: 'ui-watch-manage', files: 'web/src/pages/Watch.tsx and the player controls in web/src/player/; Manage.tsx (Downloads); Settings.tsx; History.tsx; Setup.tsx; web/src/components/ NotificationPanel, DownloadsPanel, UpNext, ReleasePicker, Anime4KDialog, OpenInApp, AccessRequests, Unpaired, Layout.tsx header, nav and profile menu', focus: 'watching, downloads, settings, history, setup and the app chrome', lens: UI_LENS },
  { name: 'ui-consistency', files: 'all of web/src, starting with web/src/components/ui.tsx, web/src/lib/ui.ts and web/src/index.css', focus: 'cross-cutting design consistency', lens: UI_LENS + ' Focus on inconsistencies ACROSS screens: competing button/badge/card styles, spacing and type-scale drift, colour/contrast tokens, icon usage, repeated markup that should be one component, and patterns handled well on one screen but badly on another.' },

  // Cross-cutting reviews.
  { name: 'modernization', files: 'go.mod, web/package.json, then the code across internal/, cmd/ and web/src', focus: 'language, library and dependency modernization', lens: MODERN_LENS },
  { name: 'test-gaps', files: 'every *_test.go beside the code it covers, and web/scripts/release-check.mjs for the browser checks', focus: 'what is risky and untested', lens: TEST_LENS },
  { name: 'suggestions', files: 'web/src/pages/, web/src/components/, internal/server/server.go (the route table), internal/library/, README.md', focus: 'what Yoru should do next for its users', lens: SUGGEST_LENS },
]

const FINDINGS = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title: { type: 'string' },
          kind: { type: 'string', enum: ['bug', 'improvement', 'ui', 'modernization', 'test', 'suggestion'] },
          category: { type: 'string' },
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
          file: { type: 'string' },
          line: { type: 'integer' },
          scenario: { type: 'string' },
          evidence: { type: 'string' },
          fix: { type: 'string' },
        },
        required: ['title', 'kind', 'category', 'severity', 'file', 'line', 'scenario', 'evidence', 'fix'],
      },
    },
    omitted: { type: 'integer' },
  },
  required: ['findings', 'omitted'],
}

const VERDICT = {
  type: 'object',
  properties: {
    verdict: { type: 'string', enum: ['confirmed', 'refuted', 'uncertain'] },
    severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] },
    reasoning: { type: 'string' },
    line: { type: 'integer' },
  },
  required: ['verdict', 'severity', 'reasoning'],
}

const GAPS = {
  type: 'object',
  properties: {
    gaps: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          files: { type: 'string' },
          focus: { type: 'string' },
          ui: { type: 'boolean' },
        },
        required: ['name', 'files', 'focus', 'ui'],
      },
    },
  },
  required: ['gaps'],
}

const finderPrompt = (a) => `${CONTEXT}

You are auditing the "${a.name}" area of Yoru: ${a.focus}.
Files: ${a.files}

Look for: ${a.lens}

${RULES}

Report the most important items first, at most 15. Put the number of lower-value items you left out in "omitted".`

const LENS_TEXT = {
  correctness: 'Your job is to REFUTE it if you can. Read the cited code and whatever surrounds and calls it. Refute if the scenario cannot happen (a guard, caller or invariant prevents it), if the code does not say what the report claims, or if it is already handled elsewhere. Confirm only if you can trace it in the current code. If you genuinely cannot decide, answer uncertain.',
  trace: 'Independently trace it end to end: start from the user action, request, peer message or scheduled job that would trigger it, follow the exact calls and state, and name the precise line where the outcome goes wrong. If any step of the path is blocked, refute it.',
  ui: 'Check this UI/UX suggestion against the current frontend code. Refute if the claim about the current UI is inaccurate (the element, state, label or style is actually present or handled), if it is already implemented elsewhere, or if it is too vague to act on. Confirm if the gap is real and the change is concrete. Judge accuracy and actionability, not taste.',
  value: 'Check this item against the current code and, where it names a version or API, against public docs. Refute if it is already done, if the claim about the code, the library or its version is wrong, if the payoff is cosmetic, or if it is too vague to act on. Confirm if it is accurate, concrete and worth doing.',
}

const verifyPrompt = (f, lens) => `${CONTEXT}

Verify this reported ${f.kind} in Yoru. ${LENS_TEXT[lens]}
Give the severity you would assign, and the corrected line if the cited one is off.

${RULES}

Reported item:
${JSON.stringify(f, null, 2)}`

const lensesFor = (f) => {
  if (f.kind === 'ui') return ['ui']
  if (f.kind === 'modernization' || f.kind === 'test' || f.kind === 'suggestion') return ['value']
  return f.severity === 'critical' || f.severity === 'high' ? ['correctness', 'trace'] : ['correctness']
}

const verifyAll = (res, a) => {
  if (!res) return []
  if (res.omitted) log(`${a.name}: ${res.omitted} lower-value item(s) left out by the finder`)
  log(`${a.name}: ${res.findings.length} to verify`)
  return parallel(res.findings.map((f, i) => () =>
    parallel(lensesFor(f).map((l) => () =>
      agent(verifyPrompt(f, l), { label: `verify:${a.name}#${i + 1}:${l}`, phase: 'Verify', schema: VERDICT, effort: 'high' })))
      .then((vs) => ({ ...f, area: a.name, verdicts: vs.filter(Boolean) }))))
}

const ORDER = ['low', 'medium', 'high', 'critical']
const classify = (f) => {
  const v = f.verdicts || []
  const yes = v.filter((x) => x.verdict === 'confirmed')
  const no = v.filter((x) => x.verdict === 'refuted')
  let status = 'refuted'
  if (yes.length && !no.length) status = 'confirmed'
  else if (yes.length && no.length) status = 'disputed'
  else if (!no.length && v.length) status = 'uncertain'
  const severity = yes.length
    ? yes.map((x) => x.severity).sort((p, q) => ORDER.indexOf(p) - ORDER.indexOf(q))[0]
    : f.severity
  return { ...f, status, severity, reportedSeverity: f.severity }
}

const norm = (p) => String(p || '').replace(/\\/g, '/').replace(/^.*?(internal\/|cmd\/|web\/|scripts\/)/, '$1').toLowerCase()
const dedup = (items) => {
  const kept = []
  const sorted = [...items].sort((p, q) => ORDER.indexOf(q.severity) - ORDER.indexOf(p.severity))
  for (const f of sorted) {
    const dup = kept.find((k) => norm(k.file) === norm(f.file) && Math.abs((k.line || 0) - (f.line || 0)) <= 3 && k.kind === f.kind)
    if (dup) { dup.alsoFoundBy = [...(dup.alsoFoundBy || []), f.area]; continue }
    kept.push(f)
  }
  return kept
}

phase('Find')
const round1 = await pipeline(
  AREAS,
  (a) => agent(finderPrompt(a), { label: `find:${a.name}`, phase: 'Find', schema: FINDINGS }),
  (res, a) => verifyAll(res, a),
)
let all = round1.filter(Boolean).flat().filter(Boolean)
log(`round 1: ${all.length} items verified`)

phase('Critic')
const summary = all.map((f) => `${f.area} | ${norm(f.file)}:${f.line} | ${f.title}`).join('\n')
const critic = await agent(`${CONTEXT}

An audit of Yoru just ran over these areas:
${AREAS.map((a) => `- ${a.name}: ${a.files}`).join('\n')}

It produced these items (area | file:line | title):
${summary}

Find what the audit under-covered. Run \`git ls-files internal cmd web/src scripts\` and compare: which source files or risk types got little or no scrutiny (files in no area, subsystems with suspiciously few findings, whole classes of issue nobody looked for)? Return up to 4 gaps worth a dedicated auditor, each with the files, the focus, and ui=true if it is a UI/UX review. Return an empty list if coverage is genuinely adequate.

${RULES}`, { label: 'critic', phase: 'Critic', schema: GAPS })

const gaps = (critic?.gaps || []).slice(0, 4).map((g) => ({
  name: `gap-${g.name}`, files: g.files, focus: g.focus, lens: g.ui ? UI_LENS : BUG_LENS,
}))
if (gaps.length) {
  log(`critic found ${gaps.length} gap(s): ${gaps.map((g) => g.name).join(', ')}`)
  const round2 = await pipeline(
    gaps,
    (a) => agent(finderPrompt(a), { label: `find:${a.name}`, phase: 'Find', schema: FINDINGS }),
    (res, a) => verifyAll(res, a),
  )
  all = all.concat(round2.filter(Boolean).flat().filter(Boolean))
}

const judged = all.map(classify)
const confirmed = dedup(judged.filter((f) => f.status === 'confirmed'))
const disputed = judged.filter((f) => f.status === 'disputed' || f.status === 'uncertain')
const refuted = judged.filter((f) => f.status === 'refuted')
log(`confirmed ${confirmed.length} (after dedup), disputed/uncertain ${disputed.length}, refuted ${refuted.length}`)

return {
  confirmed,
  disputed,
  refuted: refuted.map((f) => ({ area: f.area, file: f.file, line: f.line, title: f.title, why: (f.verdicts || []).map((v) => v.reasoning).join(' | ').slice(0, 400) })),
  gaps: gaps.map((g) => g.name),
}
