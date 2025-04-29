package secretstore

type Store interface {
	Put(name string, data []byte) error
	Get(name string) ([]byte, error)
	Delete(name string) error
}
var Default Store // set in init of each platform file
