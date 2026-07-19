package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIsCityPlayerCommand(t *testing.T) {
	for _, commandType := range []string{
		service.CityCommandTypeActorCreate,
		service.CityCommandTypeActorActivityPerform,
		service.CityCommandTypeActorRoleTransition,
		service.CityCommandTypeActorLocationMove,
		service.CityCommandTypePortalStateTransition,
		service.CityCommandTypeActorNavigationIntentSet,
		service.CityCommandTypeActorNavigationIntentCancel,
		service.CityCommandTypeOpenWorldActorCreate,
		service.CityCommandTypeOpenWorldActorActivityPerform,
		service.CityCommandTypeOpenWorldActorRoleTransition,
		service.CityCommandTypeOpenWorldActorMove,
		service.CityCommandTypeOpenWorldActorPortalUse,
		service.CityCommandTypeOpenWorldActorNavigationSet,
		service.CityCommandTypeOpenWorldActorNavigationCancel,
	} {
		require.Truef(t, isCityPlayerCommand(commandType), "expected %q to remain playable", commandType)
	}

	for _, commandType := range []string{
		service.CityCommandTypeActorControlGrant,
		service.CityCommandTypeActorControlRevoke,
		service.CityCommandTypePortalAccessConfigure,
		service.CityCommandTypeDevelopmentSubmit,
		service.CityCommandTypeOpenWorldSectorMaterialize,
	} {
		require.Falsef(t, isCityPlayerCommand(commandType), "expected %q to require an administrator", commandType)
	}
}
