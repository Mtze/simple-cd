package diff

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	if Unified("a", "b", "x\n", "x\n") != "" {
		t.Fatal("equal inputs produced a diff")
	}
	old := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"
	new := "1\n2\n3\n4\nfive\n6\n7\n8\n9\n10\neleven\n"
	got := Unified("old", "new", old, new)
	want := `--- old
+++ new
@@ -2,9 +2,10 @@
 2
 3
 4
-5
+five
 6
 7
 8
 9
 10
+eleven
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestUnifiedSeparateHunks(t *testing.T) {
	var a, b []string
	for i := 0; i < 30; i++ {
		a = append(a, "line")
		b = append(b, "line")
	}
	b[2], b[25] = "changed", "changed"
	got := Unified("o", "n", strings.Join(a, "\n"), strings.Join(b, "\n"))
	if strings.Count(got, "@@ -") != 2 {
		t.Fatalf("expected two hunks:\n%s", got)
	}
}

func TestUnifiedFromEmpty(t *testing.T) {
	got := Unified("o", "n", "", "a\nb\n")
	if !strings.Contains(got, "+a\n+b\n") {
		t.Fatal(got)
	}
}
