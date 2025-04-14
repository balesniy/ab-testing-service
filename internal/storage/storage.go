package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ab-testing-service/internal/models"
	"github.com/ab-testing-service/internal/proxy"
)

type Storage struct {
	q     Querier
	db    *pgxpool.Pool
	Redis *redis.Client
}

const (
	proxyTTL  = 1 * time.Hour
	targetTTL = 1 * time.Hour
)

func NewStorage(conn *pgxpool.Pool, redis *redis.Client) *Storage {
	return &Storage{
		q:     New(conn),
		db:    conn,
		Redis: redis,
	}
}

func (s *Storage) SaveVisit(ctx context.Context, visit *models.Visit) error {
	visit.ID = uuid.New().String()
	visit.CreatedAt = time.Now()

	_, err := s.db.Exec(ctx,
		`INSERT INTO visits (id, proxy_id, target_id, user_id, rid, rrid, ruid, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		visit.ID, visit.ProxyID, visit.TargetID, visit.UserID,
		visit.RID, visit.RRID, visit.RUID, visit.CreatedAt,
	)
	return err
}

func (s *Storage) GetProxies(ctx context.Context) ([]proxy.Config, error) {
	var proxies []proxy.Config

	// Replace raw SQL query with the SQLC generated method
	rows, err := s.q.GetProxies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query proxies: %w", err)
	}

	for _, p := range rows {
		var proxyModel models.Proxy
		proxyModel.ID = p.ID
		proxyModel.Name = *p.Name
		proxyModel.Mode = models.ProxyMode(p.Mode)
		proxyModel.Tags = p.Tags
		proxyModel.SavingCookiesFlg = p.SavingCookiesFlg
		proxyModel.QueryForwardingFlg = p.QueryForwardingFlg
		proxyModel.CookiesForwardingFlg = p.CookiesForwardingFlg

		if len(p.Condition) > 0 {
			proxyModel.Condition = &models.RouteCondition{}
			if err := json.Unmarshal(p.Condition, proxyModel.Condition); err != nil {
				return nil, fmt.Errorf("failed to unmarshal condition: %w", err)
			}
		}

		// Fetch ListenURLs from proxy_listen_urls table
		listenURLs, err := s.q.GetProxyListenURLs(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get listen URLs for proxy %s: %w", p.ID, err)
		}

		// Map ListenURLs to the models.Proxy structure
		for _, listenURL := range listenURLs {
			proxyModel.ListenURLs = append(proxyModel.ListenURLs, models.ListenURL{
				ID:        listenURL.ID,
				ProxyID:   listenURL.ProxyID,
				ListenURL: listenURL.ListenUrl,
				PathKey:   listenURL.PathKey,
				CreatedAt: listenURL.CreatedAt.Time,
				UpdatedAt: listenURL.UpdatedAt.Time,
			})
		}

		// Fetch targets
		//targets, err := s.q.GetTargetsByProxyID(ctx, p.ID)
		targets, err := s.GetTargets(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get targets for proxy %s: %w", p.ID, err)
		}

		// Map targets to the models.Proxy structure
		for _, target := range targets {
			proxyModel.Targets = append(proxyModel.Targets, models.Target{
				ID:       target.ID,
				ProxyID:  p.ID,
				URL:      target.URL,
				Weight:   target.Weight,
				IsActive: target.IsActive,
			})
		}

		// Use the helper function to create proxy config
		config, err := s.createProxyConfigFromModel(&proxyModel)
		if err != nil {
			log.Printf("Failed to convert proxy model to config for proxy %s: %v", p.ID, err)
			// Skip this proxy and continue with others
			continue
		}

		proxies = append(proxies, config)
	}
	return proxies, nil
}

// Безопасное приведение типов с обработкой ошибок
func convertCondition(rc *models.RouteCondition) (*proxy.Condition, error) {
	if rc == nil {
		return nil, nil // Если входной параметр nil, возвращаем nil без ошибки
	}

	// Проверяем поля на корректность
	if !rc.Type.IsValid() {
		return nil, fmt.Errorf("invalid condition type: %v", rc.Type)
	}

	// Создаем новый объект Condition
	condition := &proxy.Condition{
		Type:      rc.Type,
		ParamName: rc.ParamName,
		Values:    make(map[string]string),
		Default:   rc.Default,
		Expr:      rc.Expr,
	}

	// Копируем значения map, проверяя их валидность
	for k, v := range rc.Values {
		if k == "" {
			return nil, fmt.Errorf("empty key in Values map")
		}
		condition.Values[k] = v
	}

	return condition, nil
}
