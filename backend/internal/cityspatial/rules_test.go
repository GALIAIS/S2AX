package cityspatial

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultRuleSetIsCompleteDeterministicAndResolvable(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)

	summaries := registry.List()
	require.Len(t, summaries, 1)
	require.Equal(t, DefaultRuleSetID, summaries[0].ID)
	require.Equal(t, "1.0.0", summaries[0].Version)
	require.Equal(t, DefaultChunkSize, summaries[0].ChunkSize)
	require.Equal(t, MinimumZ, summaries[0].MinimumZ)
	require.Equal(t, MaximumZ, summaries[0].MaximumZ)
	require.Equal(t, 44, summaries[0].DefinitionCount)
	require.Regexp(t, `^[0-9a-f]{64}$`, summaries[0].ContentHash)
	require.Equal(t, "136ce6b71a6ebd0f9db4fdfe2662dc7530485330e565e0a7feebcec4399b5277", summaries[0].ContentHash)

	ruleSet, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	require.Equal(t, summaries[0].ContentHash, ruleSet.ContentHash)
	for _, definition := range ruleSet.Definitions {
		visual, resolveErr := ruleSet.ResolveVisual(definition.Kind, definition.ID)
		require.NoError(t, resolveErr, definition.ID)
		require.NotEmpty(t, visual.Glyph, definition.ID)
		require.NotEmpty(t, visual.GlyphSourceID, definition.ID)
		require.NotEmpty(t, visual.FallbackPath, definition.ID)
	}

	door, err := ruleSet.ResolveVisual(RuleKindPortal, "portal.door_open")
	require.NoError(t, err)
	require.Equal(t, "/", door.Glyph)
	require.Equal(t, "portal.door_open", door.GlyphSourceID)
	require.Equal(t, "classic/portal/door_closed", door.Sprite)
	require.Equal(t, "portal.door_closed", door.SpriteSourceID)
	require.Equal(t, []string{"portal.door_open", "portal.door_closed"}, door.FallbackPath)

	missing, err := ruleSet.ResolveVisual(RuleKindItem, "item.does_not_exist")
	require.NoError(t, err)
	require.Equal(t, "missing.item", missing.DefinitionID)
	require.Equal(t, "?", missing.Glyph)
}

func TestRuleSetHashIgnoresInputOrderingButChangesWithSemantics(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	original, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)

	reordered, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	slices.Reverse(reordered.Palette)
	slices.Reverse(reordered.Definitions)
	for index := range reordered.Definitions {
		slices.Reverse(reordered.Definitions[index].Flags)
	}
	reloaded, err := LoadRuleSet(encodeRuleSetForLoad(t, reordered))
	require.NoError(t, err)
	require.Equal(t, original.ContentHash, reloaded.ContentHash)

	changed, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	definitionByID(t, changed, "terrain.ground").MovementCost++
	reloaded, err = LoadRuleSet(encodeRuleSetForLoad(t, changed))
	require.NoError(t, err)
	require.NotEqual(t, original.ContentHash, reloaded.ContentHash)
}

func TestRegistryReturnsCopiesAndRejectsDuplicateSetIDs(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)
	first, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	first.Name = "mutated"
	first.Definitions[0].Name = "mutated"

	second, err := registry.Get(DefaultRuleSetID)
	require.NoError(t, err)
	require.NotEqual(t, "mutated", second.Name)
	require.NotEqual(t, "mutated", second.Definitions[0].Name)

	_, err = registry.Get("missing-rule-set")
	require.ErrorIs(t, err, ErrRuleSetNotFound)
	_, err = NewRegistry(embeddedDefaultRules, embeddedDefaultRules)
	require.ErrorIs(t, err, ErrInvalidRuleSet)
}

func TestRuleSetLoaderRejectsMalformedOrInconsistentRules(t *testing.T) {
	t.Parallel()
	registry, err := DefaultRegistry()
	require.NoError(t, err)

	testCases := []struct {
		name         string
		preserveHash bool
		mutate       func(*RuleSet)
	}{
		{
			name:         "generated hash in source",
			preserveHash: true,
			mutate: func(ruleSet *RuleSet) {
				ruleSet.ContentHash = "not-source-data"
			},
		},
		{
			name: "duplicate definition",
			mutate: func(ruleSet *RuleSet) {
				ruleSet.Definitions = append(ruleSet.Definitions, ruleSet.Definitions[0])
			},
		},
		{
			name: "missing terminal",
			mutate: func(ruleSet *RuleSet) {
				for index, definition := range ruleSet.Definitions {
					if definition.ID == "missing.item" {
						ruleSet.Definitions = append(ruleSet.Definitions[:index], ruleSet.Definitions[index+1:]...)
						return
					}
				}
			},
		},
		{
			name: "cross kind fallback",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").LooksLike = "furniture.table"
			},
		},
		{
			name: "fallback cycle",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").LooksLike = "terrain.soil"
				definitionByID(t, ruleSet, "terrain.soil").LooksLike = "terrain.ground"
			},
		},
		{
			name: "unknown flag",
			mutate: func(ruleSet *RuleSet) {
				definition := definitionByID(t, ruleSet, "terrain.ground")
				definition.Flags = append(definition.Flags, "teleports_by_magic")
			},
		},
		{
			name: "duplicate flag",
			mutate: func(ruleSet *RuleSet) {
				definition := definitionByID(t, ruleSet, "terrain.ground")
				definition.Flags = append(definition.Flags, definition.Flags[0])
			},
		},
		{
			name: "multiple glyphs",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").Glyph = "ab"
			},
		},
		{
			name: "blank display name",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").Name = "   "
			},
		},
		{
			name: "unsafe sprite id",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").Sprite = "https://example.invalid/tile"
			},
		},
		{
			name: "passable without movement cost",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "terrain.ground").MovementCost = 0
			},
		},
		{
			name: "movement cost without passable",
			mutate: func(ruleSet *RuleSet) {
				definitionByID(t, ruleSet, "missing.terrain").MovementCost = 1
			},
		},
		{
			name: "invalid z range",
			mutate: func(ruleSet *RuleSet) {
				ruleSet.MinimumZ = MinimumZ - 1
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ruleSet, getErr := registry.Get(DefaultRuleSetID)
			require.NoError(t, getErr)
			if !testCase.preserveHash {
				ruleSet.ContentHash = ""
			}
			testCase.mutate(ruleSet)
			_, loadErr := LoadRuleSet(encodeRuleSetForLoadPreservingHash(t, ruleSet))
			require.ErrorIs(t, loadErr, ErrInvalidRuleSet)
		})
	}

	duplicateTopLevelKey := bytes.Replace(embeddedDefaultRules, []byte(`"id": "sub2api-classic"`),
		[]byte(`"id": "duplicate", "id": "sub2api-classic"`), 1)
	_, err = LoadRuleSet(duplicateTopLevelKey)
	require.ErrorIs(t, err, ErrInvalidRuleSet)

	unknownTopLevelField := bytes.Replace(embeddedDefaultRules, []byte(`"id": "sub2api-classic"`),
		[]byte(`"unsupported": true, "id": "sub2api-classic"`), 1)
	_, err = LoadRuleSet(unknownTopLevelField)
	require.ErrorIs(t, err, ErrInvalidRuleSet)

	emptyGeneratedHash := bytes.Replace(embeddedDefaultRules, []byte(`"id": "sub2api-classic"`),
		[]byte(`"content_hash": "", "id": "sub2api-classic"`), 1)
	_, err = LoadRuleSet(emptyGeneratedHash)
	require.ErrorIs(t, err, ErrInvalidRuleSet)

	withTrailingJSON := append(append([]byte(nil), embeddedDefaultRules...), []byte(` {}`)...)
	_, err = LoadRuleSet(withTrailingJSON)
	require.ErrorIs(t, err, ErrInvalidRuleSet)
}

func encodeRuleSetForLoad(t *testing.T, ruleSet *RuleSet) []byte {
	t.Helper()
	clone := *ruleSet
	clone.ContentHash = ""
	encoded, err := json.Marshal(clone)
	require.NoError(t, err)
	return encoded
}

func encodeRuleSetForLoadPreservingHash(t *testing.T, ruleSet *RuleSet) []byte {
	t.Helper()
	encoded, err := json.Marshal(ruleSet)
	require.NoError(t, err)
	return encoded
}

func definitionByID(t *testing.T, ruleSet *RuleSet, id string) *Definition {
	t.Helper()
	for index := range ruleSet.Definitions {
		if ruleSet.Definitions[index].ID == id {
			return &ruleSet.Definitions[index]
		}
	}
	require.FailNow(t, "definition not found", id)
	return nil
}
