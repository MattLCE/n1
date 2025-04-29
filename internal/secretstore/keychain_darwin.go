//go:build darwin

package secretstore

import "github.com/zalando/go-keyring"

func init() { Default = keyringStore("n1") }

type keyringStore string

func (k keyringStore) Put(n string, d []byte) error { return keyring.Set(string(k), n, string(d)) }
func (k keyringStore) Get(n string) ([]byte, error) {
	s, e := keyring.Get(string(k), n)
	return []byte(s), e
}
func (k keyringStore) Delete(n string) error { return keyring.Delete(string(k), n) }
