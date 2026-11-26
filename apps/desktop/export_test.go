package main

import "testing"

func TestLetterRegion(t *testing.T) {
	for locale, want := range map[string]bool{
		"en_US": true, "es-MX": true, "en_CA@calendar=gregorian": true,
		"sv_SE": false, "en_GB": false, "de": false, "": false,
	} {
		if got := letterRegion(locale); got != want {
			t.Errorf("letterRegion(%q) = %v", locale, got)
		}
	}
}
