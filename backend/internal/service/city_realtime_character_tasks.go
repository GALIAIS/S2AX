package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	cityRealtimeCharacterTaskSchemaVersion  = 1
	cityRealtimeCharacterTaskCatalogID      = "city-realtime-character-task-core"
	cityRealtimeCharacterTaskCatalogVersion = "1.0.0"
	cityRealtimeCharacterTaskBindingVersion = "city-realtime-character-task-binding-v1"
	cityRealtimeCharacterTaskStateVersion   = "city-realtime-character-task-state-v1"
	cityRealtimeCharacterTaskChainVersion   = "city-realtime-character-task-chain-v1"
	cityRealtimeCharacterTaskEventVersion   = "city-realtime-character-task-event-v1"
	cityRealtimeCharacterTaskRunVersion     = "city-realtime-character-task-run-v1"

	// A task is a bounded, content-defined agreement to perform exactly one
	// already-catalogued activity. It has no task-specific reward: only the
	// normal activity reducer can change life/progression state. Keeping the
	// expiry server-owned prevents an accepted task from becoming an indefinite
	// background obligation when an Agent is paused or its observation expires.
	cityRealtimeCharacterTaskExpiryDelayUS  int64 = 300 * cityRealtimeTimeQuantumUS
	cityRealtimeCharacterTaskExpiryPriority       = 110

	cityRealtimeCharacterTaskAccepted  = "accepted"
	cityRealtimeCharacterTaskCompleted = "completed"
	cityRealtimeCharacterTaskExpired   = "expired"

	cityRealtimeCharacterTaskAcceptedEvent  = "task_accepted"
	cityRealtimeCharacterTaskCompletedEvent = "task_completed"
	cityRealtimeCharacterTaskExpiredEvent   = "task_expired"

	cityRealtimeDueEventTypeCharacterTaskExpire = "system.realtime.character_task_expire"
)

// CityRealtimeCharacterTask is an owner-private task projection. It excludes
// Agent codes, intent hashes, activity hashes, provider data, reward data and
// other characters' state. Task definitions themselves are content facts; a
// caller can only read its own accepted/completed/expired task history.
type CityRealtimeCharacterTask struct {
	TaskRunCode              string `json:"task_run_code"`
	TaskCode                 string `json:"task_code"`
	ActivityCode             string `json:"activity_code"`
	Status                   string `json:"status"`
	Revision                 int64  `json:"revision"`
	AcceptedFrameSequence    int64  `json:"accepted_frame_sequence"`
	ExpirationDueWorldTimeUS int64  `json:"expiration_due_world_time_us"`
	CompletedFrameSequence   int64  `json:"completed_frame_sequence,omitempty"`
	LastFrameSequence        int64  `json:"last_frame_sequence"`
}

type CityRealtimeCharacterTaskListInput struct {
	UserID  int64
	WorldID int64
	Limit   int
}

type cityRealtimeCharacterTaskDefinition struct {
	Code              string `json:"code"`
	ActivityCode      string `json:"activity_code"`
	ExpirationDelayUS int64  `json:"expiration_delay_us"`
}

type cityRealtimeCharacterTaskCatalogManifest struct {
	SchemaVersion      int                                   `json:"schema_version"`
	CompletionContract string                                `json:"completion_contract"`
	Tasks              []cityRealtimeCharacterTaskDefinition `json:"tasks"`
}

type cityRealtimeCharacterTaskBinding struct {
	SchemaVersion       int    `json:"schema_version"`
	AgentBindingHash    string `json:"agent_binding_hash"`
	ActivityBindingHash string `json:"activity_binding_hash"`
	CatalogID           string `json:"catalog_id"`
	CatalogVersion      string `json:"catalog_version"`
	CatalogHash         string `json:"catalog_hash"`
	BindingHash         string `json:"binding_hash"`
}

type cityRealtimeCharacterTaskRuntime struct {
	Binding     cityRealtimeCharacterTaskBinding
	Definitions map[string]cityRealtimeCharacterTaskDefinition
}

// cityRealtimeCharacterTaskHead is the sole mutable task projection. Each
// task run is append-only through its event chain. A Character has at most one
// accepted run at a time, enforced both by a partial unique index and by the
// service recheck before accepting another task.
type cityRealtimeCharacterTaskHead struct {
	ActorCode                       string
	TaskRunCode                     string
	TaskCode                        string
	ActivityCode                    string
	SourceIntentCode                string
	TaskRevision                    int64
	TaskStatus                      string
	AcceptedFrameSequence           int64
	ExpirationDueWorldTimeUS        int64
	CompletionActivityEventSequence int64
	CompletionActivityEventHash     string
	LastFrameSequence               int64
	EventChainHash                  string
	StateHash                       string
}

// cityRealtimeCharacterTaskEvent deliberately contains no free text, reward,
// wallet, case, Rule, penalty, provider, or user-claimed completion data. A
// completion records only the exact pre-existing activity event identity.
type cityRealtimeCharacterTaskEvent struct {
	ActorCode                       string
	TaskRunCode                     string
	TaskCode                        string
	ActivityCode                    string
	SourceIntentCode                string
	EventSequence                   int64
	FrameSequence                   int64
	EventType                       string
	ExpirationDueWorldTimeUS        int64
	CompletionActivityEventSequence int64
	CompletionActivityEventHash     string
	PreviousEventHash               string
	EventHash                       string
}

type cityRealtimeCharacterTaskHashState struct {
	SchemaVersion int                               `json:"schema_version"`
	Binding       *cityRealtimeCharacterTaskBinding `json:"binding,omitempty"`
	Heads         []cityRealtimeCharacterTaskHead   `json:"heads"`
}

type cityRealtimeCharacterTaskExpiryDuePayload struct {
	SchemaVersion    int    `json:"schema_version"`
	ActorCode        string `json:"actor_code"`
	TaskRunCode      string `json:"task_run_code"`
	TaskCode         string `json:"task_code"`
	SourceIntentCode string `json:"source_intent_code"`
}

func cityRealtimeCharacterTaskCatalogDefinitions() []cityRealtimeCharacterTaskDefinition {
	return []cityRealtimeCharacterTaskDefinition{
		{Code: "task.civic.cleanup", ActivityCode: "civic.cleanup", ExpirationDelayUS: cityRealtimeCharacterTaskExpiryDelayUS},
		{Code: "task.civic.shift", ActivityCode: "work.civic_shift", ExpirationDelayUS: cityRealtimeCharacterTaskExpiryDelayUS},
	}
}

func cityRealtimeCharacterTaskCodeValid(code string) bool {
	return strings.HasPrefix(code, "task.") && cityRealtimeAgentIdentifierValid(code, 64)
}

func cityRealtimeCharacterTaskRunCodeValid(code string) bool {
	return strings.HasPrefix(code, "task.run.") && cityRealtimeAgentIdentifierValid(code, 96)
}

func cityRealtimeCharacterTaskDefinitionValid(definition cityRealtimeCharacterTaskDefinition) bool {
	return cityRealtimeCharacterTaskCodeValid(definition.Code) &&
		cityRealtimeAgentIdentifierValid(definition.ActivityCode, 64) &&
		definition.ExpirationDelayUS == cityRealtimeCharacterTaskExpiryDelayUS &&
		definition.ExpirationDelayUS%cityRealtimeTimeQuantumUS == 0
}

func cityRealtimeCharacterTaskDefinitionsEqual(left, right cityRealtimeCharacterTaskDefinition) bool {
	return left.Code == right.Code && left.ActivityCode == right.ActivityCode &&
		left.ExpirationDelayUS == right.ExpirationDelayUS && cityRealtimeCharacterTaskDefinitionValid(left)
}

func cityRealtimeCharacterTaskCatalogManifestDecode(
	raw []byte,
	catalogVersion string,
) (map[string]cityRealtimeCharacterTaskDefinition, error) {
	manifest := cityRealtimeCharacterTaskCatalogManifest{}
	if len(raw) == 0 || json.Unmarshal(raw, &manifest) != nil ||
		catalogVersion != cityRealtimeCharacterTaskCatalogVersion ||
		manifest.SchemaVersion != cityRealtimeCharacterTaskSchemaVersion ||
		manifest.CompletionContract != "sealed_agent_activity_event" {
		return nil, errors.New("invalid realtime character task catalog manifest")
	}
	expectedDefinitions := cityRealtimeCharacterTaskCatalogDefinitions()
	if len(manifest.Tasks) != len(expectedDefinitions) {
		return nil, errors.New("unexpected realtime character task catalog size")
	}
	definitions := make(map[string]cityRealtimeCharacterTaskDefinition, len(manifest.Tasks))
	for index, definition := range manifest.Tasks {
		if !cityRealtimeCharacterTaskDefinitionsEqual(expectedDefinitions[index], definition) {
			return nil, fmt.Errorf("unexpected realtime character task definition %q", definition.Code)
		}
		if _, duplicate := definitions[definition.Code]; duplicate {
			return nil, fmt.Errorf("duplicate realtime character task definition %q", definition.Code)
		}
		definitions[definition.Code] = definition
	}
	return definitions, nil
}

func cityRealtimeCharacterTaskBindingHash(binding cityRealtimeCharacterTaskBinding) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTaskBindingVersion,
		binding.AgentBindingHash,
		binding.ActivityBindingHash,
		binding.CatalogID,
		binding.CatalogVersion,
		binding.CatalogHash,
	}, "\x1f")))
}

func cityRealtimeCharacterTaskBindingValid(binding cityRealtimeCharacterTaskBinding) bool {
	return binding.SchemaVersion == cityRealtimeCharacterTaskSchemaVersion &&
		binding.CatalogID == cityRealtimeCharacterTaskCatalogID &&
		binding.CatalogVersion == cityRealtimeCharacterTaskCatalogVersion &&
		cityRealtimeSHA256Hex(binding.AgentBindingHash) &&
		cityRealtimeSHA256Hex(binding.ActivityBindingHash) &&
		cityRealtimeSHA256Hex(binding.CatalogHash) &&
		cityRealtimeSHA256Hex(binding.BindingHash) &&
		binding.BindingHash == cityRealtimeCharacterTaskBindingHash(binding)
}

func loadCityRealtimeCharacterTaskCoreRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	agentBinding cityRealtimeAgentPolicyBinding,
	activityBinding cityRealtimeCharacterActivityCatalogBinding,
) (*cityRealtimeCharacterTaskRuntime, error) {
	if !cityRealtimeAgentCharacterTaskRuntimeEnabled(agentBinding) ||
		!validateCityRealtimeCharacterActivityBinding(activityBinding) {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterTaskBinding{
		SchemaVersion:       cityRealtimeCharacterTaskSchemaVersion,
		AgentBindingHash:    agentBinding.BindingHash,
		ActivityBindingHash: activityBinding.BindingHash,
		CatalogID:           cityRealtimeCharacterTaskCatalogID,
		CatalogVersion:      cityRealtimeCharacterTaskCatalogVersion,
	}
	var status string
	var rawManifest []byte
	err := queryer.QueryRowContext(ctx, `
SELECT catalog_hash, status, manifest
FROM city_realtime_character_task_catalogs
WHERE catalog_id = $1 AND catalog_version = $2`, binding.CatalogID, binding.CatalogVersion,
	).Scan(&binding.CatalogHash, &status, &rawManifest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_catalog"})
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character task core catalog: %w", err)
	}
	definitions, decodeErr := cityRealtimeCharacterTaskCatalogManifestDecode(rawManifest, binding.CatalogVersion)
	if decodeErr != nil || status != "published" || !cityRealtimeSHA256Hex(binding.CatalogHash) {
		if decodeErr == nil {
			decodeErr = errors.New("invalid realtime character task catalog")
		}
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_catalog"}).WithCause(decodeErr)
	}
	binding.BindingHash = cityRealtimeCharacterTaskBindingHash(binding)
	if !cityRealtimeCharacterTaskBindingValid(binding) {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_binding"})
	}
	return &cityRealtimeCharacterTaskRuntime{Binding: binding, Definitions: definitions}, nil
}

func initializeCityRealtimeCharacterTaskFoundation(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	simulationVersion string,
) error {
	if tx == nil || worldID <= 0 || !cityEngineSupportsRealtimeStaticWorldgen(simulationVersion) {
		return ErrCitySimulationVersion.WithMetadata(map[string]string{"version": simulationVersion})
	}
	agentState, err := loadCityRealtimeAgentHashState(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if agentState == nil || agentState.Binding == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_policy"})
	}
	if !cityRealtimeAgentCharacterTaskRuntimeEnabled(*agentState.Binding) {
		return nil
	}
	lifeRuntime, err := loadCityRealtimeCharacterLifeRuntime(ctx, tx, worldID)
	if err != nil {
		return err
	}
	if lifeRuntime == nil {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_activity_binding"})
	}
	runtime, err := loadCityRealtimeCharacterTaskCoreRuntime(ctx, tx, *agentState.Binding, lifeRuntime.Binding)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`SELECT set_config('sub2api.city_realtime_character_task_initialize_world_id', $1, TRUE)`,
		strconv.FormatInt(worldID, 10),
	); err != nil {
		return fmt.Errorf("activate realtime character task initialization gate: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_task_world_bindings
    (world_id, schema_version, agent_binding_hash, activity_binding_hash,
     catalog_id, catalog_version, catalog_hash, binding_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb)`,
		worldID, runtime.Binding.SchemaVersion, runtime.Binding.AgentBindingHash,
		runtime.Binding.ActivityBindingHash, runtime.Binding.CatalogID,
		runtime.Binding.CatalogVersion, runtime.Binding.CatalogHash, runtime.Binding.BindingHash,
	); err != nil {
		return fmt.Errorf("create realtime character task binding: %w", err)
	}
	return nil
}

func enableCityRealtimeCharacterTaskMutationGate(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence, dueWorldTimeUS int64,
) error {
	if tx == nil || worldID <= 0 || frameSequence <= 0 || dueWorldTimeUS < 0 ||
		dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return ErrCityInvalidInput
	}
	for _, setting := range []struct {
		name  string
		value int64
	}{
		{name: "sub2api.city_realtime_character_task_world_id", value: worldID},
		{name: "sub2api.city_realtime_character_task_frame_sequence", value: frameSequence},
		{name: "sub2api.city_realtime_character_task_due_world_time_us", value: dueWorldTimeUS},
	} {
		if _, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, TRUE)`, setting.name, strconv.FormatInt(setting.value, 10)); err != nil {
			return fmt.Errorf("activate realtime character task gate %s: %w", setting.name, err)
		}
	}
	return nil
}

func loadCityRealtimeCharacterTaskRuntime(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterTaskRuntime, error) {
	if worldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	binding := cityRealtimeCharacterTaskBinding{}
	var policyID, policyVersion, agentBindingHash, activityBindingHash string
	var status string
	var rawManifest []byte
	err := queryer.QueryRowContext(ctx, `
SELECT task.schema_version, task.agent_binding_hash, task.activity_binding_hash,
       task.catalog_id, task.catalog_version, task.catalog_hash, task.binding_hash,
       agent.policy_id, agent.policy_version, agent.binding_hash,
       activity.binding_hash, catalog.status, catalog.manifest
FROM city_realtime_character_task_world_bindings task
JOIN city_realtime_agent_world_bindings agent ON agent.world_id = task.world_id
JOIN city_realtime_character_activity_world_bindings activity ON activity.world_id = task.world_id
JOIN city_realtime_character_task_catalogs catalog
  ON catalog.catalog_id = task.catalog_id AND catalog.catalog_version = task.catalog_version
WHERE task.world_id = $1`, worldID).Scan(
		&binding.SchemaVersion, &binding.AgentBindingHash, &binding.ActivityBindingHash,
		&binding.CatalogID, &binding.CatalogVersion, &binding.CatalogHash, &binding.BindingHash,
		&policyID, &policyVersion, &agentBindingHash, &activityBindingHash, &status, &rawManifest,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var taskCount int
		if countErr := queryer.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM city_realtime_character_task_heads WHERE world_id = $1`, worldID,
		).Scan(&taskCount); countErr != nil {
			return nil, fmt.Errorf("check historical realtime character task state: %w", countErr)
		}
		if taskCount != 0 {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_binding"})
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load realtime character task binding: %w", err)
	}
	if policyID != cityRealtimeAgentCorePolicyID ||
		(policyVersion != cityRealtimeAgentCorePolicyVersionTask &&
			policyVersion != cityRealtimeAgentCorePolicyVersionNavigationPlan) ||
		binding.AgentBindingHash != agentBindingHash || binding.ActivityBindingHash != activityBindingHash ||
		!cityRealtimeCharacterTaskBindingValid(binding) || status != "published" {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_binding"})
	}
	definitions, decodeErr := cityRealtimeCharacterTaskCatalogManifestDecode(rawManifest, binding.CatalogVersion)
	if decodeErr != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_catalog"}).WithCause(decodeErr)
	}
	return &cityRealtimeCharacterTaskRuntime{Binding: binding, Definitions: definitions}, nil
}

func cityRealtimeCharacterTaskRunCode(
	actorCode, taskCode, sourceIntentCode string,
) (string, error) {
	if !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterTaskCodeValid(taskCode) ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) {
		return "", ErrCityInvalidInput
	}
	code := "task.run." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTaskRunVersion,
		actorCode,
		taskCode,
		sourceIntentCode,
	}, "\x1f")))
	if !cityRealtimeCharacterTaskRunCodeValid(code) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_run_code"})
	}
	return code, nil
}

func cityRealtimeCharacterTaskChainGenesisHash(head cityRealtimeCharacterTaskHead) (string, error) {
	if !cityRealtimeCharacterTaskStaticFieldsValid(head) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTaskChainVersion,
		head.ActorCode,
		head.TaskRunCode,
		head.TaskCode,
		head.ActivityCode,
		head.SourceIntentCode,
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
		strconv.FormatInt(head.ExpirationDueWorldTimeUS, 10),
	}, "\x1f"))), nil
}

func cityRealtimeCharacterTaskStaticFieldsValid(head cityRealtimeCharacterTaskHead) bool {
	return cityRealtimePlayerActorCodeValid(head.ActorCode) &&
		cityRealtimeCharacterTaskRunCodeValid(head.TaskRunCode) &&
		cityRealtimeCharacterTaskCodeValid(head.TaskCode) &&
		cityRealtimeAgentIdentifierValid(head.ActivityCode, 64) &&
		cityRealtimeAgentIdentifierValid(head.SourceIntentCode, 96)
}

func cityRealtimeCharacterTaskHeadStateHashUnchecked(head cityRealtimeCharacterTaskHead) string {
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTaskStateVersion,
		head.ActorCode,
		head.TaskRunCode,
		head.TaskCode,
		head.ActivityCode,
		head.SourceIntentCode,
		strconv.FormatInt(head.TaskRevision, 10),
		head.TaskStatus,
		strconv.FormatInt(head.AcceptedFrameSequence, 10),
		strconv.FormatInt(head.ExpirationDueWorldTimeUS, 10),
		strconv.FormatInt(head.CompletionActivityEventSequence, 10),
		head.CompletionActivityEventHash,
		strconv.FormatInt(head.LastFrameSequence, 10),
		head.EventChainHash,
	}, "\x1f")))
}

func cityRealtimeCharacterTaskHeadValid(head cityRealtimeCharacterTaskHead) bool {
	if !cityRealtimeCharacterTaskStaticFieldsValid(head) || head.AcceptedFrameSequence <= 0 ||
		head.ExpirationDueWorldTimeUS <= 0 ||
		head.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		head.LastFrameSequence < head.AcceptedFrameSequence ||
		!cityRealtimeSHA256Hex(head.EventChainHash) || !cityRealtimeSHA256Hex(head.StateHash) {
		return false
	}
	switch head.TaskRevision {
	case 1:
		if head.TaskStatus != cityRealtimeCharacterTaskAccepted ||
			head.LastFrameSequence != head.AcceptedFrameSequence ||
			head.CompletionActivityEventSequence != 0 || head.CompletionActivityEventHash != "" {
			return false
		}
	case 2:
		if head.LastFrameSequence <= head.AcceptedFrameSequence {
			return false
		}
		switch head.TaskStatus {
		case cityRealtimeCharacterTaskCompleted:
			if head.CompletionActivityEventSequence <= 0 || !cityRealtimeSHA256Hex(head.CompletionActivityEventHash) {
				return false
			}
		case cityRealtimeCharacterTaskExpired:
			if head.CompletionActivityEventSequence != 0 || head.CompletionActivityEventHash != "" {
				return false
			}
		default:
			return false
		}
	default:
		return false
	}
	return head.StateHash == cityRealtimeCharacterTaskHeadStateHashUnchecked(head)
}

func cityRealtimeCharacterTaskEventValid(event cityRealtimeCharacterTaskEvent) bool {
	if !cityRealtimePlayerActorCodeValid(event.ActorCode) || !cityRealtimeCharacterTaskRunCodeValid(event.TaskRunCode) ||
		!cityRealtimeCharacterTaskCodeValid(event.TaskCode) || !cityRealtimeAgentIdentifierValid(event.ActivityCode, 64) ||
		!cityRealtimeAgentIdentifierValid(event.SourceIntentCode, 96) || event.EventSequence <= 0 ||
		event.FrameSequence <= 0 || event.ExpirationDueWorldTimeUS <= 0 ||
		event.ExpirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 ||
		!cityRealtimeSHA256Hex(event.PreviousEventHash) ||
		(event.EventHash != "" && !cityRealtimeSHA256Hex(event.EventHash)) {
		return false
	}
	switch event.EventSequence {
	case 1:
		return event.EventType == cityRealtimeCharacterTaskAcceptedEvent &&
			event.CompletionActivityEventSequence == 0 && event.CompletionActivityEventHash == ""
	case 2:
		switch event.EventType {
		case cityRealtimeCharacterTaskCompletedEvent:
			return event.CompletionActivityEventSequence > 0 && cityRealtimeSHA256Hex(event.CompletionActivityEventHash)
		case cityRealtimeCharacterTaskExpiredEvent:
			return event.CompletionActivityEventSequence == 0 && event.CompletionActivityEventHash == ""
		default:
			return false
		}
	default:
		return false
	}
}

func cityRealtimeCharacterTaskEventHash(event cityRealtimeCharacterTaskEvent) (string, error) {
	if !cityRealtimeCharacterTaskEventValid(event) {
		return "", ErrCityInvalidInput
	}
	return cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		cityRealtimeCharacterTaskEventVersion,
		event.ActorCode,
		event.TaskRunCode,
		event.TaskCode,
		event.ActivityCode,
		event.SourceIntentCode,
		strconv.FormatInt(event.EventSequence, 10),
		strconv.FormatInt(event.FrameSequence, 10),
		event.EventType,
		strconv.FormatInt(event.ExpirationDueWorldTimeUS, 10),
		strconv.FormatInt(event.CompletionActivityEventSequence, 10),
		event.CompletionActivityEventHash,
		event.PreviousEventHash,
	}, "\x1f"))), nil
}

func cityRealtimeCharacterAcceptTask(
	definition cityRealtimeCharacterTaskDefinition,
	actorCode, sourceIntentCode string,
	frameSequence, expirationDueWorldTimeUS int64,
) (cityRealtimeCharacterTaskHead, cityRealtimeCharacterTaskEvent, error) {
	if !cityRealtimeCharacterTaskDefinitionValid(definition) || !cityRealtimePlayerActorCodeValid(actorCode) ||
		!cityRealtimeAgentIdentifierValid(sourceIntentCode, 96) || frameSequence <= 0 ||
		expirationDueWorldTimeUS <= 0 || expirationDueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, ErrCityInvalidInput
	}
	runCode, err := cityRealtimeCharacterTaskRunCode(actorCode, definition.Code, sourceIntentCode)
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, err
	}
	head := cityRealtimeCharacterTaskHead{
		ActorCode:                actorCode,
		TaskRunCode:              runCode,
		TaskCode:                 definition.Code,
		ActivityCode:             definition.ActivityCode,
		SourceIntentCode:         sourceIntentCode,
		TaskRevision:             1,
		TaskStatus:               cityRealtimeCharacterTaskAccepted,
		AcceptedFrameSequence:    frameSequence,
		ExpirationDueWorldTimeUS: expirationDueWorldTimeUS,
		LastFrameSequence:        frameSequence,
	}
	genesisHash, err := cityRealtimeCharacterTaskChainGenesisHash(head)
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, err
	}
	event := cityRealtimeCharacterTaskEvent{
		ActorCode:                head.ActorCode,
		TaskRunCode:              head.TaskRunCode,
		TaskCode:                 head.TaskCode,
		ActivityCode:             head.ActivityCode,
		SourceIntentCode:         head.SourceIntentCode,
		EventSequence:            1,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterTaskAcceptedEvent,
		ExpirationDueWorldTimeUS: expirationDueWorldTimeUS,
		PreviousEventHash:        genesisHash,
	}
	event.EventHash, err = cityRealtimeCharacterTaskEventHash(event)
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, err
	}
	head.EventChainHash = event.EventHash
	head.StateHash = cityRealtimeCharacterTaskHeadStateHashUnchecked(head)
	if !cityRealtimeCharacterTaskHeadValid(head) {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_accept"})
	}
	return head, event, nil
}

func cityRealtimeCharacterCompleteTask(
	previous cityRealtimeCharacterTaskHead,
	activity cityRealtimeCharacterActivityEventRecord,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterTaskHead, cityRealtimeCharacterTaskEvent, error) {
	if !cityRealtimeCharacterTaskHeadValid(previous) ||
		previous.TaskRevision != 1 || previous.TaskStatus != cityRealtimeCharacterTaskAccepted ||
		activity.ActorCode != previous.ActorCode || activity.ActivityCode != previous.ActivityCode ||
		activity.FrameSequence != frameSequence || frameSequence <= previous.LastFrameSequence ||
		dueWorldTimeUS < 0 || dueWorldTimeUS >= previous.ExpirationDueWorldTimeUS ||
		activity.EventSequence <= 0 || !cityRealtimeSHA256Hex(activity.EventHash) {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, ErrCityInvalidInput
	}
	next := previous
	next.TaskRevision = 2
	next.TaskStatus = cityRealtimeCharacterTaskCompleted
	next.CompletionActivityEventSequence = activity.EventSequence
	next.CompletionActivityEventHash = activity.EventHash
	next.LastFrameSequence = frameSequence
	event := cityRealtimeCharacterTaskEvent{
		ActorCode:                       previous.ActorCode,
		TaskRunCode:                     previous.TaskRunCode,
		TaskCode:                        previous.TaskCode,
		ActivityCode:                    previous.ActivityCode,
		SourceIntentCode:                previous.SourceIntentCode,
		EventSequence:                   2,
		FrameSequence:                   frameSequence,
		EventType:                       cityRealtimeCharacterTaskCompletedEvent,
		ExpirationDueWorldTimeUS:        previous.ExpirationDueWorldTimeUS,
		CompletionActivityEventSequence: activity.EventSequence,
		CompletionActivityEventHash:     activity.EventHash,
		PreviousEventHash:               previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterTaskEventHash(event)
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterTaskHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterTaskHeadValid(next) {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_complete"})
	}
	return next, event, nil
}

func cityRealtimeCharacterExpireTask(
	previous cityRealtimeCharacterTaskHead,
	frameSequence, dueWorldTimeUS int64,
) (cityRealtimeCharacterTaskHead, cityRealtimeCharacterTaskEvent, error) {
	if !cityRealtimeCharacterTaskHeadValid(previous) ||
		previous.TaskRevision != 1 || previous.TaskStatus != cityRealtimeCharacterTaskAccepted ||
		frameSequence <= previous.LastFrameSequence || dueWorldTimeUS != previous.ExpirationDueWorldTimeUS {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, ErrCityInvalidInput
	}
	next := previous
	next.TaskRevision = 2
	next.TaskStatus = cityRealtimeCharacterTaskExpired
	next.LastFrameSequence = frameSequence
	event := cityRealtimeCharacterTaskEvent{
		ActorCode:                previous.ActorCode,
		TaskRunCode:              previous.TaskRunCode,
		TaskCode:                 previous.TaskCode,
		ActivityCode:             previous.ActivityCode,
		SourceIntentCode:         previous.SourceIntentCode,
		EventSequence:            2,
		FrameSequence:            frameSequence,
		EventType:                cityRealtimeCharacterTaskExpiredEvent,
		ExpirationDueWorldTimeUS: previous.ExpirationDueWorldTimeUS,
		PreviousEventHash:        previous.EventChainHash,
	}
	var err error
	event.EventHash, err = cityRealtimeCharacterTaskEventHash(event)
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{}, err
	}
	next.EventChainHash = event.EventHash
	next.StateHash = cityRealtimeCharacterTaskHeadStateHashUnchecked(next)
	if !cityRealtimeCharacterTaskHeadValid(next) {
		return cityRealtimeCharacterTaskHead{}, cityRealtimeCharacterTaskEvent{},
			ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_expire"})
	}
	return next, event, nil
}

func scanCityRealtimeCharacterTaskHead(scanner cityScannable) (cityRealtimeCharacterTaskHead, error) {
	head := cityRealtimeCharacterTaskHead{}
	var completionSequence sql.NullInt64
	var completionHash sql.NullString
	if err := scanner.Scan(
		&head.ActorCode, &head.TaskRunCode, &head.TaskCode, &head.ActivityCode, &head.SourceIntentCode,
		&head.TaskRevision, &head.TaskStatus, &head.AcceptedFrameSequence, &head.ExpirationDueWorldTimeUS,
		&completionSequence, &completionHash, &head.LastFrameSequence, &head.EventChainHash, &head.StateHash,
	); err != nil {
		return cityRealtimeCharacterTaskHead{}, err
	}
	if completionSequence.Valid {
		head.CompletionActivityEventSequence = completionSequence.Int64
	}
	if completionHash.Valid {
		head.CompletionActivityEventHash = completionHash.String
	}
	return head, nil
}

func loadCityRealtimeCharacterTaskHead(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode, taskRunCode string,
	forUpdate bool,
) (cityRealtimeCharacterTaskHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) || !cityRealtimeCharacterTaskRunCodeValid(taskRunCode) {
		return cityRealtimeCharacterTaskHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, task_run_code, task_code, activity_code, source_intent_code,
       task_revision, task_status, accepted_frame_sequence, expiration_due_world_time_us,
       completion_activity_event_sequence, completion_activity_event_hash,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head, err := scanCityRealtimeCharacterTaskHead(queryer.QueryRowContext(ctx, query, worldID, actorCode, taskRunCode))
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterTaskHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, false, fmt.Errorf("load realtime character task head: %w", err)
	}
	if !cityRealtimeCharacterTaskHeadValid(head) {
		return cityRealtimeCharacterTaskHead{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_head"})
	}
	return head, true, nil
}

func loadCityRealtimeCharacterActiveTask(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	actorCode string,
	forUpdate bool,
) (cityRealtimeCharacterTaskHead, bool, error) {
	if worldID <= 0 || !cityRealtimePlayerActorCodeValid(actorCode) {
		return cityRealtimeCharacterTaskHead{}, false, ErrCityInvalidInput
	}
	query := `
SELECT actor_code, task_run_code, task_code, activity_code, source_intent_code,
       task_revision, task_status, accepted_frame_sequence, expiration_due_world_time_us,
       completion_activity_event_sequence, completion_activity_event_hash,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2 AND task_status = 'accepted'
ORDER BY accepted_frame_sequence ASC, task_run_code ASC
LIMIT 1`
	if forUpdate {
		query += " FOR UPDATE"
	}
	head, err := scanCityRealtimeCharacterTaskHead(queryer.QueryRowContext(ctx, query, worldID, actorCode))
	if errors.Is(err, sql.ErrNoRows) {
		return cityRealtimeCharacterTaskHead{}, false, nil
	}
	if err != nil {
		return cityRealtimeCharacterTaskHead{}, false, fmt.Errorf("load active realtime character task: %w", err)
	}
	if !cityRealtimeCharacterTaskHeadValid(head) || head.TaskStatus != cityRealtimeCharacterTaskAccepted || head.TaskRevision != 1 {
		return cityRealtimeCharacterTaskHead{}, false, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_active"})
	}
	return head, true, nil
}

func loadCityRealtimeCharacterTaskEvents(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterTaskHead,
) ([]cityRealtimeCharacterTaskEvent, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, task_run_code, task_code, activity_code, source_intent_code,
       event_sequence, frame_sequence, event_type, expiration_due_world_time_us,
       completion_activity_event_sequence, completion_activity_event_hash,
       previous_event_hash, event_hash
FROM city_realtime_character_task_events
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3
ORDER BY event_sequence ASC`, worldID, head.ActorCode, head.TaskRunCode)
	if err != nil {
		return nil, fmt.Errorf("load realtime character task events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]cityRealtimeCharacterTaskEvent, 0, head.TaskRevision)
	for rows.Next() {
		event := cityRealtimeCharacterTaskEvent{}
		var completionSequence sql.NullInt64
		var completionHash sql.NullString
		if err = rows.Scan(
			&event.ActorCode, &event.TaskRunCode, &event.TaskCode, &event.ActivityCode, &event.SourceIntentCode,
			&event.EventSequence, &event.FrameSequence, &event.EventType, &event.ExpirationDueWorldTimeUS,
			&completionSequence, &completionHash, &event.PreviousEventHash, &event.EventHash,
		); err != nil {
			return nil, err
		}
		if completionSequence.Valid {
			event.CompletionActivityEventSequence = completionSequence.Int64
		}
		if completionHash.Valid {
			event.CompletionActivityEventHash = completionHash.String
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime character task events: %w", err)
	}
	return events, nil
}

func validateCityRealtimeCharacterTaskHeadHistory(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
	head cityRealtimeCharacterTaskHead,
) error {
	if !cityRealtimeCharacterTaskHeadValid(head) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_head"})
	}
	events, err := loadCityRealtimeCharacterTaskEvents(ctx, queryer, worldID, head)
	if err != nil {
		return err
	}
	if int64(len(events)) != head.TaskRevision {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_event_count"})
	}
	genesisHash, err := cityRealtimeCharacterTaskChainGenesisHash(head)
	if err != nil {
		return err
	}
	previousHash := genesisHash
	for index, event := range events {
		if !cityRealtimeCharacterTaskEventValid(event) || event.ActorCode != head.ActorCode ||
			event.TaskRunCode != head.TaskRunCode || event.TaskCode != head.TaskCode ||
			event.ActivityCode != head.ActivityCode || event.SourceIntentCode != head.SourceIntentCode ||
			event.EventSequence != int64(index+1) || event.PreviousEventHash != previousHash ||
			event.ExpirationDueWorldTimeUS != head.ExpirationDueWorldTimeUS {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_event_chain"})
		}
		expectedHash, hashErr := cityRealtimeCharacterTaskEventHash(event)
		if hashErr != nil || expectedHash != event.EventHash {
			return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_event_hash"})
		}
		previousHash = event.EventHash
	}
	if len(events) == 0 || events[0].EventType != cityRealtimeCharacterTaskAcceptedEvent ||
		events[0].FrameSequence != head.AcceptedFrameSequence || previousHash != head.EventChainHash {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_history"})
	}
	last := events[len(events)-1]
	if head.TaskStatus == cityRealtimeCharacterTaskCompleted &&
		(last.EventType != cityRealtimeCharacterTaskCompletedEvent ||
			last.CompletionActivityEventSequence != head.CompletionActivityEventSequence ||
			last.CompletionActivityEventHash != head.CompletionActivityEventHash) {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_completion"})
	}
	if head.TaskStatus == cityRealtimeCharacterTaskExpired && last.EventType != cityRealtimeCharacterTaskExpiredEvent {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_expiry"})
	}
	return nil
}

func insertCityRealtimeCharacterTaskHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	head cityRealtimeCharacterTaskHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTaskHeadValid(head) ||
		head.TaskRevision != 1 || head.TaskStatus != cityRealtimeCharacterTaskAccepted {
		return ErrCityInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_task_heads
    (world_id, actor_code, task_run_code, task_code, activity_code, source_intent_code,
     task_revision, task_status, accepted_frame_sequence, expiration_due_world_time_us,
     completion_activity_event_sequence, completion_activity_event_hash,
     last_frame_sequence, event_chain_hash, state_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, '{}'::jsonb)`,
		worldID, head.ActorCode, head.TaskRunCode, head.TaskCode, head.ActivityCode, head.SourceIntentCode,
		head.TaskRevision, head.TaskStatus, head.AcceptedFrameSequence, head.ExpirationDueWorldTimeUS,
		head.LastFrameSequence, head.EventChainHash, head.StateHash,
	); err != nil {
		return fmt.Errorf("insert realtime character task head: %w", err)
	}
	return nil
}

func insertCityRealtimeCharacterTaskEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	event cityRealtimeCharacterTaskEvent,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTaskEventValid(event) {
		return ErrCityInvalidInput
	}
	var completionSequence any
	var completionHash any
	if event.CompletionActivityEventSequence > 0 {
		completionSequence = event.CompletionActivityEventSequence
		completionHash = event.CompletionActivityEventHash
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO city_realtime_character_task_events
    (world_id, actor_code, task_run_code, task_code, activity_code, source_intent_code,
     event_sequence, frame_sequence, event_type, expiration_due_world_time_us,
     completion_activity_event_sequence, completion_activity_event_hash,
     previous_event_hash, event_hash, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)`,
		worldID, event.ActorCode, event.TaskRunCode, event.TaskCode, event.ActivityCode, event.SourceIntentCode,
		event.EventSequence, event.FrameSequence, event.EventType, event.ExpirationDueWorldTimeUS,
		completionSequence, completionHash, event.PreviousEventHash, event.EventHash,
	); err != nil {
		return fmt.Errorf("append realtime character task event: %w", err)
	}
	return nil
}

func updateCityRealtimeCharacterTaskHead(
	ctx context.Context,
	tx *sql.Tx,
	worldID int64,
	previous, next cityRealtimeCharacterTaskHead,
) error {
	if tx == nil || worldID <= 0 || !cityRealtimeCharacterTaskHeadValid(previous) ||
		!cityRealtimeCharacterTaskHeadValid(next) || previous.ActorCode != next.ActorCode ||
		previous.TaskRunCode != next.TaskRunCode || next.TaskRevision != previous.TaskRevision+1 {
		return ErrCityInvalidInput
	}
	var completionSequence any
	var completionHash any
	if next.CompletionActivityEventSequence > 0 {
		completionSequence = next.CompletionActivityEventSequence
		completionHash = next.CompletionActivityEventHash
	}
	result, err := tx.ExecContext(ctx, `
UPDATE city_realtime_character_task_heads
SET task_revision = $4, task_status = $5,
    completion_activity_event_sequence = $6, completion_activity_event_hash = $7,
    last_frame_sequence = $8, event_chain_hash = $9, state_hash = $10, updated_at = NOW()
WHERE world_id = $1 AND actor_code = $2 AND task_run_code = $3
  AND task_revision = $11 AND task_status = $12 AND last_frame_sequence = $13
  AND event_chain_hash = $14 AND state_hash = $15`,
		worldID, next.ActorCode, next.TaskRunCode, next.TaskRevision, next.TaskStatus,
		completionSequence, completionHash, next.LastFrameSequence, next.EventChainHash, next.StateHash,
		previous.TaskRevision, previous.TaskStatus, previous.LastFrameSequence, previous.EventChainHash, previous.StateHash,
	)
	if err != nil {
		return fmt.Errorf("advance realtime character task head: %w", err)
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("check realtime character task update: %w", rowsErr)
	} else if rows != 1 {
		return ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_revision"})
	}
	return nil
}

func cityRealtimeCharacterTaskExpirationDueWorldTime(
	currentWorldTimeUS int64,
	definition cityRealtimeCharacterTaskDefinition,
) (int64, error) {
	if currentWorldTimeUS < 0 || !cityRealtimeCharacterTaskDefinitionValid(definition) ||
		currentWorldTimeUS > cityRealtimeMaximumWorldTimeUS-definition.ExpirationDelayUS {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_due_time"})
	}
	dueWorldTimeUS := currentWorldTimeUS + definition.ExpirationDelayUS
	if dueWorldTimeUS%cityRealtimeTimeQuantumUS != 0 {
		return 0, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_due_time"})
	}
	return dueWorldTimeUS, nil
}

func cityRealtimeCharacterTaskExpiryDedupKey(head cityRealtimeCharacterTaskHead) (string, error) {
	if !cityRealtimeCharacterTaskHeadValid(head) {
		return "", ErrCityInvalidInput
	}
	key := "task.expire." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-task-expiry-dedup-v1",
		head.ActorCode,
		head.TaskRunCode,
		head.SourceIntentCode,
		strconv.FormatInt(head.ExpirationDueWorldTimeUS, 10),
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_dedup"})
	}
	return key, nil
}

func cityRealtimeCharacterTaskAggregateKey(head cityRealtimeCharacterTaskHead) (string, error) {
	if !cityRealtimeCharacterTaskHeadValid(head) {
		return "", ErrCityInvalidInput
	}
	key := "task.aggregate." + cityOpenWorldPayloadHash([]byte(strings.Join([]string{
		"city-realtime-character-task-aggregate-v1",
		head.ActorCode,
		head.TaskRunCode,
	}, "\x1f")))
	if !cityRealtimeDueEventIdentifierValid(key, 160) {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_aggregate"})
	}
	return key, nil
}

func scheduleCityRealtimeCharacterTaskExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, createdFrameSequence int64,
	head cityRealtimeCharacterTaskHead,
) error {
	if tx == nil || worldID <= 0 || createdFrameSequence <= 0 ||
		!cityRealtimeCharacterTaskHeadValid(head) || head.TaskRevision != 1 ||
		head.TaskStatus != cityRealtimeCharacterTaskAccepted {
		return ErrCityInvalidInput
	}
	dedupKey, err := cityRealtimeCharacterTaskExpiryDedupKey(head)
	if err != nil {
		return err
	}
	aggregateKey, err := cityRealtimeCharacterTaskAggregateKey(head)
	if err != nil {
		return err
	}
	payload, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":     cityRealtimeCharacterTaskSchemaVersion,
		"actor_code":         head.ActorCode,
		"task_run_code":      head.TaskRunCode,
		"task_code":          head.TaskCode,
		"source_intent_code": head.SourceIntentCode,
	})
	if err != nil {
		return fmt.Errorf("canonicalize realtime character task expiry payload: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO city_due_events
    (world_id, event_type, schema_version, due_world_time_us, temporal_phase,
     priority, aggregate_type, aggregate_key, dedup_key, source_kind,
     source_reference, payload, payload_hash, expected_version, status,
     created_frame_sequence)
VALUES ($1, $2, 1, $3, 'rule_effect', $4, 'realtime_character_task', $5, $6, 'system',
        'realtime_character_task', $7::jsonb, $8, 1, 'pending', $9)`,
		worldID, cityRealtimeDueEventTypeCharacterTaskExpire, head.ExpirationDueWorldTimeUS,
		cityRealtimeCharacterTaskExpiryPriority, aggregateKey, dedupKey, []byte(payload), payloadHash,
		createdFrameSequence,
	); err != nil {
		return fmt.Errorf("schedule realtime character task expiry: %w", err)
	}
	return nil
}

func decodeCityRealtimeCharacterTaskExpiryDuePayload(
	event cityRealtimeDueEventRecord,
) (cityRealtimeCharacterTaskExpiryDuePayload, bool) {
	payload := cityRealtimeCharacterTaskExpiryDuePayload{}
	if err := decodeStrictCityObject(event.Payload, &payload); err != nil ||
		payload.SchemaVersion != cityRealtimeCharacterTaskSchemaVersion ||
		!cityRealtimePlayerActorCodeValid(payload.ActorCode) ||
		!cityRealtimeCharacterTaskRunCodeValid(payload.TaskRunCode) ||
		!cityRealtimeCharacterTaskCodeValid(payload.TaskCode) ||
		!cityRealtimeAgentIdentifierValid(payload.SourceIntentCode, 96) {
		return cityRealtimeCharacterTaskExpiryDuePayload{}, false
	}
	_, payloadHash, err := cityRealtimeCanonicalJSONObject(map[string]any{
		"schema_version":     payload.SchemaVersion,
		"actor_code":         payload.ActorCode,
		"task_run_code":      payload.TaskRunCode,
		"task_code":          payload.TaskCode,
		"source_intent_code": payload.SourceIntentCode,
	})
	if err != nil || payloadHash != event.PayloadHash {
		return cityRealtimeCharacterTaskExpiryDuePayload{}, false
	}
	return payload, true
}

func applyCityRealtimeCharacterTaskExpiryDueEvent(
	ctx context.Context,
	tx *sql.Tx,
	worldID, frameSequence int64,
	event cityRealtimeDueEventRecord,
) (bool, error) {
	if tx == nil || worldID <= 0 || frameSequence <= 0 ||
		event.EventType != cityRealtimeDueEventTypeCharacterTaskExpire ||
		event.SchemaVersion != cityRealtimeCharacterTaskSchemaVersion ||
		event.SourceKind != cityRealtimeDueEventSourceKindSystem || event.TemporalPhase != "rule_effect" ||
		event.AggregateType != "realtime_character_task" || event.SourceReference != "realtime_character_task" ||
		event.ExpectedVersion == nil || *event.ExpectedVersion != 1 {
		return false, nil
	}
	payload, validPayload := decodeCityRealtimeCharacterTaskExpiryDuePayload(event)
	if !validPayload {
		return false, nil
	}
	head, found, err := loadCityRealtimeCharacterTaskHead(
		ctx, tx, worldID, payload.ActorCode, payload.TaskRunCode, true,
	)
	if err != nil {
		return false, err
	}
	if !found || head.TaskRevision != 1 || head.TaskStatus != cityRealtimeCharacterTaskAccepted ||
		head.TaskCode != payload.TaskCode || head.SourceIntentCode != payload.SourceIntentCode ||
		head.ExpirationDueWorldTimeUS != event.DueWorldTimeUS {
		return false, nil
	}
	expectedAggregateKey, aggregateErr := cityRealtimeCharacterTaskAggregateKey(head)
	expectedDedupKey, dedupErr := cityRealtimeCharacterTaskExpiryDedupKey(head)
	if aggregateErr != nil || dedupErr != nil || event.AggregateKey != expectedAggregateKey || event.DedupKey != expectedDedupKey {
		return false, nil
	}
	runtime, err := loadCityRealtimeCharacterTaskRuntime(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	if runtime == nil {
		return false, nil
	}
	if err = validateCityRealtimeCharacterTaskHeadHistory(ctx, tx, worldID, head); err != nil {
		return false, err
	}
	nextHead, taskEvent, transitionErr := cityRealtimeCharacterExpireTask(head, frameSequence, event.DueWorldTimeUS)
	if transitionErr != nil {
		return false, transitionErr
	}
	if err = enableCityRealtimeCharacterMutationGates(ctx, tx, worldID, frameSequence, true); err != nil {
		return false, err
	}
	if err = enableCityRealtimeCharacterTaskMutationGate(ctx, tx, worldID, frameSequence, event.DueWorldTimeUS); err != nil {
		return false, err
	}
	if err = insertCityRealtimeCharacterTaskEvent(ctx, tx, worldID, taskEvent); err != nil {
		return false, err
	}
	if err = updateCityRealtimeCharacterTaskHead(ctx, tx, worldID, head, nextHead); err != nil {
		return false, err
	}
	return true, nil
}

func cityRealtimeCharacterAvailableTaskCodes(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID, worldTimeUS int64,
	actorState cityRealtimeActorState,
	profile cityRealtimeCharacterProfile,
	lifeRuntime *cityRealtimeCharacterLifeRuntime,
	actorCode string,
	agentBinding cityRealtimeAgentPolicyBinding,
) ([]string, error) {
	if worldID <= 0 || worldTimeUS < 0 || !cityRealtimePlayerActorCodeValid(actorCode) ||
		actorState.ActorCode != actorCode || !cityRealtimeCharacterProfileMatchesRuntime(profile, lifeRuntime) ||
		!cityRealtimeAgentCharacterTaskRuntimeEnabled(agentBinding) {
		return nil, ErrCityInvalidInput
	}
	runtime, err := loadCityRealtimeCharacterTaskRuntime(ctx, queryer, worldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil || runtime.Binding.AgentBindingHash != agentBinding.BindingHash ||
		runtime.Binding.ActivityBindingHash != lifeRuntime.Binding.BindingHash {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_runtime"})
	}
	active, found, err := loadCityRealtimeCharacterActiveTask(ctx, queryer, worldID, actorCode, false)
	if err != nil {
		return nil, err
	}
	if found {
		if active.TaskStatus != cityRealtimeCharacterTaskAccepted {
			return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_active"})
		}
		return make([]string, 0), nil
	}
	availability, err := cityRealtimeCharacterActivityAvailability(
		ctx, queryer, worldID, worldTimeUS, actorState, profile, lifeRuntime,
	)
	if err != nil {
		return nil, err
	}
	activityAvailable := make(map[string]bool, len(availability))
	for _, item := range availability {
		activityAvailable[item.Code] = item.Available
	}
	codes := make([]string, 0, len(runtime.Definitions))
	for code, definition := range runtime.Definitions {
		if activityAvailable[definition.ActivityCode] {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	return codes, nil
}

func cityRealtimeAgentDecisionTaskCodeFromArguments(arguments map[string]any) (string, error) {
	if len(arguments) != 1 {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	rawCode, exists := arguments["task_code"]
	code, ok := rawCode.(string)
	code = strings.TrimSpace(code)
	if !exists || !ok || !cityRealtimeCharacterTaskCodeValid(code) {
		return "", ErrCityRealtimeAgentDecisionUnavailable.WithMetadata(map[string]string{"field": "action_arguments"})
	}
	return code, nil
}

func cityRealtimeAgentDecisionTaskCodeFromRawArguments(arguments json.RawMessage) (string, error) {
	decoded := make(map[string]any)
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_agent_intent_arguments"}).WithCause(err)
	}
	return cityRealtimeAgentDecisionTaskCodeFromArguments(decoded)
}

func loadCityRealtimeCharacterTaskHashState(
	ctx context.Context,
	queryer citySQLQueryer,
	worldID int64,
) (*cityRealtimeCharacterTaskHashState, error) {
	runtime, err := loadCityRealtimeCharacterTaskRuntime(ctx, queryer, worldID)
	if err != nil || runtime == nil {
		return nil, err
	}
	state := &cityRealtimeCharacterTaskHashState{
		SchemaVersion: cityRealtimeCharacterTaskSchemaVersion,
		Binding:       &runtime.Binding,
		Heads:         make([]cityRealtimeCharacterTaskHead, 0),
	}
	rows, err := queryer.QueryContext(ctx, `
SELECT actor_code, task_run_code, task_code, activity_code, source_intent_code,
       task_revision, task_status, accepted_frame_sequence, expiration_due_world_time_us,
       completion_activity_event_sequence, completion_activity_event_hash,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_task_heads
WHERE world_id = $1
ORDER BY actor_code ASC, task_run_code ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load realtime character task heads: %w", err)
	}
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterTaskHead(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		state.Heads = append(state.Heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character task heads: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character task heads: %w", err)
	}
	// A PostgreSQL connection cannot safely execute the event-chain query while
	// its head cursor is still active. First materialize the small canonical head
	// list, then validate every append-only event chain on the same transaction.
	for _, head := range state.Heads {
		if err = validateCityRealtimeCharacterTaskHeadHistory(ctx, queryer, worldID, head); err != nil {
			return nil, err
		}
	}
	if err = validateCityRealtimeCharacterTaskHashState(state); err != nil {
		return nil, ErrCitySimulationInvariant.WithMetadata(map[string]string{"field": "realtime_character_task_state"}).WithCause(err)
	}
	return state, nil
}

func validateCityRealtimeCharacterTaskHashState(state *cityRealtimeCharacterTaskHashState) error {
	if state == nil || state.SchemaVersion != cityRealtimeCharacterTaskSchemaVersion ||
		state.Binding == nil || state.Heads == nil || !cityRealtimeCharacterTaskBindingValid(*state.Binding) {
		return errors.New("invalid realtime character task hash state")
	}
	activeByActor := make(map[string]struct{})
	for index, head := range state.Heads {
		if !cityRealtimeCharacterTaskHeadValid(head) {
			return errors.New("invalid realtime character task head")
		}
		if index > 0 {
			previous := state.Heads[index-1]
			if previous.ActorCode > head.ActorCode ||
				(previous.ActorCode == head.ActorCode && previous.TaskRunCode >= head.TaskRunCode) {
				return errors.New("unordered realtime character task heads")
			}
		}
		if head.TaskStatus == cityRealtimeCharacterTaskAccepted {
			if _, duplicate := activeByActor[head.ActorCode]; duplicate {
				return errors.New("multiple active realtime character tasks")
			}
			activeByActor[head.ActorCode] = struct{}{}
		}
	}
	return nil
}

func (head cityRealtimeCharacterTaskHead) projection() CityRealtimeCharacterTask {
	projection := CityRealtimeCharacterTask{
		TaskRunCode:              head.TaskRunCode,
		TaskCode:                 head.TaskCode,
		ActivityCode:             head.ActivityCode,
		Status:                   head.TaskStatus,
		Revision:                 head.TaskRevision,
		AcceptedFrameSequence:    head.AcceptedFrameSequence,
		ExpirationDueWorldTimeUS: head.ExpirationDueWorldTimeUS,
		LastFrameSequence:        head.LastFrameSequence,
	}
	if head.TaskStatus == cityRealtimeCharacterTaskCompleted {
		projection.CompletedFrameSequence = head.LastFrameSequence
	}
	return projection
}

// ListRealtimeCharacterTasks returns the caller's own bounded task ledger.
// It deliberately has no mutation endpoint: acceptance only arrives through a
// sealed autonomous Agent intent, and completion only arrives through an exact
// later Agent activity reducer.
func (s *CityEconomyService) ListRealtimeCharacterTasks(
	ctx context.Context,
	input CityRealtimeCharacterTaskListInput,
) ([]CityRealtimeCharacterTask, error) {
	if input.UserID <= 0 || input.WorldID <= 0 {
		return nil, ErrCityInvalidInput
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, ErrCityInvalidInput.WithMetadata(map[string]string{"field": "limit"})
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin realtime character task read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockCityRealtimeCharacterWorld(ctx, tx, input.UserID, input.WorldID); err != nil {
		return nil, err
	}
	record, found, err := loadCityRealtimeOwnedCharacter(ctx, tx, input.WorldID, input.UserID, false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrCityRealtimeCharacterNotFound
	}
	runtime, err := loadCityRealtimeCharacterTaskRuntime(ctx, tx, input.WorldID)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return []CityRealtimeCharacterTask{}, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT actor_code, task_run_code, task_code, activity_code, source_intent_code,
       task_revision, task_status, accepted_frame_sequence, expiration_due_world_time_us,
       completion_activity_event_sequence, completion_activity_event_hash,
       last_frame_sequence, event_chain_hash, state_hash
FROM city_realtime_character_task_heads
WHERE world_id = $1 AND actor_code = $2
ORDER BY accepted_frame_sequence DESC, task_run_code DESC
LIMIT $3`, input.WorldID, record.identity.ActorCode, limit)
	if err != nil {
		return nil, fmt.Errorf("list realtime character tasks: %w", err)
	}
	heads := make([]cityRealtimeCharacterTaskHead, 0)
	for rows.Next() {
		head, scanErr := scanCityRealtimeCharacterTaskHead(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		heads = append(heads, head)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate realtime character tasks: %w", err)
	}
	if err = rows.Close(); err != nil {
		return nil, fmt.Errorf("close realtime character tasks: %w", err)
	}
	items := make([]CityRealtimeCharacterTask, 0, len(heads))
	for _, head := range heads {
		if err = validateCityRealtimeCharacterTaskHeadHistory(ctx, tx, input.WorldID, head); err != nil {
			return nil, err
		}
		items = append(items, head.projection())
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit realtime character task read: %w", err)
	}
	return items, nil
}
