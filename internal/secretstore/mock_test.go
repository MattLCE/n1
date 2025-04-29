package secretstore

import "errors"

var errNotFound = errors.New("secret not found")

type testStore map[string][]byte

func (m testStore) Put(n string, d []byte) error { m[n] = d; return nil }

func (m testStore) Get(n string) ([]byte, error) {
	d, ok := m[n]
	if !ok {
		return nil, errNotFound
	}
	return d, nil
}

func (m testStore) Delete(n string) error { delete(m, n); return nil }
