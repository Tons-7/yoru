import { createServer } from 'node:http'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, readdirSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'

const UPSTREAM = 'https://graphql.anilist.co'
const DAY = 24 * 60 * 60 * 1000
const dir = join(tmpdir(), 'yoru-e2e-anilist')
mkdirSync(dir, { recursive: true })
for (const name of readdirSync(dir)) {
  try {
    const file = join(dir, name)
    if (Date.now() - statSync(file).mtimeMs >= DAY) rmSync(file, { force: true })
  } catch {
  }
}

const cached = (file) => {
  try {
    if (Date.now() - statSync(file).mtimeMs < DAY) return readFileSync(file)
  } catch {
  }
  return null
}

const server = createServer(async (req, res) => {
  const chunks = []
  for await (const c of req) chunks.push(c)
  const body = Buffer.concat(chunks)
  const key = createHash('sha1').update(req.headers.authorization ?? '').update(body).digest('hex')
  const file = join(dir, `${key}.json`)

  const hit = req.method === 'POST' ? cached(file) : null
  if (hit) {
    res.writeHead(200, { 'Content-Type': 'application/json' }).end(hit)
    return
  }

  try {
    const headers = { 'Content-Type': 'application/json', Accept: 'application/json' }
    for (const h of ['authorization', 'referer', 'user-agent']) if (req.headers[h]) headers[h] = req.headers[h]
    const up = await fetch(UPSTREAM + (req.url === '/' ? '' : req.url), {
      method: req.method,
      headers,
      body: req.method === 'POST' ? body : undefined,
    })
    const answer = Buffer.from(await up.arrayBuffer())
    const pass = {}
    for (const h of ['content-type', 'retry-after', 'x-ratelimit-remaining', 'x-ratelimit-reset']) {
      if (up.headers.get(h)) pass[h] = up.headers.get(h)
    }
    if (req.method === 'POST' && up.status === 200 && !answer.includes('"errors"')) {
      const tmp = `${file}.${process.pid}.tmp`
      writeFileSync(tmp, answer)
      renameSync(tmp, file)
    }
    res.writeHead(up.status, pass).end(answer)
  } catch (err) {
    res.writeHead(502, { 'Content-Type': 'application/json' }).end(JSON.stringify({ errors: [{ message: String(err) }] }))
  }
})

server.listen(Number(process.argv[2]), '127.0.0.1')
