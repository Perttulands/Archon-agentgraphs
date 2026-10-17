import { describe, expect, it } from 'vitest'
import { BoxGeometry, Color, DoubleSide, Group, Mesh, MeshPhongMaterial, MeshStandardMaterial, Texture } from 'three'
import { modelHasTextures, restoreMissingModelMaterials } from './modelMaterials'

function sceneWith(...materials: MeshPhongMaterial[]) {
  const scene = new Group()
  for (const material of materials) scene.add(new Mesh(new BoxGeometry(), material))
  return scene
}

describe('missing model appearance', () => {
  it('restores an untextured black FBX export such as the dryad MI_Body_Baked across shared LODs', () => {
    const material = new MeshPhongMaterial({ name: 'MI_Body_Baked', color: 0, side: DoubleSide })
    const scene = sceneWith(material, material)
    expect(modelHasTextures(scene)).toBe(false)
    const retired = restoreMissingModelMaterials(scene, true)
    const first = scene.children[0] as Mesh
    expect(first.material).toBeInstanceOf(MeshStandardMaterial)
    expect((first.material as MeshStandardMaterial).color).toEqual(new Color(0xb8bec8))
    expect((first.material as MeshStandardMaterial).side).toBe(DoubleSide)
    expect((scene.children[1] as Mesh).material).toBe(first.material)
    expect(retired).toEqual(new Set([material]))
    expect(restoreMissingModelMaterials(scene, true).size).toBe(0)
  })

  it('restores placeholder textures even when no resource request failed', () => {
    const missing = new MeshPhongMaterial({ map: new Texture() })
    const scene = sceneWith(missing)
    expect(modelHasTextures(scene)).toBe(true)
    expect(restoreMissingModelMaterials(scene, true)).toEqual(new Set([missing]))
    expect((scene.children[0] as Mesh).material).toBeInstanceOf(MeshStandardMaterial)
  })

  it('preserves loaded textures and nonblack untextured FBX materials, and leaves glTF black materials alone', () => {
    const textured = new MeshPhongMaterial({ color: 0, map: new Texture({ width: 1, height: 1 }) })
    const plain = new MeshPhongMaterial({ color: 0x204060 })
    const scene = sceneWith(textured, plain)
    expect(modelHasTextures(scene)).toBe(true)
    expect(restoreMissingModelMaterials(scene, true).size).toBe(0)
    expect((scene.children[0] as Mesh).material).toBe(textured)
    expect((scene.children[1] as Mesh).material).toBe(plain)
    expect(restoreMissingModelMaterials(sceneWith(new MeshPhongMaterial({ color: 0 })), false).size).toBe(0)
  })
})
