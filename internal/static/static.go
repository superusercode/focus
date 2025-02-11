package static

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/pterm/pterm"
)

//go:embed files/*
var Files embed.FS

const (
	dir       = "files"
	configDir = "focus"
)

// FilePath returns the path to the specified file.
func FilePath(fileName string) string {
	return filepath.Join(dir, fileName)
}

func init() {
	_ = fs.WalkDir(
		Files,
		dir,
		func(path string, d fs.DirEntry, err error) error {
			if d.Name() == "icon.png" {
				var b []byte

				b, err = fs.ReadFile(Files, path)
				if err != nil {
					pterm.Error.Println(err)
					os.Exit(1)
				}

				relPath := filepath.Join(configDir, path)

				var pathToFile string

				pathToFile, err = xdg.DataFile(relPath)
				if err != nil {
					pterm.Error.Println(err)
					os.Exit(1)
				}

				if _, err = xdg.SearchDataFile(relPath); err != nil {
					err = os.WriteFile(pathToFile, b, os.ModePerm)
					if err != nil {
						pterm.Error.Println(err)
						os.Exit(1)
					}
				}
			}

			return err
		},
	)
}
