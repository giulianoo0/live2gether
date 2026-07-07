package restream

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func SafeHLSName(name string) (string, error) {
	if name == "" {
		return "", errors.New("file name is required")
	}
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		return "", errors.New("invalid HLS file name")
	}

	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u8", ".ts", ".m4s", ".mp4", ".vtt":
		return name, nil
	default:
		return "", errors.New("unsupported HLS file extension")
	}
}

func prepareHLSDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}
