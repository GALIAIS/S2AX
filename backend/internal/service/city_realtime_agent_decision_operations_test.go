package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListRealtimeAgentDecisionQueueRequiresAdministrator(t *testing.T) {
	service := &CityEconomyService{}
	_, err := service.ListRealtimeAgentDecisionQueue(context.Background(), CityRealtimeAgentDecisionQueueListInput{WorldID: 7})
	require.ErrorIs(t, err, ErrCityManagementRequired)
}

func TestListRealtimeAgentDecisionQueueValidatesBoundedFilters(t *testing.T) {
	service := &CityEconomyService{}
	adminCtx := WithCitySystemAdministrator(context.Background())
	for _, input := range []CityRealtimeAgentDecisionQueueListInput{
		{WorldID: 0},
		{WorldID: 7, Status: "unexpected"},
		{WorldID: 7, Limit: cityRealtimeAgentDecisionQueueMaximumLimit + 1},
		{WorldID: 7, BeforeCursor: "not-a-cursor"},
	} {
		_, err := service.ListRealtimeAgentDecisionQueue(adminCtx, input)
		require.ErrorIs(t, err, ErrCityInvalidInput)
	}
}

func TestListRealtimeAgentDecisionQueueReturnsRedactedStablePage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	created := time.Date(2026, time.July, 23, 8, 9, 10, 123456000, time.UTC)
	updated := created.Add(time.Second)
	retry := created.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT simulation_version FROM city_worlds`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"simulation_version"}).AddRow(CitySimulationVersionRealtimeV2))
	mock.ExpectQuery(`SELECT request\.request_code, instance\.definition_code`).
		WithArgs(int64(7), 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_code", "definition_code", "request_status", "outbox_status", "attempt_count", "retry_not_before",
			"model_profile_code", "model_profile_version", "last_attempt_status", "last_error_code",
			"dead_letter_status", "dead_letter_reason_code", "dead_letter_quarantined_at", "created_at", "updated_at",
		}).
			AddRow("adr.queue.new", "character.npc", "queued", "queued", 1, retry,
				"system.fake.deterministic", 1, "failed", "provider_timeout", "quarantined", "operator_review", created.Add(-25*time.Hour), created, updated).
			AddRow("adr.queue.old", "character.user", "leased", "leased", 2, nil,
				nil, nil, "started", nil, nil, nil, nil, created.Add(-time.Second), updated))
	mock.ExpectCommit()

	service := &CityEconomyService{db: db}
	page, err := service.ListRealtimeAgentDecisionQueue(WithCitySystemAdministrator(context.Background()), CityRealtimeAgentDecisionQueueListInput{
		WorldID: 7,
		Limit:   1,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	item := page.Items[0]
	require.Equal(t, "adr.queue.new", item.RequestCode)
	require.Equal(t, "character.npc", item.AgentDefinitionCode)
	require.Equal(t, "queued", item.RequestStatus)
	require.Equal(t, "queued", item.OutboxStatus)
	require.Equal(t, 1, item.AttemptCount)
	require.Equal(t, "system.fake.deterministic", *item.ModelProfileCode)
	require.Equal(t, 1, *item.ModelProfileVersion)
	require.Equal(t, "failed", *item.LastAttemptStatus)
	require.Equal(t, "provider_timeout", *item.LastErrorCode)
	require.NotNil(t, item.RetryNotBefore)
	require.Equal(t, "quarantined", *item.DeadLetterStatus)
	require.Equal(t, "operator_review", *item.DeadLetterReasonCode)
	require.NotNil(t, item.DeadLetterQuarantinedAt)
	require.Equal(t, created.Add(-25*time.Hour), *item.DeadLetterQuarantinedAt)
	require.NotNil(t, page.NextCursor)
	parsed, parseErr := parseCityRealtimeAgentDecisionQueueCursor(*page.NextCursor)
	require.NoError(t, parseErr)
	require.Equal(t, item.RequestCode, parsed.RequestCode)
	require.Equal(t, item.CreatedAt, parsed.CreatedAt)

}

func TestRealtimeAgentDecisionDeadLetterRequiresAdministrator(t *testing.T) {
	service := &CityEconomyService{}
	_, err := service.QuarantineRealtimeAgentDecision(context.Background(), CityRealtimeAgentDecisionDeadLetterInput{
		AdministratorUserID: 7,
		WorldID:             8,
		RequestCode:         "adr.queue.dead-letter",
		ReasonCode:          cityRealtimeAgentDecisionDeadLetterReasonOperatorReview,
	})
	require.ErrorIs(t, err, ErrCityManagementRequired)
	_, err = service.ReleaseRealtimeAgentDecisionDeadLetter(context.Background(), CityRealtimeAgentDecisionDeadLetterReleaseInput{
		AdministratorUserID: 7,
		WorldID:             8,
		RequestCode:         "adr.queue.dead-letter",
	})
	require.ErrorIs(t, err, ErrCityManagementRequired)
}

func TestRealtimeAgentDecisionDeadLetterValidatesFiniteReason(t *testing.T) {
	service := &CityEconomyService{}
	_, err := service.QuarantineRealtimeAgentDecision(WithCitySystemAdministrator(context.Background()), CityRealtimeAgentDecisionDeadLetterInput{
		AdministratorUserID: 7,
		WorldID:             8,
		RequestCode:         "adr.queue.dead-letter",
		ReasonCode:          "arbitrary free text is forbidden",
	})
	require.ErrorIs(t, err, ErrCityInvalidInput)
}

func TestListRealtimeAgentDecisionDeadLetterEventsReturnsRedactedStablePage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	created := time.Date(2026, time.July, 23, 9, 10, 11, 123456000, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT simulation_version FROM city_worlds`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"simulation_version"}).AddRow(CitySimulationVersionRealtimeV2))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(int64(7), "adr.queue.dead-letter").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT event_id, event_type, reason_code, actor_user_id, created_at`).
		WithArgs(int64(7), "adr.queue.dead-letter", 2).
		WillReturnRows(sqlmock.NewRows([]string{"event_id", "event_type", "reason_code", "actor_user_id", "created_at"}).
			AddRow(int64(12), "released", "operator_release", int64(41), created).
			AddRow(int64(11), "quarantined", "operator_review", int64(40), created.Add(-time.Second)))
	mock.ExpectCommit()

	service := &CityEconomyService{db: db}
	page, err := service.ListRealtimeAgentDecisionDeadLetterEvents(WithCitySystemAdministrator(context.Background()), CityRealtimeAgentDecisionDeadLetterEventListInput{
		WorldID:     7,
		RequestCode: "adr.queue.dead-letter",
		Limit:       1,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, int64(12), page.Items[0].EventID)
	require.Equal(t, "released", page.Items[0].EventType)
	require.Equal(t, "operator_release", page.Items[0].ReasonCode)
	require.Equal(t, int64(41), page.Items[0].ActorUserID)
	require.Equal(t, int64(12), *page.NextBeforeEventID)
}

func TestListRealtimeAgentDecisionDeadLetterEventsRequiresAdministratorAndValidatesInput(t *testing.T) {
	service := &CityEconomyService{}
	_, err := service.ListRealtimeAgentDecisionDeadLetterEvents(context.Background(), CityRealtimeAgentDecisionDeadLetterEventListInput{
		WorldID: 7, RequestCode: "adr.queue.dead-letter",
	})
	require.ErrorIs(t, err, ErrCityManagementRequired)
	_, err = service.ListRealtimeAgentDecisionDeadLetterEvents(WithCitySystemAdministrator(context.Background()), CityRealtimeAgentDecisionDeadLetterEventListInput{
		WorldID: 7, RequestCode: "adr.queue.dead-letter", BeforeEventID: -1,
	})
	require.ErrorIs(t, err, ErrCityInvalidInput)
}
