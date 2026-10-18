// The text helpers in src/lib/format.ts, run straight by Node: node scripts/format-check.mjs
const { bytes, episodeSpan, relativeTime, speed, speedLimit } = await import(new URL('../src/lib/format.ts', import.meta.url))

let failures = 0
const check = (got, want, label) => {
  if (got === want) return console.log(`ok   ${label}`)
  failures++
  console.log(`FAIL ${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
}

const now = Date.now() / 1000
check(relativeTime(now + 30), 'in 1m', 'under a minute reads as a minute')
check(relativeTime(now + 59.6 * 60), 'in 1h', '59.6 minutes rounds to an hour, not "60m"')
check(relativeTime(now + 2 * 3600 + 59.7 * 60), 'in 3h', 'never "2h 60m"')
check(relativeTime(now - 90 * 60), '1h 30m ago', 'hours and minutes in the past')
check(relativeTime(now + 3 * 86400), 'in 3d', 'days')

check(episodeSpan(Array.from({ length: 22 }, (_, i) => i + 1)), 'Episodes 1–22', 'a run of episodes is one span')
check(episodeSpan([12, 10, 1, 2, 3, 11, 8]), 'Episodes 1–3, 8, 10–12', 'gaps split the spans, in order')
check(episodeSpan([4]), 'Episode 4', 'one episode is named alone')
check(episodeSpan([1, 3, 5, 7, 9, 11]), '6 episodes', 'too many gaps become a count')
check(episodeSpan([5, 5, 6]), 'Episodes 5–6', 'a repeated episode counts once')

check(speed(12.44), '12.4 Mbps', 'megabits unless asked otherwise')
check(speed(12.8, 'mbs'), '1.5 MB/s', 'megabytes, as file sizes count them')
check(speed(8.388608, 'mbs'), '1.0 MB/s', 'a megabyte a second is the megabyte bytes() shows')
check(bytes(1024 ** 2), '1.0 MB', 'and bytes() agrees')
check(speed(1.44, 'mbs'), '176 KB/s', 'under one megabyte, kilobytes')
check(speed(8.19, 'mbs'), '1.0 MB/s', 'never "1000 KB/s"')
check(speedLimit(1_250_000), '10 Mbps', 'a limit in megabits')
check(speedLimit(1_250_000, 'mbs'), '1.2 MB/s', 'the same limit in megabytes')
check(speedLimit(2 * 1024 ** 2, 'mbs'), '2 MB/s', 'a round limit has no decimals')

console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
