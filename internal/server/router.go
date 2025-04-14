package server

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ab-testing-service/internal/middleware"
)

func (s *Server) setupRouter() {
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Public routes
	auth := r.Group("/api/auth")
	{
		auth.POST("/login", s.login)
		auth.POST("/register", s.register)
	}

	// Protected routes
	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware(s.config))
	{
		api.GET("/proxies", s.listProxies)
		api.POST("/proxies", s.createProxy)
		api.GET("/proxies/:id", s.getProxy)
		api.DELETE("/proxies/:id", s.deleteProxy)
		api.GET("/proxies/:id/history", s.getProxyChanges)
		api.GET("/proxies/:id/changes", s.getProxyChanges)
		api.PUT("/proxies/:id/targets", s.updateProxyTargets)
		api.PUT("/proxies/:id/url", s.updateProxyURL)
		api.PUT("/proxies/:id/cookies", s.updateProxySavingCookies)
		api.PUT("/proxies/:id/query-forwarding", s.updateProxyQueryForwarding)
		api.PUT("/proxies/:id/cookies-forwarding", s.updateProxyCookiesForwarding)
		api.PUT("/proxies/:id/condition", s.updateProxyCondition)

		// Tag management
		api.GET("/tags", s.getAllTags)
		api.GET("/proxies/by-tags", s.getProxiesByTags)
		api.PUT("/proxies/:id/tags", s.updateProxyTags)

		// Stats endpoints
		api.GET("/stats", s.getStats)
		api.GET("/stats/:proxy_id", s.getProxyStats)
	}

	// Metrics
	r.GET("/metrics", func(c *gin.Context) {
		handler := promhttp.Handler()
		handler.ServeHTTP(c.Writer, c.Request)
	})

	s.router = r
	s.srv = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port),
		Handler: r,
	}
}
