import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import MediaPreview from './MediaPreview'
import FileView from './FileView'

const { loaded } = vi.hoisted(() => ({ loaded: vi.fn() }))
vi.mock('./ModelViewer', () => {
  loaded()
  return { default: ({ src, name }: { src: string; name: string }) => <div aria-label={`3D ${name}`} data-src={src} /> }
})
afterEach(cleanup)

it('opens videos in file windows with native controls and loads the 3D viewer only for models', async () => {
  const request = { id: 'video', name: 'turntable.mp4', rawUrl: '/api/files/raw?path=%2Fturntable.mp4', load: async () => ({ kind: 'video' as const }) }
  const { rerender } = render(<FileView request={request} preview={{ kind: 'video' }} error="" mode="preview" />)
  const video = screen.getByLabelText('turntable.mp4') as HTMLVideoElement
  expect(video.tagName).toBe('VIDEO')
  expect(video.controls).toBe(true)
  expect(video.preload).toBe('metadata')
  expect(video.autoplay).toBe(false)
  expect(video).toHaveAttribute('src', request.rawUrl)
  expect(loaded).not.toHaveBeenCalled()

  rerender(<MediaPreview kind="model" src="/api/files/raw?path=%2Fchair.glb" name="chair.glb" />)
  expect(screen.queryByLabelText('3D chair.glb')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Preview 3D model chair.glb' }))
  expect(await screen.findByLabelText('3D chair.glb')).toHaveAttribute('data-src', '/api/files/raw?path=%2Fchair.glb')
  expect(loaded).toHaveBeenCalledOnce()
})
