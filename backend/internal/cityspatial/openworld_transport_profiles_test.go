package cityspatial

import "testing"

func TestOpenWorldTransportStyleProfilesAreDeterministicAndDistinct(t *testing.T) {
	profileIDs := []string{DefaultWorldgenProfileID, WorldgenProfileJapanMetropolitan, WorldgenProfileChinaMetropolitan}
	hashes := make(map[string]struct{}, len(profileIDs))
	for _, profileID := range profileIDs {
		first, err := OpenWorldTransportStyleProfileForWorldgenProfile(profileID)
		if err != nil {
			t.Fatalf("resolve %s transport style: %v", profileID, err)
		}
		second, err := OpenWorldTransportStyleProfileForWorldgenProfile(profileID)
		if err != nil {
			t.Fatalf("resolve %s transport style again: %v", profileID, err)
		}
		if first.ContentHash != second.ContentHash || first.ContentHash == "" {
			t.Fatalf("%s transport style hash is not deterministic", profileID)
		}
		if _, found := hashes[first.ContentHash]; found {
			t.Fatalf("%s transport style hash is unexpectedly shared", profileID)
		}
		hashes[first.ContentHash] = struct{}{}
		if _, ok := OpenWorldTransportStyleNodeClass(*first, "interchange"); !ok {
			t.Fatalf("%s lacks interchange node class", profileID)
		}
		if _, ok := OpenWorldTransportStyleCorridorClass(*first, "freight", "trunk"); !ok {
			t.Fatalf("%s lacks freight trunk class", profileID)
		}
	}
}

func TestOpenWorldTransportStyleProfilesRejectUnknownWorldgenProfile(t *testing.T) {
	if _, err := OpenWorldTransportStyleProfileForWorldgenProfile("unknown.profile"); err == nil {
		t.Fatal("expected unknown transport style to fail")
	}
}
