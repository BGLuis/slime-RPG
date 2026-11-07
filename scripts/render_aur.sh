#!/usr/bin/env bash
# Gera PKGBUILD e .SRCINFO determinísticos para o pacote AUR slime-rpg-bin.
# Uso: scripts/render_aur.sh <tag> <sha256_amd64> <sha256_arm64> [out_dir]
set -euo pipefail

if [[ $# -lt 3 ]]; then
    echo "Uso: $0 <tag> <sha256_amd64> <sha256_arm64> [out_dir]" >&2
    exit 2
fi

tag="$1"
sha_amd64="$2"
sha_arm64="$3"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template="${repo_root}/packaging/aur/slime-rpg-bin/PKGBUILD.in"
out_dir="${4:-${repo_root}/packaging/aur/slime-rpg-bin}"

ver="${tag#v}"
pkgver="$(printf '%s' "$ver" | sed 's/[^A-Za-z0-9._]/_/g')"

mkdir -p "$out_dir"

sed \
    -e "s|@TAG@|${tag}|g" \
    -e "s|@VERSION@|${ver}|g" \
    -e "s|@PKGVER@|${pkgver}|g" \
    -e "s|@SHA256_AMD64@|${sha_amd64}|g" \
    -e "s|@SHA256_ARM64@|${sha_arm64}|g" \
    "$template" > "${out_dir}/PKGBUILD"

cat > "${out_dir}/.SRCINFO" <<EOF
pkgbase = slime-rpg-bin
	pkgdesc = Modular game localization and translation tool (CLI, TUI and Desktop GUI) for RPG Maker, WOLF RPG, CSV and JSON
	pkgver = ${pkgver}
	pkgrel = 1
	url = https://github.com/BGLuis/slime-RPG
	arch = x86_64
	arch = aarch64
	license = MIT
	depends = gtk3
	depends = webkit2gtk-4.1
	provides = slime-rpg
	conflicts = slime-rpg
	noextract = slime-${pkgver}-x86_64
	noextract = slime-${pkgver}-aarch64
	options = !strip
	options = !debug
	source_x86_64 = slime-${pkgver}-x86_64::https://github.com/BGLuis/slime-RPG/releases/download/${tag}/slime-${ver}-linux-amd64
	sha256sums_x86_64 = ${sha_amd64}
	source_aarch64 = slime-${pkgver}-aarch64::https://github.com/BGLuis/slime-RPG/releases/download/${tag}/slime-${ver}-linux-arm64
	sha256sums_aarch64 = ${sha_arm64}

pkgname = slime-rpg-bin
EOF

echo "✓ PKGBUILD e .SRCINFO gerados em ${out_dir} (pkgver=${pkgver})"
