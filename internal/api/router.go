package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/api/handlers"
	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/l7shield"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

// NewRouter builds the full gin engine: public routes (login, subscription
// links), the auth-gated /api group, and the static SPA frontend served
// under the panel's configured base path.
func NewRouter(s *core.Server, webDir string) *gin.Engine {
	r := gin.New()
	r.MaxMultipartMemory = 64 << 20 // 64 MiB in-memory; larger parts spill to temp files
	r.Use(gin.Recovery(), securityHeaders(), l7shield.DefaultBlackhole.Middleware(), requestObservability(), requestRateLimit(240, time.Minute))
	_ = r.SetTrustedProxies(parseTrustedProxies(os.Getenv("ELECTRU_TRUSTED_PROXIES")))

	base := s.Config.PanelBasePath
	if base == "" {
		base = "/"
	}

	authH := handlers.NewAuthHandler(s)
	inboundH := handlers.NewInboundHandler(s)
	clientH := handlers.NewClientHandler(s)
	dashH := handlers.NewDashboardHandler(s)
	updH := handlers.NewUpdateHandler(s)
	subH := handlers.NewSubscriptionHandler(s)
	proH := handlers.NewProHandler(s)
	resellerH := handlers.NewResellerHandler(s)
	adminH := handlers.NewAdminHandler(s)
	ticketH := handlers.NewTicketHandler(s)
	tgH := handlers.NewTelegramHandler(s)
	shopH := handlers.NewShopHandler(s)
	financeH := handlers.NewFinanceHandler(s)
	obfH := handlers.NewObfuscationHandler(s)
	hpH := handlers.NewHoneypotHandler(s)
	replayH := handlers.NewReplayHandler(s)
	chainH := handlers.NewChainHandler(s)
	tmaH := handlers.NewTMAHandler(s)
	portalH := handlers.NewPortalHandler(s)
	opsH := handlers.NewOpsHandler(s)
	dnsH := handlers.NewDNSHandler(s)
	ultraH := handlers.NewUltraHandler(s)
	platformH := handlers.NewPlatformHandler(s)
	networkH := handlers.NewNetworkHandler(s)
	controlH := handlers.NewControlHandler(s)
	v1H := handlers.NewV1Handler(s)
	priorityH := handlers.NewPriorityHandler(s)
	enterpriseH := handlers.NewEnterpriseHandler(s)
	automationH := handlers.NewAutomationHandler(s)
	v35H := handlers.NewV35Handler(s)

	// For a non-root randomized base path, deliberately do not redirect / to
	// the panel path. That would disclose the hidden base path to anyone who
	// probes the server root. The root convenience login is only available
	// when the panel itself is mounted at /.

	group := r.Group(base)
	group.Use(func(c *gin.Context) { c.Set("panel_base_path", base); c.Next() })
	{
		// Public
		group.POST("/api/login", requestBodyLimit(64<<10), requestRateLimit(20, time.Minute), l7shield.Default.LoginMiddleware(), authH.Login)
		group.GET("/metrics", requestRateLimit(60, time.Minute), metricsHandler(s))
		group.GET("/sub/:subId/usage", ticketH.PublicUsage)
		group.GET("/sub/:subId/live", requestRateLimit(240, time.Minute), subH.PublicLive)
		pubTickets := group.Group("/sub/:subId/tickets", requestBodyLimit(256<<10), requestRateLimit(30, time.Minute))
		{
			pubTickets.GET("", ticketH.PublicListTickets)
			pubTickets.POST("", ticketH.PublicCreateTicket)
			pubTickets.GET("/:id", ticketH.PublicGetTicket)
			pubTickets.POST("/:id/reply", ticketH.PublicReplyTicket)
		}
		group.GET("/sub/:subId", requestRateLimit(120, time.Minute), l7shield.Default.Middleware(), subH.Serve)
		group.GET("/sub-group/:token", requestRateLimit(120, time.Minute), l7shield.Default.Middleware(), ultraH.ServeSubscriptionGroup)
		group.POST("/sub/:subId/device", requestBodyLimit(64<<10), requestRateLimit(30, time.Minute), l7shield.Default.Middleware(), subH.RegisterDevice)
		group.GET("/api/agent/manifest", platformH.AgentManifest)
		group.POST("/api/agent/heartbeat", requestBodyLimit(1<<20), requestRateLimit(120, time.Minute), platformH.AgentHeartbeat)
		group.POST("/api/agent/traffic", requestBodyLimit(1<<20), requestRateLimit(120, time.Minute), platformH.AgentTraffic)
		group.GET("/api/agent/poll", platformH.AgentPoll)
		group.POST("/api/agent/job/:id/result", requestBodyLimit(1<<20), requestRateLimit(120, time.Minute), platformH.AgentJobResult)
		group.Group("/assets", cacheStaticAssets(webDir)).Static("/", webDir+"/assets")
		group.GET("/", serveEntry(webDir, s))
		group.GET("/login", serveLogin(webDir))
		group.StaticFile("/api/docs/openapi.yaml", webDir+"/docs/openapi.yaml")
		group.GET("/dashboard", serveIndex(webDir))
		group.GET("/dashboard/*route", serveIndex(webDir))
		apiV1 := group.Group("/api/v1", auth.RequireAPIKey(s.DB))
		apiV1.GET("/health", auth.RequireAPIScope("system:read"), v1H.Health)
		apiV1.GET("/inbounds", auth.RequireAPIScope("inbounds:read"), v1H.Inbounds)
		apiV1.GET("/clients", auth.RequireAPIScope("clients:read"), v1H.Clients)
		apiV1.GET("/subscriptions", auth.RequireAPIScope("clients:read"), v1H.Subscriptions)
		apiV1.GET("/subscriptions/:id", auth.RequireAPIScope("clients:read"), v1H.Subscription)
		apiV1.GET("/subscriptions/:id/devices", auth.RequireAPIScope("clients:read"), v1H.SubscriptionDevices)
		apiV1.POST("/restart", auth.RequireAPIScope("system:write"), v1H.Restart)

		// Authenticated API -- gzip is scoped to just this group, see gzip.go.
		api := group.Group("/api", requestBodyLimit(4<<20), gzipMiddleware(), auth.RequireAuth(s.Config.JWTSecret, s.DB), sameOriginProtection(), requestRateLimit(240, time.Minute))
		{
			api.POST("/logout", authH.Logout)
			api.POST("/sessions/revoke-all", authH.RevokeAllSessions)
			api.GET("/me", authH.Me)
			api.POST("/change-password", auth.RequireRoles(s.DB, "owner", "admin", "operator", "viewer", "reseller"), authH.ChangePassword)
			api.POST("/change-credentials", auth.RequireRoles(s.DB, "owner", "admin", "operator", "viewer", "reseller"), authH.ChangeCredentials)

			// Multi-tier reseller (outside /pro group so resellers are not blocked by owner/admin-only middleware)
			rs := api.Group("/resellers", auth.RequireRoles(s.DB, "owner", "admin", "reseller"))
			{
				rs.GET("", resellerH.List)
				rs.GET("/me", resellerH.Me)
				rs.GET("/summary", resellerH.Summary)
				rs.GET("/stats", resellerH.Stats)
				rs.POST("/quote", resellerH.Quote)
				rs.GET("/settings/wholesale", resellerH.GetWholesaleSettings)
				rs.POST("/settings/wholesale", auth.RequireRoles(s.DB, "owner", "admin"), resellerH.SaveWholesaleSettings)
				rs.GET("/topups", resellerH.ListTopups)
				rs.POST("/topups", resellerH.CreateTopup)
				rs.POST("/topups/:id/approve", auth.RequireRoles(s.DB, "owner", "admin"), resellerH.ApproveTopup)
				rs.POST("/topups/:id/reject", auth.RequireRoles(s.DB, "owner", "admin"), resellerH.RejectTopup)
				rs.POST("", resellerH.Create)
				rs.POST("/:id/credit", resellerH.Credit)
				rs.POST("/:id/quotas", resellerH.UpdateQuotas)
				rs.GET("/:id/ledger", resellerH.Ledger)
			}
			api.GET("/panel/settings", auth.RequireRoles(s.DB, "owner", "admin"), authH.PanelSettings)
			api.POST("/panel/settings/apply", auth.RequireRoles(s.DB, "owner", "admin"), authH.ApplyPanelSettings)

			api.GET("/inbounds", inboundH.List)
			api.GET("/inbounds/search", inboundH.Search)
			api.GET("/inbounds/port-availability", inboundH.PortAvailability)
			api.POST("/inbounds/test-port", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.TestPort)
			api.POST("/inbounds/validate-draft", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.ValidateDraft)
			api.GET("/inbounds/reality-keypair", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.RealityKeypair)
			api.GET("/inbounds/:id/status", inboundH.Status)
			api.GET("/inbounds/:id/clients", clientH.ListByInbound)
			api.GET("/inbounds/:id/live", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Live)
			api.GET("/inbounds/:id/preview", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Preview)
			api.GET("/inbounds/:id/revisions", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Revisions)
			api.POST("/inbounds/:id/rollback", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Rollback)
			api.POST("/inbounds/:id/reset-traffic", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.ResetTraffic)
			api.POST("/inbounds/:id/clone", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Clone)
			api.GET("/inbounds/:id/analytics", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Analytics)
			api.GET("/inbounds/:id/health", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Health)
			api.GET("/inbounds/:id/export", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Export)
			api.POST("/inbounds/import", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Import)
			api.POST("/inbounds/import-3x-ui", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.XUIImport)
			api.POST("/inbounds/bulk", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.BulkAction)
			api.POST("/inbounds/mass-provision", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.MassProvision)
			api.POST("/inbounds/quick-grpc", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickGRPCCreate)
			api.POST("/inbounds/quick-websocket", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickWebSocketCreate)
			api.POST("/inbounds/quick-httpupgrade", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickHTTPUpgradeCreate)
			api.POST("/inbounds/quick-xhttp", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickXHTTPCreate)
			api.POST("/inbounds/quick-vless-reality", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickVLESSRealityCreate)
			api.POST("/inbounds/quick-anti-filter", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.QuickAntiFilterCreate)
			api.POST("/inbounds/:id/rotate-reality", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.RotateRealityKeys)
			api.POST("/inbounds/:id/anti-dpi", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.ApplyAntiDPI)
			api.POST("/inbounds/anti-dpi/bulk", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.ApplyAntiDPIBulk)
			api.POST("/inbounds", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Create)
			api.POST("/inbounds/:id/update", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Update)
			api.POST("/inbounds/:id/delete", auth.RequireRoles(s.DB, "owner", "admin", "operator"), inboundH.Delete)

			api.GET("/clients", clientH.List)
			api.POST("/clients", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales", "reseller"), clientH.Create)
			api.POST("/clients/bulk", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.BulkCreate)
			api.POST("/clients/bulk-action", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.BulkAction)
			api.GET("/clients/groups", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales", "reseller"), clientH.ListGroups)
			api.POST("/clients/groups/rename", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.RenameGroup)
			api.POST("/clients/:id/update", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales", "reseller"), clientH.Update)
			api.POST("/clients/:id/delete", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales", "reseller"), clientH.Delete)
			api.POST("/clients/:id/reset", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.ResetTraffic)
			api.POST("/clients/:id/rotate-secret", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.RotateSecret)
			api.GET("/clients/:id/link", auth.RequireRoles(s.DB, "owner", "admin", "operator", "reseller"), clientH.ShareLink)
			api.GET("/clients/:id/live", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.Live)
			api.GET("/online-users", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.OnlineUsers)
			api.POST("/clients/:id/kick", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.Kick)
			api.GET("/clients/:id/devices", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales", "reseller"), clientH.ListDevices)
			api.POST("/clients/:id/devices/:deviceId/revoke", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.RevokeClientDevice)
			api.POST("/clients/:id/devices/revoke-all", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.RevokeAllClientDevices)
			api.GET("/clients/:id/profiles", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.Profiles)
			api.POST("/clients/:id/profiles", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.CreateProfile)
			api.POST("/clients/:id/profiles/bulk", auth.RequireRoles(s.DB, "owner", "admin", "operator", "sales"), clientH.BulkAttachProfiles)
			api.DELETE("/clients/:id/profiles/:profileId", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.DeleteProfile)
			api.POST("/clients/:id/doctor", auth.RequireRoles(s.DB, "owner", "admin", "operator"), clientH.Doctor)

			api.GET("/dashboard/overview", dashH.Overview)
			api.GET("/dashboard/xray-logs", dashH.XrayLogs)
			api.POST("/dashboard/xray/:action", auth.RequireRoles(s.DB, "owner", "admin", "operator"), dashH.XrayAction)
			api.POST("/dashboard/sync-traffic", auth.RequireRoles(s.DB, "owner", "admin", "operator"), dashH.SyncTraffic)
			api.POST("/dashboard/reset-all-traffic", auth.RequireRoles(s.DB, "owner", "admin", "operator"), dashH.ResetAllTraffic)
			api.GET("/panel/update/check", auth.RequireRoles(s.DB, "owner", "admin"), updH.CheckUpdate)
			api.POST("/panel/update/apply", auth.RequireRoles(s.DB, "owner", "admin"), updH.ApplyUpdate)
			api.GET("/panel/update/status", auth.RequireRoles(s.DB, "owner", "admin"), updH.UpdateStatus)
			pro := api.Group("/pro", auth.RequireRoles(s.DB, "owner", "admin"))
			pro.GET("/system", proH.System)
			pro.GET("/shop/plans", auth.RequireRoles(s.DB, "owner", "admin", "sales"), shopH.ListPlans)
			pro.POST("/shop/plans", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SavePlan)
			pro.POST("/shop/plans/:id/delete", auth.RequireRoles(s.DB, "owner", "admin"), shopH.DeletePlan)
			pro.GET("/shop/orders", auth.RequireRoles(s.DB, "owner", "admin", "sales"), shopH.ListOrders)
			pro.POST("/shop/orders/:id/approve", auth.RequireRoles(s.DB, "owner", "admin"), shopH.ApproveOrder)
			pro.POST("/shop/orders/:id/reject", auth.RequireRoles(s.DB, "owner", "admin"), shopH.RejectOrder)
			pro.GET("/shop/settings", auth.RequireRoles(s.DB, "owner", "admin"), shopH.GetSettings)
			pro.POST("/shop/settings", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SaveSettings)
			pro.GET("/shop/trial", auth.RequireRoles(s.DB, "owner", "admin"), shopH.GetTrialSettings)
			pro.POST("/shop/trial", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SaveTrialSettings)
			pro.GET("/shop/referral", auth.RequireRoles(s.DB, "owner", "admin"), shopH.GetReferralSettings)
			pro.POST("/shop/referral", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SaveReferralSettings)
			pro.GET("/shop/custom-gb", auth.RequireRoles(s.DB, "owner", "admin"), shopH.GetCustomGBSettings)
			pro.POST("/shop/custom-gb", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SaveCustomGBSettings)
			pro.GET("/shop/winback", auth.RequireRoles(s.DB, "owner", "admin"), shopH.GetWinbackSettings)
			pro.POST("/shop/winback", auth.RequireRoles(s.DB, "owner", "admin"), shopH.SaveWinbackSettings)
			pro.GET("/shop/broadcast/preview", auth.RequireRoles(s.DB, "owner", "admin"), shopH.BroadcastPreview)
			pro.POST("/shop/broadcast", auth.RequireRoles(s.DB, "owner", "admin"), shopH.Broadcast)
			pro.GET("/shop/broadcast/history", auth.RequireRoles(s.DB, "owner", "admin"), shopH.BroadcastHistory)

			// Financial reports & analytics
			pro.GET("/finance/summary", auth.RequireRoles(s.DB, "owner", "admin", "sales"), financeH.Summary)
			pro.GET("/finance/plan-sales", auth.RequireRoles(s.DB, "owner", "admin", "sales"), financeH.PlanSales)
			pro.GET("/finance/monthly-trend", auth.RequireRoles(s.DB, "owner", "admin", "sales"), financeH.MonthlyTrend)
			pro.GET("/finance/churn", auth.RequireRoles(s.DB, "owner", "admin", "sales"), financeH.Churn)
			pro.GET("/finance/transactions", auth.RequireRoles(s.DB, "owner", "admin", "sales"), financeH.Transactions)
			pro.GET("/finance/export.csv", auth.RequireRoles(s.DB, "owner", "admin"), financeH.ExportCSV)
			pro.GET("/finance/export-report", auth.RequireRoles(s.DB, "owner", "admin"), financeH.ExportReportHTML)
			pro.GET("/obfuscation/dests", auth.RequireRoles(s.DB, "owner", "admin", "operator"), obfH.ListDests)
			pro.POST("/obfuscation/dests", auth.RequireRoles(s.DB, "owner", "admin"), obfH.UpsertDest)
			pro.DELETE("/obfuscation/dests/:id", auth.RequireRoles(s.DB, "owner", "admin"), obfH.DeleteDest)
			pro.POST("/obfuscation/health", auth.RequireRoles(s.DB, "owner", "admin", "operator"), obfH.HealthCheck)
			pro.POST("/obfuscation/apply", auth.RequireRoles(s.DB, "owner", "admin"), obfH.ApplyDest)
			pro.GET("/honeypot/stats", auth.RequireRoles(s.DB, "owner", "admin", "operator"), hpH.Stats)
			pro.POST("/honeypot/unban/:ip", auth.RequireRoles(s.DB, "owner", "admin"), hpH.Unban)
			pro.POST("/honeypot/ban", auth.RequireRoles(s.DB, "owner", "admin"), hpH.ManualBan)
			pro.GET("/honeypot/daily-report", auth.RequireRoles(s.DB, "owner", "admin", "operator"), hpH.DailyReport)
			pro.GET("/l7shield/stats", auth.RequireRoles(s.DB, "owner", "admin", "operator"), func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true, "obj": gin.H{
					"shield":    l7shield.Default.Stats(),
					"blackhole": l7shield.DefaultBlackhole.Stats(),
				}})
			})
			pro.GET("/l7shield/blackhole", auth.RequireRoles(s.DB, "owner", "admin", "operator"), func(c *gin.Context) {
				c.JSON(200, gin.H{"success": true, "obj": gin.H{
					"bans":  l7shield.DefaultBlackhole.List(),
					"stats": l7shield.DefaultBlackhole.Stats(),
				}})
			})
			pro.POST("/l7shield/blackhole/ban", auth.RequireRoles(s.DB, "owner", "admin"), func(c *gin.Context) {
				var req struct {
					IP     string `json:"ip"`
					Reason string `json:"reason"`
					Hours  int    `json:"hours"`
				}
				if c.ShouldBindJSON(&req) != nil || req.IP == "" {
					c.JSON(400, gin.H{"success": false, "msg": "ip required"})
					return
				}
				d := time.Duration(req.Hours) * time.Hour
				if d <= 0 {
					d = 2 * time.Hour
				}
				reason := req.Reason
				if reason == "" {
					reason = "manual"
				}
				l7shield.DefaultBlackhole.Ban(req.IP, reason, "", d)
				c.JSON(200, gin.H{"success": true})
			})
			pro.POST("/l7shield/unblock", auth.RequireRoles(s.DB, "owner", "admin"), func(c *gin.Context) {
				var req struct {
					IP string `json:"ip"`
				}
				if c.ShouldBindJSON(&req) != nil || req.IP == "" {
					c.JSON(400, gin.H{"success": false, "msg": "ip required"})
					return
				}
				l7shield.Default.Unblock(req.IP)
				l7shield.DefaultBlackhole.Unban(req.IP)
				c.JSON(200, gin.H{"success": true})
			})
			pro.POST("/honeypot/settings", auth.RequireRoles(s.DB, "owner", "admin"), hpH.SaveSettings)
			pro.GET("/tunnel-chain", auth.RequireRoles(s.DB, "owner", "admin"), chainH.List)
			pro.GET("/tunnel-chain/hops", auth.RequireRoles(s.DB, "owner", "admin", "operator"), chainH.HopCandidates)
			pro.DELETE("/tunnel-chain/:id", auth.RequireRoles(s.DB, "owner", "admin"), chainH.Delete)
			pro.POST("/tunnel-chain", auth.RequireRoles(s.DB, "owner", "admin"), chainH.Save)
			pro.POST("/tunnel-chain/:id/enabled", auth.RequireRoles(s.DB, "owner", "admin"), chainH.SetEnabled)
			pro.POST("/tunnel-chain/:id/rotate", auth.RequireRoles(s.DB, "owner", "admin"), chainH.Rotate)
			pro.GET("/telegram/config", auth.RequireRoles(s.DB, "owner", "admin"), tgH.GetConfig)
			pro.POST("/telegram/config", auth.RequireRoles(s.DB, "owner", "admin"), tgH.SaveConfig)
			pro.POST("/telegram/test-admin", auth.RequireRoles(s.DB, "owner", "admin"), tgH.TestAdminBot)
			pro.POST("/telegram/test-user", auth.RequireRoles(s.DB, "owner", "admin"), tgH.TestUserBot)
			pro.GET("/tickets", auth.RequireRoles(s.DB, "owner", "admin", "support", "operator", "sales"), ticketH.ListTickets)
			pro.GET("/tickets/:id", auth.RequireRoles(s.DB, "owner", "admin", "support", "operator", "sales"), ticketH.GetTicket)
			pro.POST("/tickets", auth.RequireRoles(s.DB, "owner", "admin", "support", "operator"), ticketH.CreateTicketAdmin)
			pro.POST("/tickets/:id/reply", auth.RequireRoles(s.DB, "owner", "admin", "support", "operator"), ticketH.ReplyTicket)
			pro.POST("/tickets/:id/close", auth.RequireRoles(s.DB, "owner", "admin", "support", "operator"), ticketH.CloseTicket)
			pro.GET("/nodes", proH.Nodes)
			pro.POST("/nodes", proH.AddNode)
			pro.DELETE("/nodes/:id", proH.DeleteNode)
			pro.GET("/api-keys", proH.APIKeys)
			pro.POST("/api-keys", proH.CreateAPIKey)
			pro.DELETE("/api-keys/:id", proH.DeleteAPIKey)
			pro.GET("/backups", proH.Backups)
			pro.POST("/backups", proH.Backup)
			pro.POST("/backups/upload", auth.RequireRoles(s.DB, "owner", "admin"), proH.UploadBackup)
			pro.GET("/backups/:id/download", proH.DownloadBackup)
			pro.POST("/backups/:id/restore", auth.RequireRoles(s.DB, "owner", "admin"), proH.RestoreBackup)
			pro.DELETE("/backups/:id", proH.DeleteBackup)
			pro.POST("/users", proH.CreateUser)
			pro.POST("/users/:id/role", proH.ChangeRole)
			pro.GET("/users", proH.Users)
			pro.POST("/notifications/test", proH.TestNotification)
			pro.GET("/devices", ultraH.Devices)
			pro.DELETE("/devices/:id", ultraH.DeleteDevice)
			pro.POST("/clients/:id/devices/reset", ultraH.ResetDevices)
			pro.GET("/plans", ultraH.Plans)
			pro.POST("/plans", ultraH.CreatePlan)
			pro.DELETE("/plans/:id", ultraH.DeletePlan)
			pro.POST("/clients/:id/plan", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.ApplyPlan)
			pro.POST("/clients/:id/subscription/rotate", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.RotateSubscription)
			pro.GET("/subscriptions/analytics", ultraH.SubscriptionAnalytics)
			pro.GET("/subscriptions/analytics/overview", ultraH.SubscriptionAnalyticsOverview)
			pro.GET("/subscriptions/access-log", ultraH.SubscriptionAccessLog)
			pro.GET("/subscriptions/presets", ultraH.SubscriptionPresets)
			pro.POST("/subscriptions/presets", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.CreateSubscriptionPreset)
			pro.DELETE("/subscriptions/presets/:id", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.DeleteSubscriptionPreset)
			pro.POST("/subscriptions/groups/:id/rotate", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.RotateSubscriptionGroup)
			pro.POST("/subscriptions/groups/:id/toggle", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.ToggleSubscriptionGroup)
			pro.POST("/clients/:id/subscription/revoke", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.RevokeSubscription)
			pro.POST("/clients/:id/subscription/enable", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.EnableSubscription)
			pro.POST("/clients/:id/subscription/apply-preset", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.ApplySubscriptionPreset)
			pro.POST("/clients/:id/subscription/policy", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.UpdateSubscriptionPolicy)
			pro.POST("/clients/:id/subscription/renew", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.RenewSubscription)
			pro.POST("/devices/:id/approve", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.ApproveDevice)
			pro.POST("/devices/:id/revoke", auth.RequireRoles(s.DB, "owner", "admin", "operator"), ultraH.RevokeDevice)
			pro.GET("/subscriptions/groups", ultraH.SubscriptionGroups)
			pro.POST("/subscriptions/groups", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.CreateSubscriptionGroup)
			pro.DELETE("/subscriptions/groups/:id", auth.RequireRoles(s.DB, "owner", "admin"), ultraH.DeleteSubscriptionGroup)
			pro.GET("/subscriptions/clinic", ultraH.SubscriptionClinic)
			pro.GET("/platform/summary", platformH.PlatformSummary)
			pro.GET("/enterprise/search", enterpriseH.Search)
			pro.GET("/enterprise/metrics", enterpriseH.Metrics)
			pro.GET("/enterprise/alerts", enterpriseH.Alerts)
			pro.GET("/automation/rules", automationH.Rules)
			pro.POST("/automation/rules", auth.RequireRoles(s.DB, "owner", "admin"), automationH.CreateRule)
			pro.POST("/automation/rules/:id", auth.RequireRoles(s.DB, "owner", "admin"), automationH.UpdateRule)
			pro.DELETE("/automation/rules/:id", auth.RequireRoles(s.DB, "owner", "admin"), automationH.DeleteRule)
			pro.GET("/scheduler/tasks", automationH.Tasks)
			pro.POST("/scheduler/tasks", auth.RequireRoles(s.DB, "owner", "admin"), automationH.CreateTask)
			pro.POST("/scheduler/tasks/:id", auth.RequireRoles(s.DB, "owner", "admin"), automationH.UpdateTask)
			pro.DELETE("/scheduler/tasks/:id", auth.RequireRoles(s.DB, "owner", "admin"), automationH.DeleteTask)
			pro.POST("/scheduler/run-now", auth.RequireRoles(s.DB, "owner", "admin", "operator"), automationH.RunNow)
			pro.POST("/enterprise/alerts/refresh", auth.RequireRoles(s.DB, "owner", "admin", "operator"), enterpriseH.RefreshAlerts)
			pro.POST("/enterprise/alerts/:id/resolve", auth.RequireRoles(s.DB, "owner", "admin", "operator"), enterpriseH.ResolveAlert)
			pro.GET("/platform/security", platformH.SecuritySummary)
			pro.GET("/platform/jobs", platformH.Jobs)
			pro.POST("/platform/jobs", auth.RequireRoles(s.DB, "owner", "admin", "operator"), platformH.CreateJob)
			pro.GET("/platform/billing/plans", platformH.BillingPlans)
			pro.POST("/platform/billing/plans", auth.RequireRoles(s.DB, "owner", "admin"), platformH.CreateBillingPlan)
			pro.DELETE("/platform/billing/plans/:id", auth.RequireRoles(s.DB, "owner", "admin"), platformH.DeleteBillingPlan)
			pro.GET("/platform/billing/orders", platformH.Orders)
			pro.POST("/platform/billing/orders", auth.RequireRoles(s.DB, "owner", "admin", "operator"), platformH.CreateOrder)
			pro.POST("/platform/billing/orders/:id", auth.RequireRoles(s.DB, "owner", "admin", "operator"), platformH.UpdateOrder)
			pro.GET("/platform/webhooks", platformH.Webhooks)
			pro.POST("/platform/webhooks", auth.RequireRoles(s.DB, "owner", "admin"), platformH.CreateWebhook)
			pro.DELETE("/platform/webhooks/:id", auth.RequireRoles(s.DB, "owner", "admin"), platformH.DeleteWebhook)
			pro.GET("/platform/certificates", platformH.Certificates)
			pro.POST("/platform/certificates", auth.RequireRoles(s.DB, "owner", "admin"), platformH.AddCertificate)
			pro.DELETE("/platform/certificates/:id", auth.RequireRoles(s.DB, "owner", "admin"), platformH.DeleteCertificate)
			pro.GET("/platform/operations", priorityH.Operations)
			pro.GET("/platform/operations/:id", priorityH.Operation)
			pro.POST("/platform/operations", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.CreateOperation)
			pro.POST("/platform/operations/:id/cancel", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.CancelOperation)
			pro.GET("/platform/node-groups", priorityH.NodeGroups)
			pro.GET("/platform/node-selection", priorityH.PreviewNodeSelection)
			pro.POST("/platform/node-groups", auth.RequireRoles(s.DB, "owner", "admin"), priorityH.CreateNodeGroup)
			pro.POST("/platform/node-groups/:id/members", auth.RequireRoles(s.DB, "owner", "admin"), priorityH.AddNodeGroupMember)
			pro.POST("/platform/nodes/:id/drain", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.SetNodeDrain)
			pro.GET("/platform/certificate-history", priorityH.CertificateHistory)
			pro.GET("/platform/domains", priorityH.Domains)
			pro.POST("/platform/domains", auth.RequireRoles(s.DB, "owner", "admin"), priorityH.CreateDomain)
			pro.POST("/platform/domains/:id/check", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.CheckDomain)
			pro.POST("/platform/domains/:id/failover", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.FailoverDomain)
			pro.POST("/platform/nodes/:id/agent-diagnostics", auth.RequireRoles(s.DB, "owner", "admin", "operator"), priorityH.AgentDiagnostics)
			pro.GET("/platform/plugins", platformH.Plugins)
			pro.POST("/platform/plugins", auth.RequireRoles(s.DB, "owner", "admin"), platformH.UpsertPlugin)
			pro.DELETE("/platform/plugins/:id", auth.RequireRoles(s.DB, "owner", "admin"), platformH.DeletePlugin)
			pro.POST("/platform/nodes/agent-token", auth.RequireRoles(s.DB, "owner", "admin"), platformH.RegisterAgent)
			pro.POST("/platform/nodes/:id/agent-token/rotate", auth.RequireRoles(s.DB, "owner", "admin"), proH.RotateNodeAgentToken)
			pro.POST("/platform/nodes/:id/agent-enabled", auth.RequireRoles(s.DB, "owner", "admin"), proH.SetNodeAgentEnabled)
			pro.GET("/platform/nodes/:id/connection", proH.NodeConnectionInfo)
			pro.POST("/platform/nodes/:id/deploy-config", auth.RequireRoles(s.DB, "owner", "admin", "operator"), platformH.DeployCurrentConfig)
			pro.GET("/network/bbr", networkH.Status)
			pro.POST("/network/bbr/test", networkH.Test)
			pro.POST("/network/bbr/apply", auth.RequireRoles(s.DB, "owner", "admin", "operator"), networkH.Apply)
			pro.POST("/network/bbr/rollback", auth.RequireRoles(s.DB, "owner", "admin", "operator"), networkH.Rollback)
			pro.GET("/control/performance", controlH.Performance)
			pro.GET("/control/performance/history", controlH.PerformanceHistory)
			pro.GET("/control/routing", controlH.Routing)
			pro.POST("/control/routing", auth.RequireRoles(s.DB, "owner", "admin"), controlH.CreateRouting)
			pro.DELETE("/control/routing/:id", auth.RequireRoles(s.DB, "owner", "admin"), controlH.DeleteRouting)
			pro.GET("/control/select-node", controlH.SelectNode)
			pro.GET("/control/advisor", controlH.Advisor)
			pro.GET("/control/deployments", controlH.Deployments)
			pro.POST("/control/deployments", auth.RequireRoles(s.DB, "owner", "admin", "operator"), controlH.CreateDeployment)
			pro.POST("/control/deployments/:id/rollback", auth.RequireRoles(s.DB, "owner", "admin", "operator"), controlH.RollbackDeployment)
			pro.GET("/admins", auth.RequireRoles(s.DB, "owner"), adminH.ListAdmins)
			pro.POST("/admins", auth.RequireRoles(s.DB, "owner"), adminH.CreateAdmin)
			pro.POST("/admins/:id", auth.RequireRoles(s.DB, "owner"), adminH.UpdateAdmin)
			pro.GET("/admins/me/quota", auth.RequireRoles(s.DB, "owner", "admin", "sales", "support", "operator"), adminH.MeQuota)
			pro.GET("/audit", auth.RequireRoles(s.DB, "owner", "admin", "sales", "support", "operator"), adminH.ListAudit)
			pro.GET("/replay/search", auth.RequireRoles(s.DB, "owner", "admin", "sales", "support", "operator"), replayH.Search)
			pro.GET("/replay/client/:id", auth.RequireRoles(s.DB, "owner", "admin", "sales", "support", "operator"), replayH.Timeline)
			pro.POST("/replay/client/:id/note", auth.RequireRoles(s.DB, "owner", "admin", "sales", "support", "operator"), replayH.AddNote)
			pro.GET("/usage/snapshots", ultraH.Snapshots)
			pro.GET("/clients/:id/export", ultraH.Export)
			pro.GET("/ops/health", opsH.Health)
			pro.GET("/ops/command-center", v35H.CommandCenter)
			pro.GET("/ops/preflight", v35H.Preflight)
			pro.GET("/ops/recovery", v35H.Recovery)
			pro.GET("/ops/alerts", opsH.Alerts)
			pro.GET("/ops/system", opsH.SystemInfo)
			pro.GET("/ops/public-host", opsH.PublicHostStatus)
			pro.POST("/ops/public-host/domain", auth.RequireRoles(s.DB, "owner", "admin"), opsH.SetPublicDomain)
			pro.POST("/ops/public-host/ipv4", auth.RequireRoles(s.DB, "owner", "admin"), opsH.SetPublicIPv4)
			pro.POST("/ops/public-host/auto", auth.RequireRoles(s.DB, "owner", "admin"), opsH.SetAutoPublicIP)
			pro.GET("/ops/settings", opsH.Settings)
			pro.POST("/ops/settings", opsH.SaveSettings)
			pro.GET("/dns", auth.RequireRoles(s.DB, "owner", "admin", "operator"), dnsH.Get)
			pro.POST("/dns", auth.RequireRoles(s.DB, "owner", "admin"), dnsH.Save)
			pro.GET("/ops/templates", opsH.Templates)
			pro.POST("/ops/templates", opsH.CreateTemplate)
			pro.DELETE("/ops/templates/:id", opsH.DeleteTemplate)
			pro.GET("/ops/notifications", opsH.Notifications)
			pro.POST("/ops/notifications", opsH.SaveNotification)
			pro.DELETE("/ops/notifications/:id", opsH.DeleteNotification)
			pro.GET("/ops/diagnostics", opsH.Diagnostics)
			pro.GET("/ops/security-scan", opsH.SecurityScan)
			pro.GET("/ops/sessions", opsH.Sessions)
			pro.POST("/ops/sessions/:id/revoke", opsH.RevokeSession)
			pro.POST("/ops/backups/:id/verify", opsH.VerifyBackup)
			pro.POST("/ops/database/:action", auth.RequireRoles(s.DB, "owner", "admin"), opsH.DatabaseMaintenance)
			pro.GET("/ops/runtime", opsH.Runtime)
			pro.GET("/ops/log-tail", opsH.LogTail)
			pro.GET("/ops/emergency", opsH.EmergencyState)
			pro.POST("/ops/emergency", auth.RequireRoles(s.DB, "owner", "admin"), opsH.Emergency)
			pro.POST("/ops/validate", opsH.ValidateConfig)
			pro.POST("/ops/service/:action", auth.RequireRoles(s.DB, "owner", "admin", "operator"), opsH.Services)
		}
	}

	// Telegram Mini App API (auth via initData).
	// Body limit + stricter rate limits mitigate abuse / application-layer floods.
	tma := r.Group("/api/tma", requestBodyLimit(64<<10), requestRateLimit(90, time.Minute))
	{
		tma.GET("/dashboard", tmaH.Dashboard)
		tma.GET("/pin/status", tmaH.PinStatus)
		tma.POST("/pin/set", requestRateLimit(10, time.Minute), tmaH.PinSet)
		tma.POST("/pin/verify", requestRateLimit(20, time.Minute), tmaH.PinVerify)
		tma.POST("/pin/disable", requestRateLimit(10, time.Minute), tmaH.PinDisable)
		tma.GET("/devices", tmaH.ListDevices)
		tma.GET("/configs", tmaH.ListConfigs)
		tma.POST("/configs/:id/label", requestRateLimit(30, time.Minute), tmaH.RenameConfig)
		tma.POST("/devices/:id/revoke", requestRateLimit(20, time.Minute), tmaH.RevokeDevice)
		tma.POST("/devices/revoke-all", requestRateLimit(10, time.Minute), tmaH.RevokeAllDevices)
		tma.POST("/devices/:id/profile", requestRateLimit(30, time.Minute), tmaH.AssignDevice)
		tma.GET("/profiles", tmaH.ListProfiles)
		tma.POST("/profiles", requestRateLimit(20, time.Minute), tmaH.UpsertProfile)
		tma.GET("/profiles/bundle", tmaH.ProfilesBundle)
		tma.POST("/reset-uuid", requestRateLimit(10, time.Minute), tmaH.ResetUUID)
		tma.GET("/plans", tmaH.Plans)
		tma.GET("/wallet", tmaH.Wallet)
		tma.GET("/orders", tmaH.Orders)
		tma.POST("/wallet/topup", requestRateLimit(15, time.Minute), tmaH.CreateTopup)
		tma.POST("/shop/buy", requestRateLimit(20, time.Minute), tmaH.CreatePurchase)
		tma.POST("/shop/buy-wallet", requestRateLimit(15, time.Minute), tmaH.BuyWithWallet)
		tma.POST("/promo/apply", requestRateLimit(30, time.Minute), tmaH.ApplyPromo)
		tma.POST("/renew", requestRateLimit(15, time.Minute), tmaH.RenewWithWallet)
		tma.POST("/autorenew", requestRateLimit(20, time.Minute), tmaH.SetAutoRenew)
		// Cloud Backup & Account Migration
		tma.GET("/backup/status", tmaH.BackupStatus)
		tma.POST("/backup/create", requestRateLimit(10, time.Minute), tmaH.CreateBackup)
		tma.POST("/backup/restore", requestRateLimit(8, time.Minute), tmaH.RestoreBackup)
		tma.POST("/backup/revoke", requestRateLimit(10, time.Minute), tmaH.RevokeBackup)
		// Speed test + usage charts
		tma.GET("/speedtest/targets", tmaH.SpeedTestTargets)
		tma.POST("/speedtest/server-ping", requestRateLimit(40, time.Minute), tmaH.SpeedTestServerPing)
		tma.GET("/speedtest/payload", requestRateLimit(20, time.Minute), tmaH.SpeedTestPayload)
		tma.GET("/usage/history", tmaH.UsageHistory)
		tma.GET("/gifts", tmaH.GiftList)
		tma.POST("/gifts/buy", requestRateLimit(15, time.Minute), tmaH.GiftBuy)
		tma.POST("/gifts/redeem", requestRateLimit(10, time.Minute), tmaH.GiftRedeem)
		tma.GET("/referral", tmaH.ReferralInfo)
		tma.POST("/trial", requestRateLimit(5, time.Minute), tmaH.ClaimTrial)
		// Admin Telegram WebApp: initData → panel JWT session (tight limit)
		tma.POST("/admin/auth", requestRateLimit(8, time.Minute), tmaH.AdminAuth)
		tma.GET("/admin/me", requestRateLimit(30, time.Minute), tmaH.AdminMe)
	}
	// User web portal (browser, no Telegram required — login with Sub ID)
	portal := r.Group("/api/portal", requestBodyLimit(64<<10), requestRateLimit(60, time.Minute))
	{
		portal.POST("/login", requestRateLimit(20, time.Minute), portalH.Login)
		portal.POST("/logout", portalH.Logout)
		portal.GET("/dashboard", portalH.Dashboard)
		portal.GET("/plans", portalH.Plans)
		portal.POST("/gifts/redeem", requestRateLimit(10, time.Minute), portalH.RedeemGift)
	}
	r.Static("/portal", "./web/portal")
	r.Static("/tma", "./web/tma")

	return r
}

func parseTrustedProxies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Safe default for local reverse-proxy (nginx on same host).
		// Override with ELECTRU_TRUSTED_PROXIES=ip1,ip2 for multi-hop / cloud LB.
		raw = "127.0.0.1,::1"
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func serveIndex(webDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		serveHTML(c, webDir+"/index.html")
	}
}

// serveEntry serves the panel entrypoint at the configured panel base path.
// The route is registered inside the base-path group, so the server root (/)
// remains untouched and does not reveal a randomized base path.
func serveEntry(webDir string, s *core.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessionValid(c, s) {
			serveHTML(c, webDir+"/index.html")
			return
		}
		serveHTML(c, webDir+"/login.html")
	}
}

func serveLogin(webDir string) gin.HandlerFunc {
	return func(c *gin.Context) { serveHTML(c, webDir+"/login.html") }
}

// serveHTML injects the configured panel base path into the SPA shell and
// disables caching. The same HTML URL can represent login or dashboard
// content depending on the current session, so intermediaries must never
// cache it.
func serveHTML(c *gin.Context, filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	base := "/"
	if v, ok := c.Get("panel_base_path"); ok {
		if b, ok := v.(string); ok && b != "" {
			base = b
		}
	}
	if base == "/" {
		base = ""
	} else {
		base = "/" + strings.Trim(strings.TrimSpace(base), "/")
	}
	// Keep root as an empty string because the HTML template appends the
	// trailing slash. This yields <base href="/"> at the root instead of
	// the invalid <base href="//">.
	attrBase := base
	jsonBase := strconv.Quote(base)
	html := strings.ReplaceAll(string(data), "__ELECTRU_BASE_PATH_ATTR__", attrBase)
	html = strings.ReplaceAll(html, "__ELECTRU_BASE_PATH_JSON__", jsonBase)
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Header("Pragma", "no-cache")
	c.Header("Vary", "Accept-Encoding")
	if strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
		if gz, ok := gzipHTMLCached(filename+"\x00"+base, html); ok {
			c.Header("Content-Encoding", "gzip")
			c.Data(http.StatusOK, "text/html; charset=utf-8", gz)
			return
		}
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func sessionValid(c *gin.Context, s *core.Server) bool {
	token, err := c.Cookie(auth.SessionCookieName)
	if err != nil || token == "" {
		return false
	}
	claims, err := auth.ParseToken(s.Config.JWTSecret, token)
	if err != nil || claims == nil || claims.IssuedAt == nil {
		return false
	}
	var user models.User
	if err := s.DB.Select("password_changed_at").First(&user, claims.UserID).Error; err != nil {
		return false
	}
	return user.PasswordChangedAt == nil || !claims.IssuedAt.Time.Before(*user.PasswordChangedAt)
}

// NotFoundJSON is a small helper handlers can reuse for a consistent 404 shape.
func NotFoundJSON(c *gin.Context, msg string) {
	c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": msg})
}
