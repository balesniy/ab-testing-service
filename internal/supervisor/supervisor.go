package supervisor

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/ab-testing-service/internal/config"
	"github.com/ab-testing-service/internal/proxy"
	"github.com/ab-testing-service/internal/storage"
)

type ProxyInstance struct {
	Proxy   *proxy.Proxy
	Started bool
}

type Supervisor struct {
	proxies        map[string]*ProxyInstance
	config         *config.Config
	storage        *storage.Storage
	kafkaWriter    *kafka.Writer
	mutex          sync.RWMutex
	pubsub         *proxy.RedisPubSub
	server         *http.Server
	virtualHandler *VirtualHostHandler
}

type Config struct {
	Config      *config.Config
	Storage     *storage.Storage
	KafkaWriter *kafka.Writer
}

func NewSupervisor(cfg Config) *Supervisor {
	s := &Supervisor{
		proxies:     make(map[string]*ProxyInstance),
		config:      cfg.Config,
		storage:     cfg.Storage,
		kafkaWriter: cfg.KafkaWriter,
	}

	// Initialize Redis pub/sub with update callback
	s.pubsub = proxy.NewRedisPubSub(cfg.Storage.Redis, s.handleProxyUpdate)

	return s
}

func (s *Supervisor) Start(ctx context.Context) {
	// Start Redis subscriber
	if err := s.pubsub.StartSubscriber(ctx); err != nil {
		log.Printf("Failed to start Redis subscriber: %v", err)
	}

	// Load proxies configs from Postgres
	configs, err := s.storage.GetProxies(ctx)
	if err != nil {
		log.Printf("Failed to load proxy configs: %v", err)
	}

	for _, cfg := range configs {
		//targets, err := s.storage.GetTargets(ctx, cfg.ID)
		//if err != nil {
		//	log.Printf("Failed to load targets for proxy %s: %v", cfg.ID, err)
		//	continue
		//}
		//for _, t := range targets {
		//	cfg.Targets = append(cfg.Targets, proxy.Target{
		//		ID:       t.ID,
		//		URL:      t.URL,
		//		Weight:   t.Weight,
		//		IsActive: t.IsActive,
		//	})
		//}
		// Save existing proxy configurations to Redis cache
		if err := s.storage.SaveProxyConfig(ctx, cfg); err != nil {
			log.Printf("Failed to save proxy config %s: %v", cfg.ID, err)
		}
		if err := s.CreateProxy(cfg); err != nil {
			log.Printf("Failed to create proxy %s: %v", cfg.ID, err)
		}
	}

	// Start statistics collection
	go func() {
		log.Printf("Starting statistics collection")
		ticker := time.NewTicker(time.Second * 10)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.collectStatistics(ctx)
			}
		}
	}()
}

func (s *Supervisor) Shutdown(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var lastErr error

	// Shutdown the main server if it exists
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down main server: %v", err)
			lastErr = err
		}
	}

	s.kafkaWriter.Close()
	return lastErr
}
