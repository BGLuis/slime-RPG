package desktop

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/egoist/mygo"
)

//go:embed dist/*
var distFS embed.FS

// Run inicializa e exibe a janela desktop nativa do Slime RPG com MyGo.
func Run(initialPath string) error {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("falha ao carregar assets do frontend desktop: %w", err)
	}
	mygo.SetFrontend(sub)

	svc := NewSlimeService(initialPath)
	mygo.BindAs("Slime", svc)

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "Slime RPG",
			Width:     1080,
			Height:    720,
			MinWidth:  920,
			MinHeight: 620,
			URL:       "/",
		})
	})

	return mygo.App.Run()
}
