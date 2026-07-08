package app

import (
	"os"
	"path/filepath"
)

const Name = "BeSelf"

func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, Name), nil
}
