// Each device saved its settings (a phone, its pairing) under kuro's names; they move to Yoru's once, before
// anything reads them, which is why main.tsx imports this first.
try {
  for (const key of Object.keys(localStorage)) {
    if (!key.startsWith('kuro.')) continue
    const next = `yoru.${key.slice('kuro.'.length)}`
    const value = localStorage.getItem(key)
    if (value !== null && localStorage.getItem(next) === null) localStorage.setItem(next, value)
    localStorage.removeItem(key)
  }
} catch {
  // Storage blocked (a private window): nothing was saved to move.
}
