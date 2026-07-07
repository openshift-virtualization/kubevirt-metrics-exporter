package csi

import (
	"fmt"
	"io"
	"os"
)

const maxJSONFileSize = 1 << 20 // 1 MiB

func readFileLimited(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %s exceeds size limit of %d bytes", path, maxBytes)
	}
	return data, nil
}
