package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ab-testing-service/internal/models"
	"github.com/ab-testing-service/internal/proxy"
)

func (s *Storage) SaveProxyConfig(ctx context.Context, cfg proxy.Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.Redis.Set(ctx, "proxy:"+cfg.ID, data, 0).Err()
}

func (s *Storage) LoadProxyConfigs(ctx context.Context) ([]proxy.Config, error) {
	keys, err := s.Redis.Keys(ctx, "proxy:*").Result()
	if err != nil {
		return nil, err
	}

	var configs []proxy.Config
	for _, key := range keys {
		data, err := s.Redis.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var cfg proxy.Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

func (s *Storage) InvalidateProxyCache(ctx context.Context, proxyID string) error {
	// Delete the cache entry
	err := s.Redis.Del(ctx, fmt.Sprintf("proxy:%s", proxyID)).Err()
	if err != nil {
		return err
	}

	// Get the updated proxy from the database
	proxy, err := s.getProxyFromDB(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get proxy from database after invalidation: %w", err)
	}

	// Create a proxy config from the proxy model
	//log.Printf("[debug] InvalidateProxyCache: proxyID: %s", proxyID)
	cfg, err := s.createProxyConfigFromModel(proxy)
	if err != nil {
		return fmt.Errorf("failed to create proxy config from model: %w", err)
	}

	// Save the updated config back to Redis
	return s.SaveProxyConfig(ctx, cfg)
}

// Helper method to get proxy from database without checking Redis first
func (s *Storage) getProxyFromDB(ctx context.Context, id string) (*models.Proxy, error) {
	var conditionJSON models.RouteCondition
	var conditionPtr *models.RouteCondition = nil

	p, err := s.q.GetProxy(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(p.Condition) > 0 {
		if err := json.Unmarshal(p.Condition, &conditionJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal condition: %w", err)
		}
		conditionPtr = &conditionJSON // Only set pointer if we have condition data
	}

	proxyModel := models.Proxy{
		ID:                   p.ID,
		Name:                 *p.Name,
		Mode:                 models.ProxyMode(p.Mode),
		Condition:            conditionPtr,
		Tags:                 p.Tags,
		CreatedAt:            p.CreatedAt.Time,
		UpdatedAt:            p.UpdatedAt.Time,
		SavingCookiesFlg:     p.SavingCookiesFlg,
		QueryForwardingFlg:   p.QueryForwardingFlg,
		CookiesForwardingFlg: p.CookiesForwardingFlg,
	}

	// Get listen URLs
	listenURLs, err := s.q.GetProxyListenURLs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get listen URLs: %w", err)
	}

	for _, url := range listenURLs {
		proxyModel.ListenURLs = append(proxyModel.ListenURLs, models.ListenURL{
			ID:        url.ID,
			ProxyID:   url.ProxyID,
			ListenURL: url.ListenUrl,
			PathKey:   url.PathKey,
			CreatedAt: url.CreatedAt.Time,
			UpdatedAt: url.UpdatedAt.Time,
		})
	}

	// Get targets
	targets, err := s.q.GetTargetsByProxyID(ctx, id)
	if err != nil {
		return nil, err
	}

	for _, target := range targets {
		target := models.Target{
			ProxyID:  p.ID,
			ID:       target.ID,
			URL:      target.Url,
			Weight:   target.Weight,
			IsActive: target.IsActive,
		}
		proxyModel.Targets = append(proxyModel.Targets, target)
	}

	return &proxyModel, nil
}

// Helper method to create a proxy config from a proxy model
func (s *Storage) createProxyConfigFromModel(p *models.Proxy) (proxy.Config, error) {
	// Convert ListenURLs from models.ListenURL to proxy.ListenURL
	listenURLs := make([]proxy.ListenURL, len(p.ListenURLs))
	for i, url := range p.ListenURLs {
		listenURLs[i] = proxy.ListenURL{
			ID:        url.ID,
			ListenURL: url.ListenURL,
			PathKey:   url.PathKey,
		}
	}

	// Convert Targets from models.Target to proxy.Target
	targets := make([]proxy.Target, len(p.Targets))
	for i, t := range p.Targets {
		targets[i] = proxy.Target{
			ID:       t.ID,
			URL:      t.URL,
			Weight:   t.Weight,
			IsActive: t.IsActive,
		}
	}

	config := proxy.Config{
		ID:                   p.ID,
		Name:                 p.Name,
		Mode:                 p.Mode,
		Tags:                 p.Tags,
		SavingCookiesFlg:     p.SavingCookiesFlg,
		QueryForwardingFlg:   p.QueryForwardingFlg,
		CookiesForwardingFlg: p.CookiesForwardingFlg,
		ListenURLs:           listenURLs,
		Targets:              targets,
	}

	//fmt.Printf("DEBUG [createProxyConfigFromModel]: Proxy ID: %s, Condition pointer is nil? %v\n", p.ID, p.Condition == nil)

	if p.Condition != nil {
		//fmt.Printf("DEBUG [createProxyConfigFromModel]: Condition Type: '%s'\n", p.Condition.Type)
		condition, err := convertCondition(p.Condition)
		if err != nil {
			return proxy.Config{}, fmt.Errorf("failed to convert condition: %w", err)
		}
		config.Condition = condition
	}

	return config, nil
}

func (s *Storage) GetProxyConfig(ctx context.Context, proxyID string) (proxy.Config, error) {
	data, err := s.Redis.Get(ctx, fmt.Sprintf("proxy:%s", proxyID)).Bytes()
	if err != nil {
		return proxy.Config{}, err
	}

	var cfg proxy.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return proxy.Config{}, err
	}
	return cfg, nil
}

func (s *Storage) GetProxy(ctx context.Context, id string) (*models.Proxy, error) {
	// Try Redis first
	key := fmt.Sprintf("proxy:%s", id)
	data, err := s.Redis.Get(ctx, key).Bytes()
	if err == nil {
		var proxy models.Proxy
		if err := json.Unmarshal(data, &proxy); err == nil {
			return &proxy, nil
		}
	}

	// Fallback to PostgreSQL
	var conditionJSON models.RouteCondition
	var conditionPtr *models.RouteCondition = nil

	p, err := s.q.GetProxy(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(p.Condition) > 0 {
		if err := json.Unmarshal(p.Condition, &conditionJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal condition: %w", err)
		}
		conditionPtr = &conditionJSON
	}

	proxyModel := models.Proxy{
		ID:        p.ID,
		Mode:      models.ProxyMode(p.Mode),
		Condition: conditionPtr,
		Tags:      p.Tags,
		CreatedAt: p.CreatedAt.Time,
		UpdatedAt: p.UpdatedAt.Time,
	}

	// Get listen URLs
	listenURLs, err := s.q.GetProxyListenURLs(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get listen URLs: %w", err)
	}

	for _, url := range listenURLs {
		proxyModel.ListenURLs = append(proxyModel.ListenURLs, models.ListenURL{
			ID:        url.ID,
			ProxyID:   url.ProxyID,
			ListenURL: url.ListenUrl,
			PathKey:   url.PathKey,
			CreatedAt: url.CreatedAt.Time,
			UpdatedAt: url.UpdatedAt.Time,
		})
	}

	// Get targets
	targets, err := s.q.GetTargetsByProxyID(ctx, id)
	if err != nil {
		return nil, err
	}

	for _, target := range targets {
		target := models.Target{
			ProxyID:  p.ID,
			ID:       target.ID,
			URL:      target.Url,
			Weight:   target.Weight,
			IsActive: target.IsActive,
		}
		proxyModel.Targets = append(proxyModel.Targets, target)
	}

	// Cache in Redis
	if data, err := json.Marshal(proxyModel); err == nil {
		s.Redis.Set(ctx, key, data, proxyTTL)
	}

	return &proxyModel, nil
}

func (s *Storage) GetTargets(ctx context.Context, proxyID string) ([]*models.Target, error) {
	// Try Redis first
	key := fmt.Sprintf("targets:%s", proxyID)

	data, err := s.Redis.Get(ctx, key).Bytes()
	if err == nil {
		var targets []*models.Target
		if err := json.Unmarshal(data, &targets); err == nil {
			return targets, nil
		}
	}

	// Fallback to PostgreSQL
	rows, err := s.q.GetTargetsByProxyID(ctx, proxyID)
	if err != nil {
		return nil, err
	}

	targets := make([]*models.Target, 0, len(rows))

	for _, item := range rows {
		targets = append(targets, &models.Target{
			ID:       item.ID,
			ProxyID:  proxyID,
			URL:      item.Url,
			Weight:   item.Weight,
			IsActive: item.IsActive,
		})
	}

	// Cache in Redis
	if data, err := json.Marshal(targets); err == nil {
		s.Redis.Set(ctx, key, data, targetTTL)
	}

	return targets, nil
}
