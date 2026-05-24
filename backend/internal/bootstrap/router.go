package bootstrap

import (
	"github.com/gin-gonic/gin"

	"github.com/eyjian/fin-vault/backend/internal/middleware"
)

// RegisterRoutes 创建 Gin 引擎并按规定顺序挂载中间件 + 路由。
//
// 中间件顺序（强制）：RequestID → Logger → Recovery → CORS → Auth → 业务
func RegisterRoutes(app *App) *gin.Engine {
	if app.Cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else if app.Cfg.Server.Mode == "test" {
		gin.SetMode(gin.TestMode)
	}

	r := gin.New()
	// 中间件（顺序不可变）
	r.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
		middleware.CORS(app.Cfg.Server.CORSOrigins),
		middleware.Auth(middleware.AuthConfig{
			Mode:          app.Cfg.Auth.Mode,
			DefaultUserID: app.Cfg.Auth.DefaultUserID,
		}),
	)

	// 顶层 healthz（不进 v1 group，便于 LB 探活）
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0, "status": "ok"})
	})

	v1 := r.Group("/api/v1")

	h := app.Handlers
	if h == nil {
		return r
	}

	h.Meta.Register(v1)        // /healthz /version /platforms
	h.Asset.Register(v1)       // /assets
	h.Holding.Register(v1)     // /holdings
	h.Transaction.Register(v1) // /transactions
	h.Quote.Register(v1)       // /quotes
	h.Rate.Register(v1)        // /rates
	h.Export.Register(v1)      // /export

	if h.AIMeta != nil {
		h.AIMeta.Register(v1) // /ai/providers
	}
	if h.AISession != nil {
		h.AISession.Register(v1) // /ai/sessions  (CRUD + listMessages)
	}
	if h.AIMessage != nil {
		h.AIMessage.Register(v1) // POST /ai/sessions/:id/messages
	}
	if h.AIPulseDiagnosis != nil {
		h.AIPulseDiagnosis.Register(v1) // POST /ai/pulse-diagnosis & GET /ai/pulse-diagnosis
	}

	// dev 模式开放手工触发 cron
	if app.Cfg.Server.Mode == "debug" || app.Cfg.Server.Mode == "test" {
		v1.POST("/admin/cron/mature/run", func(c *gin.Context) {
			stat, err := app.Cron.RunMatureOnce(c.Request.Context())
			if err != nil {
				c.JSON(500, gin.H{"code": 90003, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": stat})
		})
	}

	return r
}
