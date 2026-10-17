// Adapted from CHROTE dashboard/src/components/ModelViewer.tsx.
// Archon resolves glTF sidecars through its raw file routes and lazy-loads Three.js.
import { useEffect, useRef, useState } from 'react'
import { LoadingManager, Box3, HemisphereLight, DirectionalLight, Mesh, SkinnedMesh, PerspectiveCamera, Scene, Sphere, Texture, Vector3, WebGLRenderer } from 'three'
import type { Material, Object3D } from 'three'
import { FBXLoader } from 'three/addons/loaders/FBXLoader.js'
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import { MeshoptDecoder } from 'three/addons/libs/meshopt_decoder.module.js'
import { modelResourceUrl } from './mediaUrls'
import { ModelPreviewError, readModelData, requireFbxMesh } from './modelPreviewData'
import { modelHasTextures, restoreMissingModelMaterials } from './modelMaterials'
import './ModelViewer.css'

function disposeModel(model: Object3D, retiredMaterials: Set<Material>) {
  const materials = new Set(retiredMaterials)
  const textures = new Set<Texture>()
  model.traverse(object => {
    if (!(object instanceof Mesh)) return
    object.geometry.dispose()
    if (object instanceof SkinnedMesh) object.skeleton.dispose()
    for (const material of Array.isArray(object.material) ? object.material : [object.material]) {
      materials.add(material)
    }
  })
  for (const material of materials) {
    for (const value of Object.values(material)) if (value instanceof Texture) textures.add(value)
    material.dispose()
  }
  for (const texture of textures) {
    texture.dispose()
    if (typeof ImageBitmap !== 'undefined' && texture.image instanceof ImageBitmap) texture.image.close()
  }
}

export default function ModelViewer({ src, name }: { src: string; name: string }) {
  const stageRef = useRef<HTMLDivElement>(null)
  const resetRef = useRef<() => void>(() => {})
  const [status, setStatus] = useState('Loading 3D model…')
  const [textureNote, setTextureNote] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const stage = stageRef.current!
    const controller = new AbortController()
    let disposed = false
    let model: Object3D | undefined
    const retiredMaterials = new Set<Material>()
    let renderer: WebGLRenderer
    try {
      renderer = new WebGLRenderer({ antialias: true, alpha: true })
    } catch {
      setError('Could not open 3D model: WebGL is unavailable in this browser.')
      return
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    renderer.domElement.setAttribute('aria-label', `3D model ${name}`)
    stage.appendChild(renderer.domElement)
    const scene = new Scene()
    scene.add(new HemisphereLight(0xffffff, 0x888888, 3))
    const light = new DirectionalLight(0xffffff, 3)
    light.position.set(3, 5, 4)
    scene.add(light)
    const camera = new PerspectiveCamera(45, 1, 0.01, 1000)
    const controls = new OrbitControls(camera, renderer.domElement)
    const render = () => renderer.render(scene, camera)
    controls.addEventListener('change', render)
    let radius = 1
    const center = new Vector3()
    const fit = () => {
      controls.target.copy(center)
      const halfAngle = Math.atan(Math.tan(Math.PI / 8) * Math.min(1, camera.aspect))
      const distance = radius / Math.sin(halfAngle) * 1.15
      camera.position.copy(controls.target).add(new Vector3(1, 0.6, 1).normalize().multiplyScalar(distance))
      camera.near = radius / 1000
      camera.far = radius * 1000
      camera.updateProjectionMatrix()
      controls.minDistance = radius * 0.05
      controls.maxDistance = radius * 100
      controls.update()
      render()
    }
    resetRef.current = fit
    const resize = () => {
      const { width, height } = stage.getBoundingClientRect()
      if (!width || !height) return
      renderer.setSize(width, height)
      camera.aspect = width / height
      camera.updateProjectionMatrix()
      if (model) fit()
      else render()
    }
    const observer = new ResizeObserver(resize)
    observer.observe(stage)
    resize()
    const manager = new LoadingManager()
    // FBX can return its geometry before textures finish, or return no
    // texture requests at all. Reconcile on both model assignment and manager
    // completion; never mistake an in-flight texture for a missing one.
    let resourcesLoaded = true
    const reconcileMaterials = () => {
      if (disposed || !model || !resourcesLoaded) return
      const hasTextures = modelHasTextures(model)
      const retired = restoreMissingModelMaterials(model, /\.fbx$/i.test(name))
      for (const material of retired) retiredMaterials.add(material)
      if (retired.size) setTextureNote(hasTextures
        ? 'Some textures could not be loaded. Showing affected surfaces with a neutral material.'
        : 'This export has no textures. Showing affected surfaces with a neutral material.')
      render()
    }
    manager.onStart = () => { resourcesLoaded = false }
    manager.onLoad = () => {
      // A texture may finish after unmount; release its newly arrived image too.
      if (disposed) { if (model) disposeModel(model, retiredMaterials); return }
      resourcesLoaded = true
      reconcileMaterials()
    }
    manager.setURLModifier(resource => modelResourceUrl(src, resource))
    void fetch(src, { signal: controller.signal })
      .then(async response => {
        const data = await readModelData(response)
        if (disposed) return
        if (/\.fbx$/i.test(name)) {
          requireFbxMesh(data)
          return new FBXLoader(manager).parse(data, '')
        }
        return (await new GLTFLoader(manager).setMeshoptDecoder(MeshoptDecoder).parseAsync(data, '')).scene
      })
      .then(object => {
        if (!object) return
        model = object
        if (disposed) { disposeModel(model, retiredMaterials); return }
        model.updateMatrixWorld(true)
        const bounds = new Box3().setFromObject(model).getBoundingSphere(new Sphere())
        if (bounds.isEmpty()) throw new ModelPreviewError('No mesh in this file')
        radius = Math.max(bounds.radius, 0.001)
        center.copy(bounds.center)
        scene.add(model)
        reconcileMaterials()
        fit()
        setStatus('')
      })
      .catch(reason => {
        if (!disposed) setError(reason instanceof ModelPreviewError ? reason.message
          : reason instanceof RangeError ? 'This file is too large or complex to preview. Download it to open in a 3D application.'
            : 'This model could not be previewed. Download it to open in a 3D application.')
      })
    return () => {
      disposed = true
      controller.abort()
      observer.disconnect()
      controls.dispose()
      if (model) disposeModel(model, retiredMaterials)
      renderer.dispose()
      renderer.forceContextLoss()
      renderer.domElement.remove()
    }
  }, [name, src])

  return (
    <div className="model-viewer">
      <div className="model-viewer-stage" ref={stageRef} />
      {textureNote && !error ? <p className="model-viewer-texture-note">{textureNote}</p> : null}
      {error ? <p role="alert">{error} <a href={src} download>Download</a></p>
        : status ? <p role="status">{status}</p>
          : <div className="model-viewer-controls"><button type="button" onClick={() => resetRef.current()}>Reset view</button></div>}
    </div>
  )
}
