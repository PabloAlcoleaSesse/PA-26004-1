package identity

import "testing"

func TestFingerprintIsStableAndDoesNotExposeToken(t *testing.T) {
	first := Fingerprint("music-user-token")
	if first != Fingerprint("music-user-token") {
		t.Fatal("fingerprint must be deterministic")
	}
	if first == "music-user-token" || len(first) != 64 {
		t.Fatalf("unexpected token fingerprint %q", first)
	}
	if first == Fingerprint("another-token") {
		t.Fatal("different tokens must have different fingerprints")
	}
}
