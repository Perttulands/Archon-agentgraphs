#!/usr/bin/env bash
set -euo pipefail

source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
prefix="${HOME}/.local"
while (($#)); do
  case "$1" in
    --prefix) prefix=${2:?--prefix requires an absolute directory}; shift 2 ;;
    -h|--help) echo 'Usage: ./install.sh [--prefix /absolute/directory]'; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ $prefix == /* ]] || { echo '--prefix must be absolute' >&2; exit 2; }
[[ $(uname -s) == Linux ]] || { echo 'Archon releases currently support Linux.' >&2; exit 1; }
case $(uname -m) in
  x86_64) platform=linux-amd64 ;;
  aarch64|arm64) platform=linux-arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
for file in VERSION COMMIT PLATFORM MANIFEST.sha256; do
  [[ -f $source_dir/$file ]] || { echo 'Run install.sh from an extracted Archon release.' >&2; exit 1; }
done
[[ $(cat "$source_dir/PLATFORM") == "$platform" ]] || { echo "This archive does not match $platform." >&2; exit 1; }
(cd "$source_dir" && sha256sum --check --status MANIFEST.sha256) || { echo 'Release checksum verification failed.' >&2; exit 1; }
version=$(cat "$source_dir/VERSION")
commit=$(cat "$source_dir/COMMIT")
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.-]+)?$ && $commit =~ ^[0-9a-f]{40}(-dirty)?$ ]] || { echo 'Invalid release identity.' >&2; exit 1; }

mkdir -p -- "$prefix"
prefix=$(cd -- "$prefix" && pwd)
base="$prefix/lib/archon"
release_id="$version-$commit-$platform"
destination="$base/releases/$release_id"
# Refuse to replace an unrelated binary or directory, including a foreign link.
for command in archon archond formationsd; do
  target="$prefix/bin/$command"
  expected="../lib/archon/current/bin/$command"
  if [[ -e $target || -L $target ]]; then
    [[ -L $target && $(readlink "$target") == "$expected" ]] || { echo "Refusing to replace unmanaged $target" >&2; exit 1; }
  fi
done
if [[ -e $base/current || -L $base/current ]]; then
  managed=false
  if [[ -L $base/current ]]; then
    current_target=$(readlink "$base/current")
    current_dir="$base/$current_target"
    if [[ $current_target == releases/* && ! -L $current_dir && -f $current_dir/VERSION && -f $current_dir/COMMIT && -f $current_dir/PLATFORM && -f $current_dir/MANIFEST.sha256 ]]; then
      current_version=$(cat "$current_dir/VERSION")
      current_commit=$(cat "$current_dir/COMMIT")
      current_platform=$(cat "$current_dir/PLATFORM")
      if [[ $current_version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.-]+)?$ && $current_commit =~ ^[0-9a-f]{40}(-dirty)?$ && $current_platform =~ ^linux-(amd64|arm64)$ && $current_target == "releases/$current_version-$current_commit-$current_platform" ]]; then
        (cd "$current_dir" && sha256sum --check --status MANIFEST.sha256) && managed=true
      fi
    fi
  fi
  $managed || { echo "Refusing to replace unmanaged or modified $base/current" >&2; exit 1; }
fi
mkdir -p "$base/releases" "$prefix/bin"
stage=''
link=''
cleanup() {
  [[ -z $stage ]] || rm -rf -- "$stage"
  [[ -z $link ]] || rm -f -- "$link"
}
trap cleanup EXIT
if [[ -e $destination ]]; then
  cmp -s "$source_dir/MANIFEST.sha256" "$destination/MANIFEST.sha256" || { echo "Different content already installed at $destination" >&2; exit 1; }
  (cd "$destination" && sha256sum --check --status MANIFEST.sha256) || { echo "Installed release has changed: $destination" >&2; exit 1; }
else
  stage=$(mktemp -d "$base/releases/.install.XXXXXXXX")
  cp -R "$source_dir/." "$stage/"
  (cd "$stage" && sha256sum --check --status MANIFEST.sha256)
  mv -- "$stage" "$destination"
  stage=''
fi
link="$base/.current.$$"
ln -s "releases/$release_id" "$link"
mv -Tf -- "$link" "$base/current"
link=''
for command in archon archond formationsd; do
  [[ -L $prefix/bin/$command ]] || ln -s "../lib/archon/current/bin/$command" "$prefix/bin/$command"
done
printf 'Installed Archon %s to %s\nAdd %s/bin to PATH.\n' "$version" "$prefix" "$prefix"
printf 'Try: archond --executor lab --state-dir "$HOME/.local/share/archon/state" --listen 127.0.0.1:8091\n'
