package storage

import (
	"errors"
	"path/filepath"
	"strings"
)

func firstComponent(filePath string) string {
	return strings.SplitN(filePath, string(filepath.Separator), 2)[0]
}

func ToSafeFilePath(fileInfoComponents ...string) (string, error) {
	safeComps := make([]string, 0, len(fileInfoComponents))
	for _, comp := range fileInfoComponents {
		safeComps = append(safeComps, filepath.Clean(comp))
	}
	safeFilePath := filepath.Join(safeComps...)
	fc := firstComponent(safeFilePath)
	switch fc {
	case "..":
		return "", errors.New("escapes root dir")
	default:
		return safeFilePath, nil
	}
}
