import { Color, Mesh, MeshStandardMaterial, Texture } from 'three'
import type { Material, Object3D } from 'three'

// Call only after the loader's resources have settled. A Texture without an
// image can be either an FBX placeholder or a request that has not finished yet.
export function restoreMissingModelMaterials(model: Object3D, fbx: boolean): Set<Material> {
  const replacements = new Map<Material, Material>()
  model.traverse(object => {
    if (!(object instanceof Mesh)) return
    const replace = (material: Material) => {
      const previous = replacements.get(material)
      if (previous) return previous
      const textures = Object.values(material).filter((value): value is Texture => value instanceof Texture)
      const missingMap = textures.some(texture => !texture.image ||
        (typeof HTMLImageElement !== 'undefined' && texture.image instanceof HTMLImageElement && !texture.image.naturalWidth))
      // Unreal FBX exports can omit every texture and export only a black
      // material, as in MI_Body_Baked. There is no resource request to fail.
      const color = 'color' in material && material.color instanceof Color ? material.color : undefined
      const missingFbxAppearance = fbx && textures.length === 0 && color?.getHex() === 0
      if (!missingMap && !missingFbxAppearance) return material
      const replacement = new MeshStandardMaterial({ color: 0xb8bec8, roughness: 0.8, side: material.side })
      replacements.set(material, replacement)
      return replacement
    }
    object.material = Array.isArray(object.material) ? object.material.map(replace) : replace(object.material)
  })
  // Keep retired resources alive until unmount: a valid map can be shared with
  // an unaffected material, and it must not be disposed while still in use.
  return new Set(replacements.keys())
}
