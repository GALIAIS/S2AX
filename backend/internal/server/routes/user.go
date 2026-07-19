package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	rateLimitMiddleware "github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterUserRoutes 注册用户相关路由（需要认证）
func RegisterUserRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	redisClient *redis.Client,
) {
	// 游戏、任务和其他服务使用独立的 HMAC 凭据，不经过用户 JWT 中间件。
	rateLimiter := rateLimitMiddleware.NewRateLimiter(redisClient)
	v1.POST("/integrations/virtual-currency/mutations", rateLimiter.LimitWithOptions("virtual-currency-integration", 120, time.Minute, rateLimitMiddleware.RateLimitOptions{
		FailureMode: rateLimitMiddleware.RateLimitFailClose,
	}), h.VirtualCurrencyIntegration.Execute)

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	// 用户管理面变更类操作入审计（含 TOTP 启用/禁用、step-up 验证、密码修改等安全事件）
	authenticated.Use(gin.HandlerFunc(auditLog))
	{
		// 服务端优先的 IP 归属解析；浏览器仅在本地数据不可用时回退兼容查询。
		ipGeolocation := authenticated.Group("/ip-geolocation")
		{
			ipGeolocation.POST("/lookup", h.IPGeolocation.Lookup)
		}

		// 用户仅可查看由管理员分配给自己的安全账号摘要，不能操作租约。
		accountAllocations := authenticated.Group("/account-allocations")
		{
			accountAllocations.GET("", h.AccountAllocation.ListMine)
		}

		// 用户接口
		user := authenticated.Group("/user")
		{
			user.GET("/profile", h.User.GetProfile)
			user.PUT("/password", h.User.ChangePassword)
			user.PUT("", h.User.UpdateProfile)
			user.GET("/aff", h.User.GetAffiliate)
			user.POST("/aff/transfer", h.User.TransferAffiliateQuota)
			user.POST("/account-bindings/email/send-code", h.User.SendEmailBindingCode)
			user.POST("/account-bindings/email", h.User.BindEmailIdentity)
			user.DELETE("/account-bindings/:provider", h.User.UnbindIdentity)
			user.POST("/auth-identities/bind/start", h.User.StartIdentityBinding)
			user.GET("/api-keys/:id/usage/daily", h.Usage.GetMyAPIKeyDailyUsage)
			user.GET("/platform-quotas", h.User.GetMyPlatformQuotas)

			// 通知邮箱管理
			notifyEmail := user.Group("/notify-email")
			{
				notifyEmail.POST("/send-code", h.User.SendNotifyEmailCode)
				notifyEmail.POST("/verify", h.User.VerifyNotifyEmail)
				notifyEmail.PUT("/toggle", h.User.ToggleNotifyEmail)
				notifyEmail.DELETE("", h.User.RemoveNotifyEmail)
			}

			// TOTP 双因素认证
			totp := user.Group("/totp")
			{
				totp.GET("/status", h.Totp.GetStatus)
				totp.GET("/verification-method", h.Totp.GetVerificationMethod)
				totp.POST("/send-code", h.Totp.SendVerifyCode)
				totp.POST("/setup", h.Totp.InitiateSetup)
				totp.POST("/enable", h.Totp.Enable)
				totp.POST("/disable", h.Totp.Disable)
				// 敏感操作二次验证：授予当前会话一段时间的 step-up 权限
				totp.POST("/step-up", h.Totp.StepUp)
			}
		}

		// API Key管理
		keys := authenticated.Group("/keys")
		{
			keys.GET("", h.APIKey.List)
			keys.GET("/:id", h.APIKey.GetByID)
			keys.POST("", h.APIKey.Create)
			keys.PUT("/:id", h.APIKey.Update)
			keys.DELETE("/:id", h.APIKey.Delete)
		}

		// 用户可用分组（非管理员接口）
		groups := authenticated.Group("/groups")
		{
			groups.GET("/available", h.APIKey.GetAvailableGroups)
			groups.GET("/rates", h.APIKey.GetUserGroupRates)
		}

		// 用户虚拟货币资产
		currencies := authenticated.Group("/user/currencies")
		{
			currencies.GET("", h.VirtualCurrency.ListWallets)
			currencies.GET("/holds", h.VirtualCurrency.ListHolds)
			currencies.POST("/:code/holds", h.VirtualCurrency.Reserve)
			currencies.POST("/holds/:id/commit", h.VirtualCurrency.CommitHold)
			currencies.POST("/holds/:id/release", h.VirtualCurrency.ReleaseHold)
			currencies.GET("/:code/ledger", h.VirtualCurrency.ListLedger)
			currencies.POST("/:code/spend", h.VirtualCurrency.Spend)
		}

		// 城市模拟基础域
		citySpatial := authenticated.Group("/city/spatial")
		citySpatial.Use(middleware.CitySimulationGuard(settingService))
		{
			citySpatial.GET("/rule-sets", h.CityEconomy.ListSpatialRuleSets)
			citySpatial.GET("/rule-sets/:rule_set_id", h.CityEconomy.GetSpatialRuleSet)
		}
		cityOpenWorld := authenticated.Group("/city/open-world")
		cityOpenWorld.Use(middleware.CitySimulationGuard(settingService))
		{
			cityOpenWorld.GET("/styles", h.CityEconomy.ListOpenWorldStyleProfiles)
			cityOpenWorld.GET("/styles/:profile_id", h.CityEconomy.GetOpenWorldStyleProfile)
		}

		cityWorlds := authenticated.Group("/city/worlds")
		cityWorlds.Use(middleware.CitySimulationGuard(settingService), middleware.RequestBodyLimit(16<<10))
		{
			cityWorlds.GET("", h.CityEconomy.ListWorlds)
			cityWorlds.POST("", middleware.AdminOnly(), h.CityEconomy.CreateWorld)
			cityWorlds.GET("/:world_id", h.CityEconomy.GetWorld)
			cityWorlds.GET("/:world_id/members", h.CityEconomy.ListWorldMembers)
			cityWorlds.POST("/:world_id/members", middleware.AdminOnly(), h.CityEconomy.AddWorldMember)
			cityWorlds.PATCH("/:world_id/members/:user_id", middleware.AdminOnly(), h.CityEconomy.UpdateWorldMember)
			cityWorlds.GET("/:world_id/state", h.CityEconomy.GetPhysicalState)
			cityWorlds.GET("/:world_id/calendar", h.CityEconomy.GetCalendarState)
			cityWorlds.GET("/:world_id/population", h.CityEconomy.GetPopulationState)
			cityWorlds.GET("/:world_id/markets", h.CityEconomy.GetMarketOverview)
			cityWorlds.GET("/:world_id/services/catalog", h.CityEconomy.GetCityServiceCatalog)
			cityWorlds.GET("/:world_id/services/facilities", h.CityEconomy.ListCityServiceFacilities)
			cityWorlds.GET("/:world_id/services/demands", h.CityEconomy.ListCityServiceDemands)
			cityWorlds.GET("/:world_id/services/connections", h.CityEconomy.ListCityServiceConnections)
			cityWorlds.GET("/:world_id/services/settlements", h.CityEconomy.ListCityServiceSettlements)
			cityWorlds.GET("/:world_id/services/lifecycle/catalog", h.CityEconomy.GetCityFacilityLifecycleCatalog)
			cityWorlds.GET("/:world_id/services/lifecycle/facilities", h.CityEconomy.ListCityFacilityLifecycleStates)
			cityWorlds.GET("/:world_id/services/lifecycle/operations", h.CityEconomy.ListCityFacilityOperations)
			cityWorlds.GET("/:world_id/services/lifecycle/staffing", h.CityEconomy.ListCityFacilityStaffAssignments)
			cityWorlds.GET("/:world_id/services/lifecycle/incidents", h.CityEconomy.ListCityFacilityIncidents)
			cityWorlds.GET("/:world_id/services/lifecycle/budget-movements", h.CityEconomy.ListCityFacilityBudgetMovements)
			cityWorlds.GET("/:world_id/services/lifecycle/facts", h.CityEconomy.ListCityFacilityLifecycleFacts)
			cityWorlds.GET("/:world_id/services/networks/catalog", h.CityEconomy.GetCityPhysicalNetworkCatalog)
			cityWorlds.GET("/:world_id/services/networks", h.CityEconomy.ListCityPhysicalNetworks)
			cityWorlds.GET("/:world_id/services/networks/nodes", h.CityEconomy.ListCityPhysicalNetworkNodes)
			cityWorlds.GET("/:world_id/services/networks/edges", h.CityEconomy.ListCityPhysicalNetworkEdges)
			cityWorlds.GET("/:world_id/services/networks/flows", h.CityEconomy.ListCityPhysicalNetworkFlows)
			cityWorlds.GET("/:world_id/services/networks/facts", h.CityEconomy.ListCityPhysicalNetworkFacts)
			cityWorlds.GET("/:world_id/services/networks/diagnostics", h.CityEconomy.GetCityPhysicalNetworkDiagnostics)
			cityWorlds.GET("/:world_id/spatial/ruleset", h.CityEconomy.GetWorldSpatialRuleSet)
			cityWorlds.GET("/:world_id/spatial/overmap", h.CityEconomy.GetOvermap)
			cityWorlds.GET("/:world_id/open-world/generation", h.CityEconomy.GetOpenWorldGeneration)
			cityWorlds.GET("/:world_id/open-world/verification", h.CityEconomy.GetOpenWorldVerification)
			cityWorlds.GET("/:world_id/open-world/map", h.CityEconomy.GetOpenWorldMap)
			cityWorlds.GET("/:world_id/open-world/buildings/:building_code/portals", h.CityEconomy.ListOpenWorldBuildingPortals)
			cityWorlds.GET("/:world_id/open-world/buildings/:building_code/interiors/:floor_index", h.CityEconomy.GetOpenWorldBuildingInterior)
			cityWorlds.GET("/:world_id/land", h.CityEconomy.GetLandState)
			cityWorlds.GET("/:world_id/development", h.CityEconomy.GetDevelopmentState)
			cityWorlds.GET("/:world_id/enterprise-locations", h.CityEconomy.GetEnterpriseLocationState)
			cityWorlds.GET("/:world_id/runtime/catalog", h.CityEconomy.GetWorldRuntimeCatalog)
			cityWorlds.GET("/:world_id/runtime/actors", h.CityEconomy.ListWorldActors)
			cityWorlds.GET("/:world_id/runtime/actors/:actor_code", h.CityEconomy.GetWorldActorState)
			cityWorlds.GET("/:world_id/runtime/actors/:actor_code/roles", h.CityEconomy.GetWorldActorRoleOptions)
			cityWorlds.GET("/:world_id/runtime/rules", h.CityEconomy.ListWorldRules)
			cityWorlds.GET("/:world_id/runtime/cases", h.CityEconomy.ListWorldRuleCases)
			cityWorlds.GET("/:world_id/spatial/chunks", h.CityEconomy.ListMapChunks)
			cityWorlds.GET("/:world_id/spatial/chunks/:chunk_x/:chunk_y/:z", h.CityEconomy.GetMapChunk)
			cityWorlds.GET("/:world_id/spatial/changes", h.CityEconomy.ListSpatialMutations)
			cityWorlds.GET("/:world_id/navigation/portals", h.CityEconomy.ListWorldPortalStates)
			cityWorlds.POST("/:world_id/navigation/path", h.CityEconomy.FindWorldActorPath)
			cityWorlds.GET("/:world_id/navigation/intents", h.CityEconomy.ListWorldNavigationIntents)
			cityWorlds.GET("/:world_id/navigation/intents/:actor_code", h.CityEconomy.GetWorldNavigationIntent)
			cityWorlds.GET("/:world_id/navigation/reservations", h.CityEconomy.ListWorldNavigationReservations)
			cityWorlds.GET("/:world_id/commands", h.CityEconomy.ListCommands)
			cityWorlds.POST("/:world_id/commands", h.CityEconomy.SubmitCommand)
			cityWorlds.GET("/:world_id/commands/:command_id", h.CityEconomy.GetCommand)
			cityWorlds.POST("/:world_id/step", middleware.AdminOnly(), h.CityEconomy.StepWorld)
			cityWorlds.GET("/:world_id/events", h.CityEconomy.ListEvents)
			cityWorlds.GET("/:world_id/journals", h.CityEconomy.ListJournals)
			cityWorlds.GET("/:world_id/journals/:tick/:sequence", h.CityEconomy.GetJournal)
			cityWorlds.GET("/:world_id/trial-balance", h.CityEconomy.GetTrialBalance)
			cityWorlds.GET("/:world_id/resource-operations", h.CityEconomy.ListResourceOperations)
			cityWorlds.GET("/:world_id/resource-operations/:tick/:sequence", h.CityEconomy.GetResourceOperation)
			cityWorlds.GET("/:world_id/market-settlements", h.CityEconomy.ListMarketSettlements)
			cityWorlds.GET("/:world_id/market-settlements/:tick/:sequence", h.CityEconomy.GetMarketSettlement)
			cityWorlds.GET("/:world_id/population-movements", h.CityEconomy.ListPopulationMovements)
			cityWorlds.GET("/:world_id/population-movements/:tick/:sequence", h.CityEconomy.GetPopulationMovement)
			cityWorlds.GET("/:world_id/population-migrations", h.CityEconomy.ListPopulationMigrations)
			cityWorlds.GET("/:world_id/population-migrations/:tick/:sequence", h.CityEconomy.GetPopulationMigration)
			cityWorlds.GET("/:world_id/household-movements", h.CityEconomy.ListHouseholdMovements)
			cityWorlds.GET("/:world_id/household-movements/:tick/:sequence", h.CityEconomy.GetHouseholdMovement)
			cityWorlds.GET("/:world_id/snapshots", h.CityEconomy.ListSnapshots)
			cityWorlds.GET("/:world_id/snapshots/:tick", h.CityEconomy.GetSnapshot)
			cityWorlds.GET("/:world_id/engine", h.CityEconomy.GetEngineInfo)
			cityWorlds.GET("/:world_id/upgrade-runs", h.CityEconomy.ListUpgrades)
			cityWorlds.POST("/:world_id/upgrade-runs", middleware.AdminOnly(), h.CityEconomy.StartUpgrade)
			cityWorlds.GET("/:world_id/upgrade-runs/:run_id", h.CityEconomy.GetUpgrade)
			cityWorlds.GET("/:world_id/replay-runs", h.CityEconomy.ListReplays)
			cityWorlds.POST("/:world_id/replay-runs", middleware.AdminOnly(), h.CityEconomy.StartReplay)
			cityWorlds.GET("/:world_id/replay-runs/:run_id", h.CityEconomy.GetReplay)
			cityWorlds.GET("/:world_id/recovery-runs", h.CityEconomy.ListRecoveries)
			cityWorlds.POST("/:world_id/recovery-runs", middleware.AdminOnly(), h.CityEconomy.StartRecovery)
			cityWorlds.GET("/:world_id/recovery-runs/:run_id", h.CityEconomy.GetRecovery)
		}

		// 用户可用渠道（非管理员接口）
		channels := authenticated.Group("/channels")
		{
			channels.GET("/available", h.AvailableChannel.List)
		}

		// 使用记录
		usage := authenticated.Group("/usage")
		{
			usage.GET("", h.Usage.List)
			usage.GET("/errors", h.Usage.ListErrors)
			usage.GET("/errors/:id", h.Usage.GetErrorDetail)
			usage.GET("/:id", h.Usage.GetByID)
			usage.GET("/stats", h.Usage.Stats)
			// User dashboard endpoints
			usage.GET("/dashboard/stats", h.Usage.DashboardStats)
			usage.GET("/dashboard/trend", h.Usage.DashboardTrend)
			usage.GET("/dashboard/models", h.Usage.DashboardModels)
			usage.GET("/dashboard/snapshot-v2", h.Usage.DashboardSnapshotV2)
			usage.POST("/dashboard/api-keys-usage", h.Usage.DashboardAPIKeysUsage)
		}

		// 公告（用户可见）
		announcements := authenticated.Group("/announcements")
		{
			announcements.GET("", h.Announcement.List)
			announcements.POST("/read-all", h.Announcement.MarkAllRead)
			announcements.POST("/:id/read", h.Announcement.MarkRead)
		}

		// 卡密兑换
		redeem := authenticated.Group("/redeem")
		{
			redeem.POST("", h.Redeem.Redeem)
			redeem.GET("/history", h.Redeem.GetHistory)
		}

		// 用户订阅
		subscriptions := authenticated.Group("/subscriptions")
		{
			subscriptions.GET("", h.Subscription.List)
			subscriptions.GET("/active", h.Subscription.GetActive)
			subscriptions.GET("/progress", h.Subscription.GetProgress)
			subscriptions.GET("/summary", h.Subscription.GetSummary)
		}

		// 渠道监控（用户只读）
		monitors := authenticated.Group("/channel-monitors")
		{
			monitors.GET("", h.ChannelMonitor.List)
			monitors.GET("/:id/status", h.ChannelMonitor.GetStatus)
		}
	}
}
