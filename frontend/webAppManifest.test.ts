import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const frontendRoot = dirname(fileURLToPath(import.meta.url))

describe('web app install metadata', () => {
  it('links the manifest and apple-touch-icon from index.html', () => {
    const html = readFileSync(join(frontendRoot, 'index.html'), 'utf8')
    expect(html).toContain('<link rel="manifest" href="/manifest.json" />')
    expect(html).toContain('<link rel="apple-touch-icon" href="/apple-touch-icon.png" />')
  })

  it('declares standalone display and three icon entries in the manifest', () => {
    const manifest = JSON.parse(readFileSync(join(frontendRoot, 'public/manifest.json'), 'utf8')) as {
      display: string
      icons: Array<{ src: string; sizes: string; purpose: string }>
    }
    expect(manifest.display).toBe('standalone')
    expect(manifest.icons).toEqual([
      { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
      { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
      { src: '/icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
    ])
  })
})
