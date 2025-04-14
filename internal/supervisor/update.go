package supervisor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ab-testing-service/internal/proxy"
)

func (s *Supervisor) DeleteProxy(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Get the instance before deleting
	instance, exists := s.proxies[id]
	if exists && instance.Proxy != nil {
		// Remove from virtual host handler
		for _, item := range instance.Proxy.Config.ListenURLs {
			host := strings.Split(item.ListenURL, ":")[0]
			if s.virtualHandler != nil {
				delete(s.virtualHandler.proxies, host)
			}
		}
	}

	// Remove from proxies map
	delete(s.proxies, id)

	return s.storage.InvalidateProxyCache(ctx, id)
}

// handleProxyUpdate is called when a proxy settings change notification is received
func (s *Supervisor) handleProxyUpdate(ctx context.Context, proxyID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Get the latest config from storage
	cfg, err := s.storage.GetProxyConfig(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	return s.UpdateProxy(ctx, cfg)
}

func (s *Supervisor) UpdateProxy(ctx context.Context, cfg proxy.Config) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	instance, exists := s.proxies[cfg.ID]
	if !exists {
		return fmt.Errorf("proxy %s not found", cfg.ID)
	}

	// Get old and new hosts
	oldHosts := make(map[string]bool)
	if instance.Proxy != nil {
		for _, lu := range instance.Proxy.Config.ListenURLs {
			host := strings.Split(lu.ListenURL, ":")[0]
			oldHosts[host] = true
		}
	}

	newHosts := make(map[string]bool)
	for _, lu := range cfg.ListenURLs {
		host := strings.Split(lu.ListenURL, ":")[0]
		newHosts[host] = true
	}

	// Вместо создания нового прокси, обновляем конфигурацию существующего
	//log.Printf("[debug] supervisor.UpdateProxy: proxyID: %s", cfg.ID)
	var newProxy *proxy.Proxy
	if instance.Proxy != nil {
		// Используем существующий прокси и обновляем его конфигурацию
		newProxy = instance.Proxy
		newProxy.UpdateConfig(cfg)
	} else {
		// Создаем новый прокси только если его не было
		var err error
		newProxy, err = proxy.NewProxy(cfg)
		if err != nil {
			return fmt.Errorf("failed to create new proxy: %w", err)
		}
	}

	// Update virtual host handler
	if s.virtualHandler != nil {
		// Removing old hosts that are not in the new config
		for oldHost := range oldHosts {
			if !newHosts[oldHost] {
				delete(s.virtualHandler.proxies, oldHost)
			}
		}

		// Adding new hosts
		for _, lu := range cfg.ListenURLs {
			host := strings.Split(lu.ListenURL, ":")[0]
			s.virtualHandler.proxies[host] = newProxy
		}
	}

	// Update the instance
	s.proxies[cfg.ID] = &ProxyInstance{
		Proxy:   newProxy,
		Started: true,
	}

	if err := s.storage.InvalidateProxyCache(ctx, cfg.ID); err != nil {
		return fmt.Errorf("failed to invalidate proxy cache: %w", err)
	}

	if err := s.storage.SaveProxyConfig(ctx, cfg); err != nil {
		return fmt.Errorf("failed to update proxy config: %w", err)
	}

	// Publish change to other instances
	if err := s.pubsub.PublishSettingsChange(ctx, cfg.ID); err != nil {
		log.Printf("Failed to publish settings change: %v", err)
	}

	return nil
}
