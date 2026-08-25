package supervisor

import "testing"

func TestComponentsHaveUniqueNamesAndExecutables(t *testing.T) {
	t.Parallel()
	for mode, expected := range map[Mode]int{ModeAPI: 3, ModeWorker: 22} {
		components, err := Components(mode)
		if err != nil {
			t.Fatalf("components for %s: %v", mode, err)
		}
		if len(components) != expected {
			t.Fatalf("components for %s = %d, want %d", mode, len(components), expected)
		}
		seen := make(map[string]struct{}, len(components))
		for _, component := range components {
			if component.Name == "" || component.Executable == "" {
				t.Fatalf("incomplete component for %s: %+v", mode, component)
			}
			if _, exists := seen[component.Name]; exists {
				t.Fatalf("duplicate component %q for %s", component.Name, mode)
			}
			seen[component.Name] = struct{}{}
		}
	}
}

func TestParseModeRejectsUnknownMode(t *testing.T) {
	t.Parallel()
	if _, err := ParseMode("everything"); err == nil {
		t.Fatal("ParseMode accepted an unknown mode")
	}
}
