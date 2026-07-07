package cachepaths

import "testing"

func TestNamespaceIncludesEndpointPathWhenUnverified(t *testing.T) {
	// Given
	account := "nemo"

	// When
	first := Namespace(account, "https://music.example/library-a", "")
	second := Namespace(account, "https://music.example/library-b", "")

	// Then
	if first == second {
		t.Fatalf("unverified namespaces should differ for different endpoint paths, both were %s", first)
	}
}

func TestNamespaceIncludesAccountWhenVerified(t *testing.T) {
	// Given
	fingerprint := "same-library"

	// When
	first := Namespace("alice", "https://music.example", fingerprint)
	second := Namespace("bob", "https://music.example", fingerprint)

	// Then
	if first == second {
		t.Fatalf("verified namespaces should differ across accounts, both were %s", first)
	}
}
