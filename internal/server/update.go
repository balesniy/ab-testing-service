package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ab-testing-service/internal/models"
	"github.com/ab-testing-service/internal/proxy"
)

type UpdateTargetsRequest struct {
	Targets []struct {
		URL      string  `json:"url" binding:"required"`
		Weight   float64 `json:"weight" binding:"required,min=0,max=1"`
		IsActive bool    `json:"is_active"`
	} `json:"targets"`
	Condition *RouteCondition `json:"condition,omitempty"`
}

type UpdateSavingCookiesRequest struct {
	SavingCookiesFlg bool `json:"saving_cookies_flg"`
}

type UpdateQueryForwardingRequest struct {
	QueryForwardingFlg bool `json:"query_forwarding_flg"`
}

type UpdateCookiesForwardingRequest struct {
	CookiesForwardingFlg bool `json:"cookies_forwarding_flg"`
}

func (s *Server) updateProxyTargets(c *gin.Context) {
	proxyID := c.Param("id")

	req, err := s.parseAndValidateRequest(c)
	if err != nil {
		return // Error already sent to client
	}

	currentProxy, err := s.getCurrentProxy(c, proxyID)
	if err != nil {
		return // Error already sent to client
	}

	targets := s.convertToTargetModels(proxyID, req)
	condition := s.convertToConditionModels(targets, req)

	if err := s.executeTransaction(c, proxyID, currentProxy, targets, condition); err != nil {
		return // Error already sent to client
	}

	if err := s.updateSupervisor(c, proxyID, currentProxy, targets, condition); err != nil {
		return // Error already sent to client
	}

	c.JSON(http.StatusOK, gin.H{"message": "proxy updated successfully"})
}

func (s *Server) updateProxySavingCookies(c *gin.Context) {
	proxyID := c.Param("id")
	var req UpdateSavingCookiesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user ID from context
	var userID *string
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			userID = &u.ID
		}
	}

	// Update saving cookies flag in storage
	if err := s.storage.UpdateProxySavingCookies(c.Request.Context(), proxyID, req.SavingCookiesFlg, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update supervisor
	cfg, err := s.storage.GetProxyConfig(c.Request.Context(), proxyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get updated proxy config: %v", err)})
		return
	}

	if err := s.supervisor.UpdateProxy(c.Request.Context(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update proxy in supervisor: %v", err)})
		return
	}

	c.Status(http.StatusOK)
}

func (s *Server) updateProxyQueryForwarding(c *gin.Context) {
	proxyID := c.Param("id")
	var req UpdateQueryForwardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user ID from context
	var userID *string
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			userID = &u.ID
		}
	}

	// Update query forwarding flag in storage
	//log.Printf("[debug] updateProxyQueryForwarding: proxyID: %s, req.QueryForwardingFlg: %v", proxyID, req.QueryForwardingFlg)
	if err := s.storage.UpdateProxyQueryForwarding(c.Request.Context(), proxyID, req.QueryForwardingFlg, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update supervisor
	cfg, err := s.storage.GetProxyConfig(c.Request.Context(), proxyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get updated proxy config: %v", err)})
		return
	}

	if err := s.supervisor.UpdateProxy(c.Request.Context(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update proxy in supervisor: %v", err)})
		return
	}

	c.Status(http.StatusOK)
}

func (s *Server) updateProxyCookiesForwarding(c *gin.Context) {
	proxyID := c.Param("id")
	var req UpdateCookiesForwardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user ID from context
	userID := s.getUserID(c)

	// Update cookies forwarding flag in storage
	// TODO: Implement UpdateProxyCookiesForwarding in storage package
	if err := s.storage.UpdateProxyQueryForwarding(c.Request.Context(), proxyID, req.CookiesForwardingFlg, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update supervisor
	cfg, err := s.storage.GetProxyConfig(c.Request.Context(), proxyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get updated proxy config: %v", err)})
		return
	}

	if err := s.supervisor.UpdateProxy(c.Request.Context(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update proxy in supervisor: %v", err)})
		return
	}

	c.Status(http.StatusOK)
}

// Request parsing and validation
func (s *Server) parseAndValidateRequest(c *gin.Context) (UpdateTargetsRequest, error) {
	var req UpdateTargetsRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return req, err
	}

	if err := s.validateCondition(c, &req); err != nil {
		return req, err
	}

	return req, nil
}

func (s *Server) validateCondition(c *gin.Context, req *UpdateTargetsRequest) error {
	if req.Condition == nil {
		return nil
	}

	if err := s.validateConditionFields(req.Condition); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return err
	}

	// todo
	//if err := s.validateConditionTargets(req); err != nil {
	//	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	//	return err
	//}

	return nil
}

func (s *Server) validateConditionFields(condition *RouteCondition) error {
	if !models.ConditionType(condition.Type).IsValid() {
		return errors.New("invalid condition type")
	}

	// For expression type, validate that we have either an Expr field or Values map
	if models.ConditionType(condition.Type) == models.ConditionTypeExpr {
		if condition.Expr == "" && len(condition.Values) == 0 {
			return errors.New("expr condition requires either Expr field or Values map with expressions")
		}
		return nil
	}

	// For non-expression types, validate that we have ParamName and Values
	if condition.ParamName == "" {
		return errors.New("param_name is required for non-expr conditions")
	}
	if len(condition.Values) == 0 {
		return errors.New("values are required for non-expr conditions")
	}
	return nil
}

func (s *Server) validateConditionTargets(req *UpdateTargetsRequest) error {
	targetIDs := make(map[string]bool)
	for _, target := range req.Targets {
		targetIDs[target.URL] = true // Assuming URL is unique we have no id of new target here
	}

	for _, targetID := range req.Condition.Values {
		if !targetIDs[targetID] {
			return fmt.Errorf("target ID %s in condition not found in targets", targetID)
		}
	}

	if req.Condition.Default != "" && !targetIDs[req.Condition.Default] {
		return errors.New("default target ID not found in targets")
	}

	return nil
}

// Helper functions
func (s *Server) getCurrentProxy(c *gin.Context, proxyID string) (*models.Proxy, error) {
	p, err := s.storage.GetProxy(c.Request.Context(), proxyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError,
			gin.H{"error": fmt.Sprintf("failed to get current proxy state: %v", err)})
		return nil, err
	}
	return p, nil
}

func (s *Server) convertToTargetModels(proxyID string, req UpdateTargetsRequest) []models.Target {
	targets := make([]models.Target, len(req.Targets))
	for i, t := range req.Targets {
		targets[i] = models.Target{
			ID:       uuid.New().String(),
			ProxyID:  proxyID,
			URL:      t.URL,
			Weight:   t.Weight,
			IsActive: t.IsActive,
		}
	}
	return targets
}

func (s *Server) convertToConditionModels(targets []models.Target, req UpdateTargetsRequest) *models.RouteCondition {
	if req.Condition == nil || req.Condition.Type == "" {
		return nil
	}

	conditionValues := make(map[string]string, len(req.Condition.Values))
	for i, v := range req.Condition.Values {
		conditionValues[targets[i].ID] = v
	}
	return &models.RouteCondition{
		Type:      models.ConditionType(req.Condition.Type),
		ParamName: req.Condition.ParamName,
		Values:    conditionValues,
		Default:   req.Condition.Default,
		Expr:      req.Condition.Expr,
	}
}

// Get user ID from request context
func (s *Server) getUserID(c *gin.Context) *string {
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			return &u.ID
		}
	}
	return nil
}

func (s *Server) executeTransaction(c *gin.Context, proxyID string,
	currentProxy *models.Proxy, targets []models.Target,
	condition *models.RouteCondition) error {

	// Get user ID from context
	var userID *string
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*models.User); ok {
			userID = &u.ID
		}
	}

	err := s.storage.UpdateProxyWithTargetsAndCondition(
		c.Request.Context(),
		proxyID,
		currentProxy,
		targets,
		condition,
		userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return err
	}

	return nil
}

// Supervisor update
func (s *Server) updateSupervisor(c *gin.Context, proxyID string, currentProxy *models.Proxy,
	targets []models.Target, condition *models.RouteCondition) error {

	config := s.buildProxyConfig(proxyID, currentProxy, targets, condition)

	if err := s.supervisor.UpdateProxy(c.Request.Context(), config); err != nil {
		c.JSON(http.StatusInternalServerError,
			gin.H{"error": fmt.Sprintf("failed to update proxy targets: %v", err)})
		return err
	}

	return nil
}

func (s *Server) buildProxyConfig(proxyID string, currentProxy *models.Proxy,
	targets []models.Target, condition *models.RouteCondition) proxy.Config {

	// Convert ListenURLs from models.ListenURL to proxy.ListenURL
	listenURLs := make([]proxy.ListenURL, len(currentProxy.ListenURLs))
	for i, url := range currentProxy.ListenURLs {
		listenURLs[i] = proxy.ListenURL{
			ID:        url.ID,
			ListenURL: url.ListenURL,
			PathKey:   url.PathKey,
		}
	}

	config := proxy.Config{
		ID:         proxyID,
		ListenURLs: listenURLs,
		Mode:       models.ProxyMode(currentProxy.Mode),
		Targets:    s.convertToConfigTargets(targets),
	}

	if condition != nil {
		config.Condition = &proxy.Condition{
			Type:      condition.Type,
			ParamName: condition.ParamName,
			Values:    condition.Values,
			Default:   condition.Default,
			Expr:      condition.Expr,
		}
	}

	return config
}

func (s *Server) convertToConfigTargets(targets []models.Target) []proxy.Target {
	configTargets := make([]proxy.Target, len(targets))
	for i, t := range targets {
		configTargets[i] = proxy.Target{
			ID:       t.ID,
			URL:      t.URL,
			Weight:   t.Weight,
			IsActive: t.IsActive,
		}
	}
	return configTargets
}
