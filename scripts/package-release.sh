#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

plugin_id="cpa-prometheus"
version="${1:-0.1.0}"
out_dir="${2:-dist}"
mkdir -p "${out_dir}"

goos="${GOOS:-linux}"
goarch="${GOARCH:-amd64}"
ext="so"
case "${goos}" in
  darwin) ext="dylib" ;;
  windows) ext="dll" ;;
esac
lib="${out_dir}/${plugin_id}.${ext}"
if [[ ! -f "${lib}" ]]; then
  echo "missing ${lib}; run make build first" >&2
  exit 1
fi
zip_name="${plugin_id}_${version}_${goos}_${goarch}.zip"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
cp "${lib}" "${tmp}/${plugin_id}.${ext}"
(cd "${tmp}" && zip -9 -q "${OLDPWD}/${out_dir}/${zip_name}" "${plugin_id}.${ext}")

(
  cd "${out_dir}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${zip_name}" > checksums.txt
  else
    shasum -a 256 "${zip_name}" > checksums.txt
  fi
)

echo "Created ${out_dir}/${zip_name}"
echo "Created ${out_dir}/checksums.txt"
