package secretstore

import "testing"

func TestRoundTrip(t *testing.T) {
	s := testStore{}
	const name = "vault.db"
	const data = "hunter2"

	if err := s.Put(name, []byte(data)); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, _ := s.Get(name)
	if string(got) != data {
		t.Fatalf("want %q got %q", data, got)
	}
	if err := s.Delete(name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(name); err == nil {
		t.Fatalf("expected miss after delete")
	}
}
