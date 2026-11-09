import { copyFileSync, linkSync, mkdirSync, readFileSync, readdirSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const WRITTEN_BY_YORU = new Set(['versions.json'])
const INSTALLED_BY_TESTS = new Set(['flyhigh'])

export function linkBin(from, to, only = null) {
  const exe = (n) => (process.platform === 'win32' ? `${n}.exe` : n)
  mkdirSync(to, { recursive: true })
  for (const entry of readdirSync(from, { withFileTypes: true })) {
    if (only && !only.some((n) => entry.name === exe(n) || entry.name === n)) continue
    if (INSTALLED_BY_TESTS.has(entry.name)) continue
    const src = join(from, entry.name)
    const dst = join(to, entry.name)
    if (entry.isDirectory()) {
      symlinkSync(src, dst, process.platform === 'win32' ? 'junction' : 'dir')
      continue
    }
    if (WRITTEN_BY_YORU.has(entry.name)) {
      let versions
      try {
        versions = JSON.parse(readFileSync(src, 'utf8').replace(/^﻿/, ''))
      } catch (err) {
        throw new Error(`cannot read ${src} to leave out what the tests install: ${err.message}`)
      }
      for (const name of INSTALLED_BY_TESTS) delete versions[name]
      writeFileSync(dst, JSON.stringify(versions, null, 4))
      continue
    }
    try {
      linkSync(src, dst)
    } catch {
      copyFileSync(src, dst)
    }
  }
}

export async function prepareLocalEpisode({ base, lib, anime, autoplay = true }) {
  const post = (path, body) =>
    fetch(base + path, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    })

  for (let i = 0; i < 60; i++) {
    try {
      if ((await fetch(`${base}/api/setup`)).ok) break
    } catch {}
    await sleep(1000)
  }
  await post('/api/local/paths', { paths: [lib] })
  await post('/api/local/scan', {})
  let file
  for (let i = 0; i < 30 && !file; i++) {
    await sleep(1000)
    const files = await (await fetch(`${base}/api/local/files`)).json()
    file = (files.items ?? []).find((f) => String(f.path ?? '').includes('Yoru Test Show'))
  }
  if (!file) return false
  const assign = await post('/api/local/assign', { id: file.id, animeId: Number(anime), episode: 1 })
  if (autoplay) await post('/api/prefs', { key: 'playback.autoplay', value: 'true' })
  return assign.ok
}
