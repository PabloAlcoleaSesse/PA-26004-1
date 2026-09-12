package transfer

import "testing"

func TestSelectCandidateRequiresExactProviderAndID(t *testing.T) {
	candidates := []Track{
		{Provider: ProviderAppleMusic, ID: "a1", Name: "Song"},
		{Provider: ProviderAppleMusic, ID: "a2", Name: "Song"},
	}
	selected, ok := selectCandidate(candidates, ProviderAppleMusic, "a2")
	if !ok || selected.ID != "a2" {
		t.Fatalf("selected=%+v ok=%v", selected, ok)
	}
	if _, ok := selectCandidate(candidates, ProviderSpotify, "a2"); ok {
		t.Fatal("accepted candidate from another provider")
	}
	if _, ok := selectCandidate(candidates, ProviderAppleMusic, "missing"); ok {
		t.Fatal("accepted candidate not present in preview")
	}
}
