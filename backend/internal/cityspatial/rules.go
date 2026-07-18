package cityspatial

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultRuleSetID = "sub2api-classic"
	maximumRuleBytes = 1 << 20
	maximumPalette   = 256
	maximumRules     = 4096
	maximumFallbacks = 32
)

type RuleKind string

const (
	RuleKindTerrain   RuleKind = "terrain"
	RuleKindFurniture RuleKind = "furniture"
	RuleKindStructure RuleKind = "structure"
	RuleKindPortal    RuleKind = "portal"
	RuleKindItem      RuleKind = "item"
	RuleKindEntity    RuleKind = "entity"
	RuleKindField     RuleKind = "field"
	RuleKindOverlay   RuleKind = "overlay"
)

var (
	ErrInvalidRuleSet  = errors.New("invalid city spatial rule set")
	ErrRuleSetNotFound = errors.New("city spatial rule set not found")

	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	spritePattern     = regexp.MustCompile(`^[a-z][a-z0-9_.-]*(/[a-z0-9_.-]+)*$`)
	knownKinds        = []RuleKind{
		RuleKindTerrain, RuleKindFurniture, RuleKindStructure, RuleKindPortal,
		RuleKindItem, RuleKindEntity, RuleKindField, RuleKindOverlay,
	}
	knownFlags = map[string]struct{}{
		"passable": {}, "transparent": {}, "supports_roof": {},
		"supports_weight": {}, "liquid": {}, "flammable": {},
		"outdoor": {}, "indoor": {}, "portal_up": {}, "portal_down": {},
		"openable": {}, "closable": {}, "hazard": {}, "blocks_items": {},
	}
)

type PaletteEntry struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ClassicForeground int    `json:"classic_foreground"`
	ClassicBackground *int   `json:"classic_background,omitempty"`
}

type Definition struct {
	ID           string         `json:"id"`
	Kind         RuleKind       `json:"kind"`
	Name         string         `json:"name"`
	Glyph        string         `json:"glyph,omitempty"`
	Foreground   string         `json:"foreground"`
	Background   string         `json:"background,omitempty"`
	LooksLike    string         `json:"looks_like,omitempty"`
	Sprite       string         `json:"sprite,omitempty"`
	MovementCost int            `json:"movement_cost"`
	Flags        []string       `json:"flags"`
	Metadata     map[string]any `json:"metadata"`
}

type RuleSet struct {
	ID          string         `json:"id"`
	Version     string         `json:"version"`
	Name        string         `json:"name"`
	ChunkSize   int64          `json:"chunk_size"`
	MinimumZ    int32          `json:"min_z"`
	MaximumZ    int32          `json:"max_z"`
	Palette     []PaletteEntry `json:"palette"`
	Definitions []Definition   `json:"definitions"`
	ContentHash string         `json:"content_hash,omitempty"`
}

type RuleSetSummary struct {
	ID              string `json:"id"`
	Version         string `json:"version"`
	Name            string `json:"name"`
	ChunkSize       int64  `json:"chunk_size"`
	MinimumZ        int32  `json:"min_z"`
	MaximumZ        int32  `json:"max_z"`
	DefinitionCount int    `json:"definition_count"`
	ContentHash     string `json:"content_hash"`
}

type ResolvedVisual struct {
	DefinitionID   string   `json:"definition_id"`
	Kind           RuleKind `json:"kind"`
	Glyph          string   `json:"glyph"`
	GlyphSourceID  string   `json:"glyph_source_id"`
	Sprite         string   `json:"sprite,omitempty"`
	SpriteSourceID string   `json:"sprite_source_id,omitempty"`
	Foreground     string   `json:"foreground"`
	Background     string   `json:"background,omitempty"`
	FallbackPath   []string `json:"fallback_path"`
}

type Registry struct {
	sets  map[string]*RuleSet
	order []string
}

//go:embed default_rules.json
var embeddedDefaultRules []byte

var defaultRegistry = mustLoadDefaultRegistry()

func DefaultRegistry() (*Registry, error) {
	return defaultRegistry, nil
}

func mustLoadDefaultRegistry() *Registry {
	registry, err := NewRegistry(embeddedDefaultRules)
	if err != nil {
		panic(fmt.Sprintf("load embedded city spatial rules: %v", err))
	}
	return registry
}

func NewRegistry(documents ...[]byte) (*Registry, error) {
	if len(documents) == 0 {
		return nil, fmt.Errorf("%w: at least one document is required", ErrInvalidRuleSet)
	}
	registry := &Registry{sets: make(map[string]*RuleSet, len(documents))}
	for _, document := range documents {
		ruleSet, err := LoadRuleSet(document)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.sets[ruleSet.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate rule set id %q", ErrInvalidRuleSet, ruleSet.ID)
		}
		registry.sets[ruleSet.ID] = ruleSet
		registry.order = append(registry.order, ruleSet.ID)
	}
	sort.Strings(registry.order)
	return registry, nil
}

func LoadRuleSet(document []byte) (*RuleSet, error) {
	if len(document) == 0 || len(document) > maximumRuleBytes || !utf8.Valid(document) {
		return nil, fmt.Errorf("%w: document size or encoding", ErrInvalidRuleSet)
	}
	if err := rejectDuplicateJSONKeys(document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuleSet, err)
	}
	var sourceFields map[string]json.RawMessage
	if err := json.Unmarshal(document, &sourceFields); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalidRuleSet, err)
	}
	if _, supplied := sourceFields["content_hash"]; supplied {
		return nil, fmt.Errorf("%w: content_hash is generated", ErrInvalidRuleSet)
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var parsed RuleSet
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrInvalidRuleSet, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuleSet, err)
	}
	normalized, err := normalizeRuleSet(parsed)
	if err != nil {
		return nil, err
	}
	canonical, err := marshalCanonicalRuleSet(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize: %v", ErrInvalidRuleSet, err)
	}
	digest := sha256.Sum256(canonical)
	normalized.ContentHash = hex.EncodeToString(digest[:])
	return &normalized, nil
}

func (registry *Registry) List() []RuleSetSummary {
	if registry == nil {
		return []RuleSetSummary{}
	}
	result := make([]RuleSetSummary, 0, len(registry.order))
	for _, id := range registry.order {
		ruleSet := registry.sets[id]
		result = append(result, RuleSetSummary{
			ID: ruleSet.ID, Version: ruleSet.Version, Name: ruleSet.Name,
			ChunkSize: ruleSet.ChunkSize, MinimumZ: ruleSet.MinimumZ,
			MaximumZ: ruleSet.MaximumZ, DefinitionCount: len(ruleSet.Definitions),
			ContentHash: ruleSet.ContentHash,
		})
	}
	return result
}

func (registry *Registry) Get(id string) (*RuleSet, error) {
	if registry == nil {
		return nil, ErrRuleSetNotFound
	}
	ruleSet, exists := registry.sets[strings.TrimSpace(id)]
	if !exists {
		return nil, ErrRuleSetNotFound
	}
	return cloneRuleSet(ruleSet)
}

func (registry *Registry) ResolveVisual(ruleSetID string, kind RuleKind, definitionID string) (ResolvedVisual, error) {
	ruleSet, err := registry.Get(ruleSetID)
	if err != nil {
		return ResolvedVisual{}, err
	}
	return ruleSet.ResolveVisual(kind, definitionID)
}

func (ruleSet *RuleSet) ResolveVisual(kind RuleKind, definitionID string) (ResolvedVisual, error) {
	if ruleSet == nil || !isKnownKind(kind) {
		return ResolvedVisual{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidRuleSet, kind)
	}
	definitions := make(map[string]Definition, len(ruleSet.Definitions))
	for _, definition := range ruleSet.Definitions {
		definitions[definition.ID] = definition
	}
	original, exists := definitions[definitionID]
	if !exists || original.Kind != kind {
		definitionID = missingDefinitionID(kind)
		original = definitions[definitionID]
	}

	path := make([]string, 0, 4)
	chain := make([]Definition, 0, 4)
	current := original
	for depth := 0; depth <= maximumFallbacks; depth++ {
		path = append(path, current.ID)
		chain = append(chain, current)
		if current.LooksLike == "" {
			break
		}
		current = definitions[current.LooksLike]
	}

	resolved := ResolvedVisual{
		DefinitionID: original.ID, Kind: original.Kind,
		Foreground: original.Foreground, Background: original.Background,
		FallbackPath: path,
	}
	for _, definition := range chain {
		if definition.Sprite != "" {
			resolved.Sprite = definition.Sprite
			resolved.SpriteSourceID = definition.ID
			break
		}
	}
	for _, definition := range chain {
		if definition.Glyph != "" {
			resolved.Glyph = definition.Glyph
			resolved.GlyphSourceID = definition.ID
			break
		}
	}
	if resolved.Glyph == "" {
		return ResolvedVisual{}, fmt.Errorf("%w: definition %q has no glyph fallback", ErrInvalidRuleSet, original.ID)
	}
	return resolved, nil
}

func normalizeRuleSet(input RuleSet) (RuleSet, error) {
	if !identifierPattern.MatchString(input.ID) {
		return RuleSet{}, fmt.Errorf("%w: invalid id %q", ErrInvalidRuleSet, input.ID)
	}
	if !versionPattern.MatchString(input.Version) {
		return RuleSet{}, fmt.Errorf("%w: invalid version %q", ErrInvalidRuleSet, input.Version)
	}
	if err := validateDisplayName(input.Name); err != nil {
		return RuleSet{}, fmt.Errorf("%w: name: %v", ErrInvalidRuleSet, err)
	}
	if input.ChunkSize != DefaultChunkSize {
		return RuleSet{}, fmt.Errorf("%w: chunk_size must be %d", ErrInvalidRuleSet, DefaultChunkSize)
	}
	if err := ValidateZ(input.MinimumZ, MinimumZ, MaximumZ); err != nil {
		return RuleSet{}, fmt.Errorf("%w: min_z: %v", ErrInvalidRuleSet, err)
	}
	if err := ValidateZ(input.MaximumZ, input.MinimumZ, MaximumZ); err != nil {
		return RuleSet{}, fmt.Errorf("%w: max_z: %v", ErrInvalidRuleSet, err)
	}
	if len(input.Palette) == 0 || len(input.Palette) > maximumPalette {
		return RuleSet{}, fmt.Errorf("%w: palette count", ErrInvalidRuleSet)
	}
	if len(input.Definitions) == 0 || len(input.Definitions) > maximumRules {
		return RuleSet{}, fmt.Errorf("%w: definition count", ErrInvalidRuleSet)
	}

	result := input
	result.ContentHash = ""
	result.Palette = append([]PaletteEntry(nil), input.Palette...)
	result.Definitions = make([]Definition, len(input.Definitions))
	copy(result.Definitions, input.Definitions)

	paletteIDs := make(map[string]struct{}, len(result.Palette))
	for index := range result.Palette {
		entry := &result.Palette[index]
		if !identifierPattern.MatchString(entry.ID) {
			return RuleSet{}, fmt.Errorf("%w: invalid palette id %q", ErrInvalidRuleSet, entry.ID)
		}
		if _, duplicate := paletteIDs[entry.ID]; duplicate {
			return RuleSet{}, fmt.Errorf("%w: duplicate palette id %q", ErrInvalidRuleSet, entry.ID)
		}
		paletteIDs[entry.ID] = struct{}{}
		if err := validateDisplayName(entry.Name); err != nil {
			return RuleSet{}, fmt.Errorf("%w: palette %q name: %v", ErrInvalidRuleSet, entry.ID, err)
		}
		if entry.ClassicForeground < 0 || entry.ClassicForeground > 255 ||
			entry.ClassicBackground != nil && (*entry.ClassicBackground < 0 || *entry.ClassicBackground > 255) {
			return RuleSet{}, fmt.Errorf("%w: palette %q classic color", ErrInvalidRuleSet, entry.ID)
		}
	}
	sort.Slice(result.Palette, func(i, j int) bool { return result.Palette[i].ID < result.Palette[j].ID })

	definitions := make(map[string]*Definition, len(result.Definitions))
	for index := range result.Definitions {
		definition := &result.Definitions[index]
		if !identifierPattern.MatchString(definition.ID) {
			return RuleSet{}, fmt.Errorf("%w: invalid definition id %q", ErrInvalidRuleSet, definition.ID)
		}
		if _, duplicate := definitions[definition.ID]; duplicate {
			return RuleSet{}, fmt.Errorf("%w: duplicate definition id %q", ErrInvalidRuleSet, definition.ID)
		}
		definitions[definition.ID] = definition
		if !isKnownKind(definition.Kind) {
			return RuleSet{}, fmt.Errorf("%w: definition %q kind %q", ErrInvalidRuleSet, definition.ID, definition.Kind)
		}
		if err := validateDisplayName(definition.Name); err != nil {
			return RuleSet{}, fmt.Errorf("%w: definition %q name: %v", ErrInvalidRuleSet, definition.ID, err)
		}
		if definition.Glyph != "" {
			runeValue, runeSize := utf8.DecodeRuneInString(definition.Glyph)
			if runeValue == utf8.RuneError && runeSize == 1 || utf8.RuneCountInString(definition.Glyph) != 1 ||
				!unicode.IsPrint(runeValue) || unicode.IsSpace(runeValue) {
				return RuleSet{}, fmt.Errorf("%w: definition %q glyph", ErrInvalidRuleSet, definition.ID)
			}
		}
		if definition.Glyph == "" && definition.LooksLike == "" {
			return RuleSet{}, fmt.Errorf("%w: definition %q needs a glyph fallback", ErrInvalidRuleSet, definition.ID)
		}
		if definition.Sprite != "" && (len(definition.Sprite) > 128 || !spritePattern.MatchString(definition.Sprite)) {
			return RuleSet{}, fmt.Errorf("%w: definition %q sprite", ErrInvalidRuleSet, definition.ID)
		}
		if _, exists := paletteIDs[definition.Foreground]; !exists {
			return RuleSet{}, fmt.Errorf("%w: definition %q foreground %q", ErrInvalidRuleSet, definition.ID, definition.Foreground)
		}
		if definition.Background != "" {
			if _, exists := paletteIDs[definition.Background]; !exists {
				return RuleSet{}, fmt.Errorf("%w: definition %q background %q", ErrInvalidRuleSet, definition.ID, definition.Background)
			}
		}
		if definition.MovementCost < 0 || definition.MovementCost > 10000 {
			return RuleSet{}, fmt.Errorf("%w: definition %q movement_cost", ErrInvalidRuleSet, definition.ID)
		}
		flagSet := make(map[string]struct{}, len(definition.Flags))
		for _, flag := range definition.Flags {
			if _, known := knownFlags[flag]; !known {
				return RuleSet{}, fmt.Errorf("%w: definition %q flag %q", ErrInvalidRuleSet, definition.ID, flag)
			}
			if _, duplicate := flagSet[flag]; duplicate {
				return RuleSet{}, fmt.Errorf("%w: definition %q duplicate flag %q", ErrInvalidRuleSet, definition.ID, flag)
			}
			flagSet[flag] = struct{}{}
		}
		_, passable := flagSet["passable"]
		if passable && definition.MovementCost == 0 || !passable && definition.MovementCost != 0 {
			return RuleSet{}, fmt.Errorf("%w: definition %q passability and movement_cost disagree", ErrInvalidRuleSet, definition.ID)
		}
		sort.Strings(definition.Flags)
		if definition.Flags == nil {
			definition.Flags = []string{}
		}
		if definition.Metadata == nil {
			definition.Metadata = map[string]any{}
		}
	}

	for _, kind := range knownKinds {
		missingID := missingDefinitionID(kind)
		missing, exists := definitions[missingID]
		if !exists || missing.Kind != kind || missing.Glyph == "" || missing.LooksLike != "" {
			return RuleSet{}, fmt.Errorf("%w: missing terminal %q", ErrInvalidRuleSet, missingID)
		}
	}
	for _, definition := range definitions {
		if definition.LooksLike == "" {
			continue
		}
		target, exists := definitions[definition.LooksLike]
		if !exists {
			return RuleSet{}, fmt.Errorf("%w: definition %q looks_like %q", ErrInvalidRuleSet, definition.ID, definition.LooksLike)
		}
		if target.Kind != definition.Kind {
			return RuleSet{}, fmt.Errorf("%w: definition %q crosses kind boundary", ErrInvalidRuleSet, definition.ID)
		}
	}
	if err := validateFallbackGraph(definitions); err != nil {
		return RuleSet{}, err
	}

	sort.Slice(result.Definitions, func(i, j int) bool {
		if result.Definitions[i].Kind == result.Definitions[j].Kind {
			return result.Definitions[i].ID < result.Definitions[j].ID
		}
		return result.Definitions[i].Kind < result.Definitions[j].Kind
	})
	return result, nil
}

func validateFallbackGraph(definitions map[string]*Definition) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(definitions))
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if depth > maximumFallbacks {
			return fmt.Errorf("%w: fallback chain for %q exceeds %d", ErrInvalidRuleSet, id, maximumFallbacks)
		}
		switch states[id] {
		case visiting:
			return fmt.Errorf("%w: fallback cycle at %q", ErrInvalidRuleSet, id)
		case visited:
			return nil
		}
		states[id] = visiting
		definition := definitions[id]
		if definition.LooksLike != "" {
			if err := visit(definition.LooksLike, depth+1); err != nil {
				return err
			}
		}
		states[id] = visited
		return nil
	}
	for id := range definitions {
		if err := visit(id, 0); err != nil {
			return err
		}
	}
	return nil
}

func marshalCanonicalRuleSet(ruleSet RuleSet) ([]byte, error) {
	type canonicalRuleSet struct {
		ID          string         `json:"id"`
		Version     string         `json:"version"`
		Name        string         `json:"name"`
		ChunkSize   int64          `json:"chunk_size"`
		MinimumZ    int32          `json:"min_z"`
		MaximumZ    int32          `json:"max_z"`
		Palette     []PaletteEntry `json:"palette"`
		Definitions []Definition   `json:"definitions"`
	}
	return json.Marshal(canonicalRuleSet{
		ID: ruleSet.ID, Version: ruleSet.Version, Name: ruleSet.Name,
		ChunkSize: ruleSet.ChunkSize, MinimumZ: ruleSet.MinimumZ, MaximumZ: ruleSet.MaximumZ,
		Palette: ruleSet.Palette, Definitions: ruleSet.Definitions,
	})
}

func cloneRuleSet(ruleSet *RuleSet) (*RuleSet, error) {
	encoded, err := json.Marshal(ruleSet)
	if err != nil {
		return nil, err
	}
	var clone RuleSet
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err = decoder.Decode(&clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func validateDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
		return errors.New("must contain 1..64 valid Unicode characters")
	}
	return nil
}

func isKnownKind(kind RuleKind) bool {
	for _, candidate := range knownKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

func missingDefinitionID(kind RuleKind) string {
	return "missing." + string(kind)
}

func rejectDuplicateJSONKeys(document []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := keys[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			keys[key] = struct{}{}
			if err = consumeJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = consumeJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != map[json.Delim]json.Delim{'{': '}', '[': ']'}[delimiter] {
		return errors.New("mismatched JSON delimiter")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON content")
		}
		return err
	}
	return nil
}
