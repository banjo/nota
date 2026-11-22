package sync

import "testing"

func TestMerge3(t *testing.T) {
	base := "# Plan\n\none\ntwo\nthree\nfour\nfive\n"
	for _, tc := range []struct {
		name, local, remote, want string
		ok                        bool
	}{
		{"identical", base, base, base, true},
		{"only local", base + "six\n", base, base + "six\n", true},
		{"only remote", base, "# Plan\n\nONE\ntwo\nthree\nfour\nfive\n", "# Plan\n\nONE\ntwo\nthree\nfour\nfive\n", true},
		{"different lines",
			"# Plan\n\nONE\ntwo\nthree\nfour\nfive\n",
			"# Plan\n\none\ntwo\nthree\nfour\nFIVE\n",
			"# Plan\n\nONE\ntwo\nthree\nfour\nFIVE\n", true},
		{"adjacent lines",
			"# Plan\n\none\nTWO\nthree\nfour\nfive\n",
			"# Plan\n\none\ntwo\nTHREE\nfour\nfive\n",
			"# Plan\n\none\nTWO\nTHREE\nfour\nfive\n", true},
		{"both append differently", base + "local\n", base + "remote\n", "", false},
		{"same line differently",
			"# Plan\n\none\ntwo (local)\nthree\nfour\nfive\n",
			"# Plan\n\none\ntwo (remote)\nthree\nfour\nfive\n", "", false},
		{"same change on both sides",
			"# Plan\n\none\n2\nthree\nfour\nfive\nsix\n",
			"# Plan\n\none\n2\nthree\nfour\nfive\n",
			"# Plan\n\none\n2\nthree\nfour\nfive\nsix\n", true},
		{"delete vs edit elsewhere",
			"# Plan\n\ntwo\nthree\nfour\nfive\n",
			"# Plan\n\none\ntwo\nthree\nfour\nfive!\n",
			"# Plan\n\ntwo\nthree\nfour\nfive!\n", true},
		{"delete vs edit same line",
			"# Plan\n\ntwo\nthree\nfour\nfive\n",
			"# Plan\n\nONE\ntwo\nthree\nfour\nfive\n", "", false},
		{"no trailing newline",
			"# Plan\n\none\ntwo\nthree\nfour\nfive",
			"# Plan!\n\none\ntwo\nthree\nfour\nfive\n",
			"# Plan!\n\none\ntwo\nthree\nfour\nfive", true}, // the lost newline is a change to the last line
		{"newline vs edit of the last line",
			"# Plan\n\none\ntwo\nthree\nfour\nfive",
			"# Plan\n\none\ntwo\nthree\nfour\nFIVE\n",
			"", false},
		{"insert at different places",
			"# Plan\nA\n\none\ntwo\nthree\nfour\nfive\n",
			"# Plan\n\none\ntwo\nthree\nB\nfour\nfive\n",
			"# Plan\nA\n\none\ntwo\nthree\nB\nfour\nfive\n", true},
	} {
		got, ok := Merge3(base, tc.local, tc.remote)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: Merge3 = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	// With an empty base (two devices created the same note) only equal
	// content merges.
	if _, ok := Merge3("", "a\n", "b\n"); ok {
		t.Error("two different creates merged")
	}
}
