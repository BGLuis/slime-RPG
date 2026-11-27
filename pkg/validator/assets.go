package validator

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var assetExts = []string{
	".png_", ".png", ".rpgmvp",
	".jpg_", ".jpg", ".jpeg", ".webp",
	".ogg_", ".ogg", ".rpgmvo",
	".m4a_", ".m4a", ".rpgmvm",
	".wav", ".mp3",
	".ttf", ".otf", ".woff", ".woff2",
	".efkefc", ".efk",
}

// AssetIndex indexa arquivos de mídia do jogo (img/, audio/, fonts/, effects/) para
// validar se referências em plugins e eventos existem no disco (case-insensitive).
type AssetIndex struct {
	bySubpath map[string]bool // ex: "img/system/window" ou "system/window"
	byBase    map[string]bool // ex: "window"
}

// NewAssetIndex varre os diretórios de recursos dentro de webRoot.
func NewAssetIndex(webRoot string) *AssetIndex {
	idx := &AssetIndex{
		bySubpath: make(map[string]bool),
		byBase:    make(map[string]bool),
	}
	for _, rootFolder := range []string{"img", "audio", "fonts", "effects"} {
		dir := filepath.Join(webRoot, rootFolder)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(webRoot, path)
			if err != nil {
				return nil
			}
			norm := strings.ToLower(filepath.ToSlash(rel))
			idx.addEntry(norm)
			if trimmed, ok := strings.CutPrefix(norm, rootFolder+"/"); ok {
				idx.addEntry(trimmed)
			}
			return nil
		})
	}
	return idx
}

func (a *AssetIndex) addEntry(norm string) {
	a.bySubpath[norm] = true
	stem := stripAssetExt(norm)
	a.bySubpath[stem] = true
	// Indexa também relativo à subpasta de categoria (ex.: "img/pictures/subChara/guild01" -> "subchara/guild01")
	parts := strings.SplitN(stem, "/", 3)
	if len(parts) == 3 {
		a.bySubpath[parts[2]] = true
	}
	if base := pathBase(stem); base != "" {
		a.byBase[base] = true
	}
	if baseFull := pathBase(norm); baseFull != "" {
		a.byBase[baseFull] = true
	}
}

func pathBase(slashPath string) string {
	if i := strings.LastIndexByte(slashPath, '/'); i >= 0 {
		return slashPath[i+1:]
	}
	return slashPath
}

func stripAssetExt(s string) string {
	lower := strings.ToLower(s)
	for _, ext := range assetExts {
		if strings.HasSuffix(lower, ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}

// Exists verifica se ref corresponde a um arquivo real de asset do jogo.
func (a *AssetIndex) Exists(ref string) bool {
	if a == nil {
		return false
	}
	clean := strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if clean == "" {
		return false
	}
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimPrefix(clean, "/")
	lower := strings.ToLower(clean)
	stem := stripAssetExt(lower)

	if a.bySubpath[lower] || a.bySubpath[stem] {
		return true
	}
	if !strings.Contains(stem, "/") {
		return a.byBase[stem] || a.byBase[lower]
	}
	return false
}

// IsEmpty indica se nenhum asset foi indexado (ex.: jogo não tem pastas img/audio).
func (a *AssetIndex) IsEmpty() bool {
	return a == nil || len(a.byBase) == 0
}

var exactAssetParamKeys = map[string]bool{
	"skin": true, "windowskin": true, "image": true, "imagename": true,
	"picture": true, "picturename": true, "face": true, "facename": true,
	"bgm": true, "basebgm": true, "bgs": true, "me": true, "se": true,
	"filename": true, "parallax": true, "parallaxname": true,
	"battleback": true, "battleback1": true, "battleback2": true,
	"displacementimage": true, "fontface": true, "mainfontfilename": true,
	"numberfontfilename": true, "icon": true, "cursor": true, "background": true,
	"backgroundimage": true, "titleimage": true, "gameoverimage": true,
}

var assetParamSuffixes = []string{
	"skin", "image", "imagename", "picture", "picturename",
	"bgm", "bgs", "parallax", "battleback", "fontfilename",
}

// IsAssetParamKey indica se o nome de parâmetro de plugin normalmente referencia um arquivo de mídia.
func IsAssetParamKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	if exactAssetParamKeys[k] {
		return true
	}
	for _, sfx := range assetParamSuffixes {
		if strings.HasSuffix(k, sfx) {
			return true
		}
	}
	return false
}
