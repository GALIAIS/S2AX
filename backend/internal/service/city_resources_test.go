package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCityResourceCommandsCanonicalizesIntent(t *testing.T) {
	expectedTick := int64(4)
	first, err := normalizeCityCommand(
		" RESOURCE.TRANSFER ",
		json.RawMessage(`{"from_entity_id":2,"to_entity_id":1,"from_district_code":" CENTRAL ","to_district_code":" North ","resource_code":" CONSUMER_GOODS ","quantity_units":25,"memo":"  shipment  "}`),
		&expectedTick,
	)
	require.NoError(t, err)
	second, err := normalizeCityCommand(
		CityCommandTypeResourceTransfer,
		json.RawMessage(`{"memo":"shipment","quantity_units":25,"resource_code":"consumer_goods","to_district_code":"north","from_district_code":"central","to_entity_id":1,"from_entity_id":2}`),
		&expectedTick,
	)
	require.NoError(t, err)
	require.Equal(t, first.fingerprint, second.fingerprint)
	require.JSONEq(t, `{"from_entity_id":2,"to_entity_id":1,"from_district_code":"central","to_district_code":"north","resource_code":"consumer_goods","quantity_units":25,"memo":"shipment"}`, string(first.payload))

	produce, err := normalizeCityCommand(
		CityCommandTypeResourceProduce,
		json.RawMessage(`{"firm_entity_id":2,"district_code":"CENTRAL","recipe_code":"BASIC_GOODS","batch_count":10}`),
		&expectedTick,
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"firm_entity_id":2,"district_code":"central","recipe_code":"basic_goods","batch_count":10}`, string(produce.payload))
}

func TestNormalizeCityResourceCommandsRejectsInvalidQuantitiesAndScopes(t *testing.T) {
	expectedTick := int64(4)
	for _, testCase := range []struct {
		commandType string
		payload     string
	}{
		{CityCommandTypeResourceTransfer, `{"from_entity_id":1,"to_entity_id":1,"from_district_code":"central","to_district_code":"central","resource_code":"food","quantity_units":1}`},
		{CityCommandTypeResourceTransfer, `{"from_entity_id":1,"to_entity_id":2,"from_district_code":"central","to_district_code":"north","resource_code":"food","quantity_units":0}`},
		{CityCommandTypeResourceConsume, `{"entity_id":1,"district_code":"central","resource_code":"food","quantity_units":1,"purpose":"   "}`},
		{CityCommandTypeResourceProduce, `{"firm_entity_id":2,"district_code":"central","recipe_code":"basic_goods","batch_count":1000001}`},
		{CityCommandTypeResourceProduce, `{"firm_entity_id":2,"district_code":"central","recipe_code":"basic_goods","batch_count":1,"unexpected":true}`},
		{CityCommandTypeResourceProduce, `{"firm_entity_id":2,"district_code":"central","recipe_code":"basic_goods","batch_count":1,"batch_count":2}`},
	} {
		_, err := normalizeCityCommand(testCase.commandType, json.RawMessage(testCase.payload), &expectedTick)
		require.ErrorIs(t, err, ErrCityInvalidInput, testCase.payload)
	}
}
