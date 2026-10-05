package shortener

import "testing"

func TestGenerator(t *testing.T) {
	t.Parallel()

	code, err := (Generator{}).Generate(12)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(code) != 12 {
		t.Fatalf("Generate() length = %d, want 12", len(code))
	}

	for _, character := range code {
		found := false
		for _, allowed := range alphabet {
			if character == allowed {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Generate() returned unsupported character %q", character)
		}
	}
}

func TestGeneratorRejectsInvalidLength(t *testing.T) {
	t.Parallel()

	if _, err := (Generator{}).Generate(0); err == nil {
		t.Fatal("Generate() error = nil, want an error")
	}
}
