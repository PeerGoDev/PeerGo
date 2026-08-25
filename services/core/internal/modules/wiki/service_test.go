package wiki

import "testing"

func TestNormalizeCreateInputRejectsInvalidEditorIDs(t *testing.T) {
	t.Parallel()

	for _, editorIDs := range [][]int64{{0}, {-1}, make([]int64, MaximumEditors+1)} {
		_, err := normalizeCreateInput(CreateManagedInput{
			Slug:             "rules",
			Title:            "Rules",
			Body:             "# Rules",
			Visibility:       VisibilityMembers,
			EditorNumericIDs: editorIDs,
		})
		if err != ErrInput {
			t.Fatalf("normalizeCreateInput(%v) error = %v, want ErrInput", editorIDs, err)
		}
	}
}

func TestNormalizeCreateInputCompactsEditorsAndGeneratesReason(t *testing.T) {
	t.Parallel()

	input, err := normalizeCreateInput(CreateManagedInput{
		Slug:             "  USER-GUIDE ",
		Title:            " User guide ",
		Body:             "# Start\r\n\r\nWelcome",
		Visibility:       VisibilityMembers,
		EditorNumericIDs: []int64{163, 1, 163},
	})
	if err != nil {
		t.Fatalf("normalizeCreateInput() error = %v", err)
	}
	if input.Slug != "user-guide" || input.Title != "User guide" {
		t.Fatalf("normalized identity = %q, %q", input.Slug, input.Title)
	}
	if len(input.EditorNumericIDs) != 2 || input.EditorNumericIDs[0] != 1 || input.EditorNumericIDs[1] != 163 {
		t.Fatalf("normalized editors = %v", input.EditorNumericIDs)
	}
	if input.Reason == "" {
		t.Fatal("empty reason was not generated")
	}
}
