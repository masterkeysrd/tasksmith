package hash

import "testing"

func TestSHA256AndValidation(t *testing.T) {
	got := SHA256Hex([]byte("abc"))
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("SHA256Hex(\"abc\") = %q, want %q", got, want)
	}
	if gotStr := SHA256String("abc"); gotStr != want {
		t.Fatalf("SHA256String(\"abc\") = %q, want %q", gotStr, want)
	}
	if !IsValidSHA256(got) {
		t.Fatalf("IsValidSHA256(%q) = false, want true", got)
	}
	for _, bad := range []string{
		"",
		"abc",
		"BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD",
		"za7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"../ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015",
	} {
		if IsValidSHA256(bad) {
			t.Errorf("IsValidSHA256(%q) = true, want false", bad)
		}
	}
}
