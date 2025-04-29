//go:build linux

//Important: a go:build line must be the first non-comment thing in the file and have a newline before the package keyword.

package secretstore

import (
	"os"
	"os/user"
	"path/filepath"
)

func init() { Default = fileStore{} }

type fileStore struct{}

func (fileStore) path(name string) string {
	u, _ := user.Current()
	return filepath.Join(u.HomeDir, ".n1-secrets", name)
}

func (f fileStore) Put(n string, d []byte) error {
	path := f.path(n)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, d, 0600)
}

func (f fileStore) Get(n string) ([]byte, error) { return os.ReadFile(f.path(n)) }

func (f fileStore) Delete(n string) error { return os.Remove(f.path(n)) }
