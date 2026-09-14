#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
out="$root/dist"
arches=(amd64 arm64)
allow_dirty=false
while (($#)); do
  case "$1" in
    --out) out=${2:?--out requires a directory}; shift 2 ;;
    --arch) arches=("${2:?--arch requires amd64 or arm64}"); shift 2 ;;
    --allow-dirty) allow_dirty=true; shift ;;
    -h|--help) echo 'Usage: scripts/build-release.sh [--out DIR] [--arch amd64|arm64] [--allow-dirty]'; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
for arch in "${arches[@]}"; do
  [[ $arch == amd64 || $arch == arm64 ]] || { echo "Unsupported Linux architecture: $arch" >&2; exit 2; }
done
version=$(tr -d '\n' < "$root/VERSION")
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.-]+)?$ ]] || { echo 'Invalid VERSION' >&2; exit 1; }
commit=$(git -C "$root" rev-parse HEAD)
changes=$(git -C "$root" status --porcelain --untracked-files=all -- . ':!.beads')
if [[ -n $changes ]]; then
  $allow_dirty || { echo 'Release requires committed source. Use --allow-dirty only for local verification.' >&2; exit 1; }
  commit="$commit-dirty"
fi
epoch=$(git -C "$root" show -s --format=%ct HEAD)
mkdir -p -- "$out"
out=$(cd -- "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT

# Build the UI for every release so an ignored dist directory cannot go stale.
(cd "$root/dashboard" && npm ci && npm run build)
ldflags="-s -w -X github.com/Perttulands/chrote-agent-formations/internal/buildinfo.Version=$version -X github.com/Perttulands/chrote-agent-formations/internal/buildinfo.Commit=$commit"
archives=()
for arch in "${arches[@]}"; do
  name="archon-$version-linux-$arch"
  bundle="$stage/$name"
  mkdir -p "$bundle/bin" "$bundle/share/archon"
  for command in archon archond; do
    (cd "$root/src" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$bundle/bin/$command" "./cmd/$command")
  done
  cp "$bundle/bin/archond" "$bundle/bin/formationsd"
  cp -R "$root/dashboard/dist" "$bundle/share/archon/ui"
  cp -R "$root/examples" "$root/docs" "$bundle/share/archon/"
  cp "$root/README.md" "$root/LICENSE" "$bundle/"
  cp -R "$root/examples" "$root/docs" "$bundle/"
  cp "$root/scripts/install.sh" "$bundle/install.sh"
  printf '%s\n' "$version" > "$bundle/VERSION"
  printf '%s\n' "$commit" > "$bundle/COMMIT"
  printf 'linux-%s\n' "$arch" > "$bundle/PLATFORM"
  (cd "$bundle" && find . -type f ! -name MANIFEST.sha256 -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > MANIFEST.sha256)
  tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner -C "$stage" -cf - "$name" | gzip -n > "$out/$name.tar.gz"
  archives+=("$name.tar.gz")
done
(cd "$out" && sha256sum "${archives[@]}" > SHA256SUMS)
printf 'Built Archon %s (%s) in %s\n' "$version" "$commit" "$out"
