package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchOpenAIAccountModelsOAuthPopulatesPickerFields(t *testing.T) {
	_, calls := newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"new-oauth-model"},{"slug":"gpt-6-astra"}]}`)
	gateway := &OpenAIGatewayService{}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := newCodexModelsTestAccount()
	account.Extra = map[string]any{
		OpenAIImageModelExtraKey:  "gpt-image-2.5-flare",
		OpenAIImageModelsExtraKey: []string{"gpt-image-2.5-flare", "custom-image-model"},
	}
	ctx := context.Background()

	before, err := gateway.FetchOpenAIModelsList(ctx, account)
	require.NoError(t, err)
	models, err := svc.FetchOpenAIAccountModels(ctx, account)
	require.NoError(t, err)
	require.Len(t, models, 4)
	for i, id := range []string{"new-oauth-model", "gpt-6-astra", "gpt-image-2.5-flare", "custom-image-model"} {
		require.Equal(t, id, models[i].ID)
	}
	for i, displayName := range []string{"new-oauth-model", "gpt-6-astra", "gpt-image-2.5-flare", "custom-image-model"} {
		require.Equal(t, displayName, models[i].DisplayName)
	}
	require.Equal(t, "model", models[0].Type)
	require.Equal(t, "model", models[1].Type)
	require.Equal(t, "image_generation", models[2].Type)
	require.Equal(t, "image_generation", models[3].Type)
	after, err := gateway.FetchOpenAIModelsList(ctx, account)
	require.NoError(t, err)
	require.Equal(t, before.Body, after.Body, "picker fields must not change the shared catalog")
	require.NotContains(t, string(after.Body), "display_name")
	require.EqualValues(t, 1, calls.Load(), "picker must reuse the shared discovery cache")
}

func TestFetchOpenAIAccountModelsAPIKeyPopulatesPickerFields(t *testing.T) {
	gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		return ordinaryModelsUpstreamResponse(`{"data":[
			{"id":"new-api-model","owned_by":"provider","created":123},
			{"id":"blank-label","display_name":"  ","type":""},
			{"id":"named-model","display_name":"Provider Model","type":"model"}
		]}`), nil
	}})
	svc := &AccountTestService{openaiGatewayService: gateway}
	models, err := svc.FetchOpenAIAccountModels(context.Background(), newCodexModelsAPIKeyTestAccount("https://models.example/v1"))
	require.NoError(t, err)
	require.Len(t, models, 3)
	for i, name := range []string{"new-api-model", "blank-label", "Provider Model"} {
		require.Equal(t, name, models[i].DisplayName)
		require.Equal(t, "model", models[i].Type)
	}
	require.Equal(t, "provider", models[0].OwnedBy)
	require.EqualValues(t, 123, models[0].Created)
	require.Equal(t, "named-model", models[2].ID)
}

func TestFetchOpenAIAccountModelsPreservesEmptyCatalog(t *testing.T) {
	gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		return ordinaryModelsUpstreamResponse(`{"data":[]}`), nil
	}})
	svc := &AccountTestService{openaiGatewayService: gateway}
	models, err := svc.FetchOpenAIAccountModels(context.Background(), newCodexModelsAPIKeyTestAccount("https://models.example/v1"))
	require.NoError(t, err)
	require.Empty(t, models, "an empty upstream catalog must not become a static model list")
}

func TestOpenAIImageGenerationExtraNormalizesConfiguredModels(t *testing.T) {
	extra := map[string]any{
		OpenAIImageModelExtraKey:     " custom-image-model ",
		OpenAIImageModelsExtraKey:    []any{"custom-image-model", " second-image-model ", "CUSTOM-IMAGE-MODEL"},
		OpenAIImageTextModelExtraKey: "gpt-5.4",
	}

	normalized, err := normalizeOpenAIImageGenerationExtra(PlatformOpenAI, extra)
	require.NoError(t, err)
	require.Equal(t, "custom-image-model", normalized[OpenAIImageModelExtraKey])
	require.Equal(t, []string{"custom-image-model", "second-image-model"}, normalized[OpenAIImageModelsExtraKey])
	require.Equal(t, "gpt-5.4", normalized[OpenAIImageTextModelExtraKey])

	_, err = normalizeOpenAIImageGenerationExtra(PlatformOpenAI, map[string]any{
		OpenAIImageModelsExtraKey:    []any{"custom-image-model"},
		OpenAIImageTextModelExtraKey: "custom-image-model",
	})
	require.Error(t, err)

	_, err = normalizeOpenAIImageGenerationExtra(PlatformOpenAI, map[string]any{
		OpenAIImageModelsExtraKey: []any{"custom-image-model", 42},
	})
	require.Error(t, err)
}
