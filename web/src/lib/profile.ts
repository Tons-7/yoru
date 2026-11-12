import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, fetchAuthed } from './api'
import { readKey, writeKey } from './storage'

const ROOM_NAME_KEY = 'yoru.together.name'

export type NameFrom = 'anilist' | 'mal' | 'own'

export interface Profile {
  name: string
  nameFrom: NameFrom
  shown: string
  avatar: '' | 'own' | 'anilist' | 'mal'
  version: string
}

export interface Crop {
  x: number
  y: number
  side: number
}

const AVATAR_PX = 128
const AVATAR_BYTES = 30_000

export function useProfile() {
  return useQuery({
    queryKey: ['profile'],
    queryFn: () => api.get<Profile>('/api/profile'),
    staleTime: 10 * 60_000,
  })
}

export function useAvatarUrl(): string | undefined {
  const profile = useProfile().data
  return profile?.avatar ? `/api/profile/avatar?v=${profile.version}` : undefined
}

export function renderAvatar(picture: CanvasImageSource & { width: number; height: number }, crop?: Crop): string {
  const side = crop?.side ?? Math.min(picture.width, picture.height)
  const x = crop?.x ?? (picture.width - side) / 2
  const y = crop?.y ?? (picture.height - side) / 2
  const canvas = document.createElement('canvas')
  canvas.width = AVATAR_PX
  canvas.height = AVATAR_PX
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error("This browser can't prepare the picture.")
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(picture, x, y, side, side, 0, 0, AVATAR_PX, AVATAR_PX)
  for (const [type, quality] of [['image/webp', 0.86], ['image/jpeg', 0.82], ['image/jpeg', 0.6]] as const) {
    const url = canvas.toDataURL(type, quality)
    if (url.startsWith(`data:${type}`) && url.length * 0.75 < AVATAR_BYTES) return url
  }
  throw new Error("That picture couldn't be made small enough.")
}

export function useAvatarEdit() {
  const qc = useQueryClient()
  const done = (next: Profile) => qc.setQueryData(['profile'], next)
  const upload = useMutation({
    meta: { inline: true },
    mutationFn: (image: string) => api.put<Profile>('/api/profile/avatar', { image }),
    onSuccess: done,
  })
  const remove = useMutation({
    meta: { inline: true },
    mutationFn: () => api.del<Profile>('/api/profile/avatar'),
    onSuccess: done,
  })
  return { upload, remove }
}

export function useRename() {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline: true },
    scope: { id: 'profile-name' },
    mutationFn: (name: string) => api.put<Profile>('/api/profile/name', { name }),
    onSuccess: (next) => qc.setQueryData(['profile'], next),
  })
}

export function useNameFrom() {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline: true },
    scope: { id: 'profile-name' },
    mutationFn: (v: { from?: NameFrom; name?: string }) => api.put<Profile>('/api/profile/name', v),
    onSuccess: (next) => qc.setQueryData(['profile'], next),
  })
}

export function useRoomName(fallback: string, host: boolean | undefined) {
  const profile = useProfile().data
  const rename = useRename()
  const name = (host === true ? '' : readKey(ROOM_NAME_KEY)) || profile?.shown || fallback
  const remember = (next: string) => {
    if (next === name) return
    if (host === true) rename.mutate(next)
    else writeKey(ROOM_NAME_KEY, next)
  }
  return { name, remember }
}

export async function avatarForRoom(): Promise<string | undefined> {
  try {
    const res = await fetchAuthed('/api/profile/avatar')
    if (!res.ok) return undefined
    const bitmap = await createImageBitmap(await res.blob())
    const url = renderAvatar(bitmap)
    bitmap.close()
    return url
  } catch {
    return undefined
  }
}
