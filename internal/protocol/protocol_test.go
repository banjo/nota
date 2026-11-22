package protocol

import (
	"errors"
	"testing"
)

func TestCleanPath(t *testing.T) {
	nfd := "Mo\u0308te.md" // "Möte.md" decomposed, as macOS stores it
	got, err := CleanPath(nfd)
	if err != nil || got != "M\u00f6te.md" {
		t.Fatalf("CleanPath(NFD) = %q, %v; want NFC", got, err)
	}
	for _, ok := range []string{"a.md", "Projekt/Plan.md", "attachments/bild 1.png", ".notaignore"} {
		if _, err := CleanPath(ok); err != nil {
			t.Errorf("CleanPath(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/a.md", "a/", "a//b.md", "../a.md", "a/./b.md", `a\b.md`, "a\x00.md", "a\n.md"} {
		if _, err := CleanPath(bad); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanPath(%q) = %v, want ErrInvalidPath", bad, err)
		}
	}
}

func TestFoldKey(t *testing.T) {
	if FoldKey("Mo\u0308te.MD") != FoldKey("m\u00f6te.md") {
		t.Fatal("NFD and NFC, upper and lower case should fold together")
	}
	if FoldKey("Å.md") == FoldKey("A.md") {
		t.Fatal("Å and A are different letters")
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]int{"1": 1, " 2 ": 2, "1.4": 1} {
		got, err := ParseVersion(in)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "x", "-1", "v1"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) should fail", bad)
		}
	}
}

func TestHash(t *testing.T) {
	h := Hash([]byte("abc"))
	if h != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || !ValidHash(h) {
		t.Fatalf("Hash = %s", h)
	}
	if ValidHash("ABC") || ValidHash(h[:10]) {
		t.Fatal("ValidHash accepts junk")
	}
}

func TestRoutes(t *testing.T) {
	if got := FilePath("main", "Möten/Plan #1.md"); got != "/v1/vaults/main/files/M%C3%B6ten/Plan%20%231.md" {
		t.Fatalf("FilePath = %s", got)
	}
	if got := HistoryPath("v", "a/b?.md"); got != "/v1/vaults/v/history/a/b%3F.md" {
		t.Fatalf("HistoryPath = %s", got)
	}
}

func TestKindAndVaultName(t *testing.T) {
	if KindOf("a/B.MD") != KindMD || KindOf("img.png") != KindBlob || KindOf(".notaignore") != KindBlob {
		t.Fatal("KindOf")
	}
	for _, ok := range []string{"main", "Work-2", "a.b_c"} {
		if !ValidVaultName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a/b", "möte", ".x"} {
		if ValidVaultName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
