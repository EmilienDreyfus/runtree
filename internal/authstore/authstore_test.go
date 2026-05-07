package authstore

import "testing"

func TestSaveLoadAndClearAuth(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	input := Auth{
		BaseURL:       "https://runtree.test",
		AccessToken:   "secret-token",
		AccountHandle: "emilien",
	}

	if err := Save(home, input); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded != input {
		t.Fatalf("Load() = %+v, want %+v", loaded, input)
	}

	if err := Clear(home); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	loaded, err = Load(home)
	if err != nil {
		t.Fatalf("Load(after Clear) error = %v", err)
	}
	if loaded != (Auth{}) {
		t.Fatalf("Load(after Clear) = %+v, want zero auth", loaded)
	}
}

func TestValidateRejectsPartialAuth(t *testing.T) {
	t.Parallel()

	err := Auth{BaseURL: "https://runtree.dev"}.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing access token error")
	}
}
