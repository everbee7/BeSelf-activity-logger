package win

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func SetStartup(appName string, enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue(appName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe := ExecutablePath()
	if exe == "" {
		return fmt.Errorf("could not determine executable path")
	}
	return key.SetStringValue(appName, `"`+exe+`"`)
}

func StartupEnabled(appName string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(appName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	return err == nil, err
}
