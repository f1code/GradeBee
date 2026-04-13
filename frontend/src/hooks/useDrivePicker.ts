import { useCallback, useRef } from 'react'

declare global {
  interface Window {
    gapi: {
      load(api: string, callback: () => void): void
    }
    google: {
      picker: {
        PickerBuilder: new () => PickerBuilder
        ViewId: { DOCS: string }
        Action: { PICKED: string; CANCEL: string }
        Feature: { NAV_HIDDEN: string }
      }
    }
  }
}

interface PickerBuilder {
  addView(view: unknown): PickerBuilder
  setOAuthToken(token: string): PickerBuilder
  setDeveloperKey(key: string): PickerBuilder
  setAppId(appId: string): PickerBuilder
  setCallback(callback: (data: PickerCallbackData) => void): PickerBuilder
  enableFeature(feature: string): PickerBuilder
  setTitle(title: string): PickerBuilder
  build(): { setVisible(visible: boolean): void }
}

interface PickerCallbackData {
  action: string
  docs?: Array<{
    id: string
    name: string
    mimeType: string
  }>
}

interface PickerResult {
  id: string
  name: string
}

const GOOGLE_CLIENT_ID = import.meta.env.VITE_GOOGLE_CLIENT_ID ?? ''

// Audio MIME types to filter in Google Picker
export const AUDIO_MIME_TYPES = [
  'audio/mpeg',
  'audio/mp4',
  'audio/mp3',
  'audio/x-m4a',
  'audio/wav',
  'audio/webm',
  'audio/ogg',
  'audio/aac',
  'audio/3gpp',
  'video/webm',
].join(',')

function loadPickerApi(): Promise<void> {
  return new Promise((resolve, reject) => {
    if (window.google?.picker) {
      resolve()
      return
    }
    if (!window.gapi) {
      reject(new Error('Google API script not loaded'))
      return
    }
    window.gapi.load('picker', () => {
      if (window.google?.picker) {
        resolve()
      } else {
        reject(new Error('Failed to load Google Picker API'))
      }
    })
  })
}

export function useDrivePicker() {
  const pickerRef = useRef<{ setVisible(v: boolean): void } | null>(null)

  const openPicker = useCallback(
    async (
      accessToken: string,
      options?: { mimeTypes?: string; title?: string; multiSelect?: boolean }
    ): Promise<PickerResult[] | null> => {
      await loadPickerApi()

      const mimeTypes = options?.mimeTypes ?? AUDIO_MIME_TYPES
      const title = options?.title ?? 'Select an audio file'
      const multiSelect = options?.multiSelect ?? false

      return new Promise((resolve) => {
        const view = new window.google.picker.PickerBuilder()
          .addView(
            // Create a view filtered to the requested MIME types
            (() => {
              // The DocsView constructor accepts a ViewId
              const docsView = new (window.google as unknown as Record<string, Record<string, new (id: unknown) => Record<string, (v: unknown) => void>>>).picker.DocsView(
                window.google.picker.ViewId.DOCS
              )
              docsView.setMimeTypes(mimeTypes)
              docsView.setMode((window.google as unknown as Record<string, Record<string, Record<string, unknown>>>).picker.DocsViewMode.LIST)
              return docsView
            })()
          )
          .setOAuthToken(accessToken)
          .enableFeature(window.google.picker.Feature.NAV_HIDDEN)

        if (multiSelect) {
          view.enableFeature((window.google.picker.Feature as unknown as Record<string, string>).MULTISELECT_ENABLED)
        }

        view
          .setTitle(title)
          .setCallback((data: PickerCallbackData) => {
            if (data.action === window.google.picker.Action.PICKED && data.docs?.length) {
              resolve(data.docs.map(doc => ({ id: doc.id, name: doc.name })))
            } else if (data.action === window.google.picker.Action.CANCEL) {
              resolve(null)
            }
          })

        // Set App ID (project number) from the client ID if available
        if (GOOGLE_CLIENT_ID) {
          view.setAppId(GOOGLE_CLIENT_ID.split('-')[0] || '')
        }

        const picker = view.build()
        pickerRef.current = picker
        picker.setVisible(true)
      })
    },
    []
  )

  return { openPicker }
}
