package components

import (
	"testing"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
)

func TestParseUnifiedDiff(t *testing.T) {
	diffStr := `@@ -1,3 +1,4 @@
-a
-b
-c
+d
+e
+f
+g`

	t.Run("SplitView", func(t *testing.T) {
		rows := parseUnifiedDiff(diffStr, true)

		// Header is index 0
		if len(rows) != 5 {
			t.Fatalf("expected 5 rows (1 header + 4 aligned diff rows), got %d", len(rows))
		}

		if !rows[0].IsHeader || rows[0].HeaderText != "@@ -1,3 +1,4 @@" {
			t.Errorf("expected header row, got %+v", rows[0])
		}

		// Row 1: Left=a, Right=d
		if rows[1].Left == nil || rows[1].Left.Content != "a" || rows[1].Left.Op != diff.OpDelete {
			t.Errorf("row 1 left expected delete 'a', got %+v", rows[1].Left)
		}
		if rows[1].Right == nil || rows[1].Right.Content != "d" || rows[1].Right.Op != diff.OpInsert {
			t.Errorf("row 1 right expected insert 'd', got %+v", rows[1].Right)
		}

		// Row 4: Left=nil, Right=g
		if rows[4].Left != nil {
			t.Errorf("row 4 left expected nil, got %+v", rows[4].Left)
		}
		if rows[4].Right == nil || rows[4].Right.Content != "g" || rows[4].Right.Op != diff.OpInsert {
			t.Errorf("row 4 right expected insert 'g', got %+v", rows[4].Right)
		}
	})

	t.Run("UnifiedView", func(t *testing.T) {
		rows := parseUnifiedDiff(diffStr, false)

		// Header is index 0, followed by 3 deletes and 4 inserts
		if len(rows) != 8 {
			t.Fatalf("expected 8 rows (1 header + 3 deletes + 4 inserts), got %d", len(rows))
		}

		if !rows[0].IsHeader || rows[0].HeaderText != "@@ -1,3 +1,4 @@" {
			t.Errorf("expected header row, got %+v", rows[0])
		}

		// Deletes
		for i := 1; i <= 3; i++ {
			r := rows[i]
			if r.Left == nil || r.Left.Op != diff.OpDelete {
				t.Errorf("row %d expected delete, got %+v", i, r.Left)
			}
			if r.Right != nil {
				t.Errorf("row %d expected nil right, got %+v", i, r.Right)
			}
		}

		// Inserts
		for i := 4; i <= 7; i++ {
			r := rows[i]
			if r.Left != nil {
				t.Errorf("row %d expected nil left, got %+v", i, r.Left)
			}
			if r.Right == nil || r.Right.Op != diff.OpInsert {
				t.Errorf("row %d expected insert, got %+v", i, r.Right)
			}
		}
	})
}
