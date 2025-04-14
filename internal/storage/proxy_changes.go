package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ab-testing-service/internal/models"
)

const (
	ChangeTypeTargetsUpdate         models.ChangeType = "targets_update"
	ChangeTypeConditionUpdate       models.ChangeType = "condition_update"
	ChangeTypeURLUpdate             models.ChangeType = "url_update"
	ChangeTypeCookiesUpdate         models.ChangeType = "cookies_update"
	ChangeTypeQueryForwardingUpdate models.ChangeType = "query_forwarding_update"
)

func (s *Storage) GetProxyChanges(ctx context.Context, proxyID string, limit, offset int) ([]models.ProxyChange, error) {
	rows, err := s.q.GetProxyChangesByProxyID(ctx, &GetProxyChangesByProxyIDParams{
		ProxyID: proxyID,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query proxy changes: %w", err)
	}

	var changes []models.ProxyChange

	for _, change := range rows {
		changes = append(changes, models.ProxyChange{
			ID:            change.ID,
			ProxyID:       change.ProxyID,
			ChangeType:    models.ChangeType(change.ChangeType),
			PreviousState: change.PreviousState,
			NewState:      change.NewState,
			CreatedAt:     change.CreatedAt.Time,
			CreatedBy:     change.CreatedBy,
		})
	}

	return changes, nil
}

func (s *Storage) UpdateProxyURL(ctx context.Context, proxyID string, listenURL string, pathKey *string, createdBy *string) error {
	// Verify proxy exists
	_, err := s.GetProxy(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get proxy: %w", err)
	}

	// Get current listen URLs
	listenURLs, err := s.q.GetProxyListenURLs(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get proxy listen URLs: %w", err)
	}

	// Check if there are any listen URLs
	if len(listenURLs) == 0 {
		// Create a new listen URL if none exist
		return s.createNewListenURL(ctx, proxyID, listenURL, pathKey, createdBy)
	}

	// Use the first listen URL as the primary one to update
	currentURL := listenURLs[0] // fixme

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"id":         currentURL.ID,
		"listen_url": currentURL.ListenUrl,
		"path_key":   currentURL.PathKey,
	}
	newState := map[string]interface{}{
		"id":         currentURL.ID,
		"listen_url": listenURL,
		"path_key":   pathKey,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Update listen URL
		err = q.UpdateProxyListenURL(ctx, &UpdateProxyListenURLParams{
			ListenUrl: listenURL,
			PathKey:   pathKey,
			ID:        currentURL.ID,
		})
		if err != nil {
			return fmt.Errorf("failed to update listen URL: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

// Helper function to create a new listen URL for a proxy
func (s *Storage) createNewListenURL(ctx context.Context, proxyID string, listenURL string, pathKey *string, createdBy *string) error {
	// Prepare new state for logging
	newState := map[string]interface{}{
		"listen_url": listenURL,
		"path_key":   pathKey,
	}

	// Marshal state to JSON
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Create a new listen URL
		urlID := uuid.New().String()
		now := time.Now()
		err = q.CreateProxyListenURL(ctx, &CreateProxyListenURLParams{
			ID:        urlID,
			ProxyID:   proxyID,
			ListenUrl: listenURL,
			PathKey:   pathKey,
			CreatedAt: pgtype.Timestamptz{Time: now},
			UpdatedAt: pgtype.Timestamptz{Time: now},
		})
		if err != nil {
			return fmt.Errorf("failed to create listen URL: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: nil, // No previous state for a new URL
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: now},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) UpdateProxyCondition(ctx context.Context, proxyID string, condition *models.RouteCondition, createdBy *string) error {
	// Get current proxy state
	currentProxy, err := s.GetProxy(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get current proxy state: %w", err)
	}

	// Marshal condition to JSON
	conditionJSON, err := json.Marshal(condition)
	if err != nil {
		return fmt.Errorf("failed to marshal condition: %w", err)
	}

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"condition": currentProxy.Condition,
	}
	newState := map[string]interface{}{
		"condition": condition,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Update condition
		err = q.UpdateProxyCondition(ctx, &UpdateProxyConditionParams{
			Condition: conditionJSON,
			ID:        proxyID,
		})
		if err != nil {
			return fmt.Errorf("failed to update proxy condition: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeConditionUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) UpdateProxyWithTargetsAndCondition(ctx context.Context, proxyID string, currentProxy *models.Proxy,
	targets []models.Target, condition *models.RouteCondition, createdBy *string) error {

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"targets":   currentProxy.Targets,
		"condition": currentProxy.Condition,
	}
	newState := map[string]interface{}{
		"targets":   targets,
		"condition": condition,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Delete existing targets
		err = q.DeleteTargetByProxyID(ctx, proxyID)
		if err != nil {
			return fmt.Errorf("failed to delete existing targets: %w", err)
		}

		// Create new targets
		for _, target := range targets {
			err = q.CreateTarget(ctx, &CreateTargetParams{
				ID:       target.ID,
				ProxyID:  proxyID,
				Url:      target.URL,
				Weight:   target.Weight,
				IsActive: target.IsActive,
			})
			if err != nil {
				return fmt.Errorf("failed to create target: %w", err)
			}
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeTargetsUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) AddProxyListenURL(ctx context.Context, proxyID string, listenURL string, pathKey *string, createdBy *string) error {
	// Get current proxy state
	currentProxy, err := s.GetProxy(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get current proxy state: %w", err)
	}

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"listen_urls": currentProxy.ListenURLs,
	}

	// Create new listen URL
	newListenURL := models.ListenURL{
		ID:        uuid.New().String(),
		ProxyID:   proxyID,
		ListenURL: listenURL,
		PathKey:   pathKey,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Add new URL to the list
	newListenURLs := append(currentProxy.ListenURLs, newListenURL)
	newState := map[string]interface{}{
		"listen_urls": newListenURLs,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Create new listen URL
		err = q.CreateProxyListenURL(ctx, &CreateProxyListenURLParams{
			ID:        newListenURL.ID,
			ProxyID:   proxyID,
			ListenUrl: listenURL,
			PathKey:   pathKey,
			CreatedAt: pgtype.Timestamptz{Time: newListenURL.CreatedAt},
			UpdatedAt: pgtype.Timestamptz{Time: newListenURL.UpdatedAt},
		})
		if err != nil {
			return fmt.Errorf("failed to create listen URL: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) UpdateProxyListenURL(ctx context.Context, urlID string, listenURL string, pathKey *string, createdBy *string) error {
	// Get current proxy state
	rows, err := s.q.GetProxyListenURLs(ctx, urlID)
	if err != nil {
		return fmt.Errorf("failed to get listen URL: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("listen URL not found")
	}
	currentURL := rows[0]

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"id":         currentURL.ID,
		"listen_url": currentURL.ListenUrl,
		"path_key":   currentURL.PathKey,
	}
	newState := map[string]interface{}{
		"id":         currentURL.ID,
		"listen_url": listenURL,
		"path_key":   pathKey,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Update listen URL
		err = q.UpdateProxyListenURL(ctx, &UpdateProxyListenURLParams{
			ListenUrl: listenURL,
			PathKey:   pathKey,
			ID:        urlID,
		})
		if err != nil {
			return fmt.Errorf("failed to update listen URL: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       currentURL.ProxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, currentURL.ProxyID)
}

func (s *Storage) DeleteProxyListenURL(ctx context.Context, urlID string, createdBy *string) error {
	// Get current proxy state
	rows, err := s.q.GetProxyListenURLs(ctx, urlID)
	if err != nil {
		return fmt.Errorf("failed to get listen URL: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("listen URL not found")
	}
	currentURL := rows[0]

	// Prepare state for change record
	previousState := map[string]interface{}{
		"id":         currentURL.ID,
		"listen_url": currentURL.ListenUrl,
		"path_key":   currentURL.PathKey,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(map[string]interface{}{})
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Delete listen URL
		err = q.DeleteProxyListenURL(ctx, urlID)
		if err != nil {
			return fmt.Errorf("failed to delete listen URL: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       currentURL.ProxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, currentURL.ProxyID)
}

func (s *Storage) UpdateProxySavingCookies(ctx context.Context, proxyID string, savingCookies bool, createdBy *string) error {
	// Get current proxy state
	currentProxy, err := s.GetProxy(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get current proxy state: %w", err)
	}

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"saving_cookies_flg": currentProxy.SavingCookiesFlg,
	}
	newState := map[string]interface{}{
		"saving_cookies_flg": savingCookies,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Update saving cookies flag
		err = q.UpdateProxySavingCookies(ctx, &UpdateProxySavingCookiesParams{
			SavingCookiesFlg: savingCookies,
			ID:               proxyID,
		})
		if err != nil {
			return fmt.Errorf("failed to update saving cookies flag: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeCookiesUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) UpdateProxyQueryForwarding(ctx context.Context, proxyID string, queryForwarding bool, createdBy *string) error {
	// Get current proxy state
	currentProxy, err := s.GetProxy(ctx, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get current proxy state: %w", err)
	}

	// Prepare previous and new states
	previousState := map[string]interface{}{
		"query_forwarding_flg": currentProxy.QueryForwardingFlg,
	}
	newState := map[string]interface{}{
		"query_forwarding_flg": queryForwarding,
	}

	// Marshal states to JSON
	previousStateJSON, err := json.Marshal(previousState)
	if err != nil {
		return fmt.Errorf("failed to marshal previous state: %w", err)
	}
	newStateJSON, err := json.Marshal(newState)
	if err != nil {
		return fmt.Errorf("failed to marshal new state: %w", err)
	}

	// Begin transaction
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := New(tx)

		// Update query forwarding flag
		err = q.UpdateProxyQueryForwarding(ctx, &UpdateProxyQueryForwardingParams{
			QueryForwardingFlg: queryForwarding,
			ID:                 proxyID,
		})
		if err != nil {
			return fmt.Errorf("failed to update query forwarding flag: %w", err)
		}

		// Create change record
		err = q.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeQueryForwardingUpdate),
			PreviousState: previousStateJSON,
			NewState:      newStateJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		})
		if err != nil {
			return fmt.Errorf("failed to create proxy change record: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Invalidate cache
	//log.Printf("[debug] UpdateProxyQueryForwarding: proxyID: %s", proxyID)

	return s.InvalidateProxyCache(ctx, proxyID)
}

func (s *Storage) UpdateProxyCookiesForwarding(ctx context.Context, proxyID string, cookiesForwarding bool, createdBy *string) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		repo := New(tx)

		// Get current proxy state for change tracking
		p, err := repo.GetProxy(ctx, proxyID)
		if err != nil {
			return fmt.Errorf("failed to get current proxy state: %w", err)
		}

		// Create change record
		previousState := map[string]interface{}{
			"cookies_forwarding_flg": p.CookiesForwardingFlg,
		}
		newState := map[string]interface{}{
			"cookies_forwarding_flg": cookiesForwarding,
		}

		previousJSON, err := json.Marshal(previousState)
		if err != nil {
			return fmt.Errorf("failed to marshal previous state: %w", err)
		}

		newJSON, err := json.Marshal(newState)
		if err != nil {
			return fmt.Errorf("failed to marshal new state: %w", err)
		}

		if err = repo.CreateProxyChange(ctx, &CreateProxyChangeParams{
			ID:            uuid.New().String(),
			ProxyID:       proxyID,
			ChangeType:    string(models.ChangeTypeURLUpdate),
			PreviousState: previousJSON,
			NewState:      newJSON,
			CreatedAt:     pgtype.Timestamptz{Time: time.Now()},
			CreatedBy:     createdBy,
		}); err != nil {
			return fmt.Errorf("failed to record cookies forwarding changes: %w", err)
		}

		// Update the cookies forwarding flag
		if err = repo.UpdateProxyCookiesForwarding(ctx, &UpdateProxyCookiesForwardingParams{
			ID:                   proxyID,
			CookiesForwardingFlg: cookiesForwarding,
		}); err != nil {
			return fmt.Errorf("failed to update cookies forwarding flag: %w", err)
		}

		return nil
	})

	// Clear cache
	if err == nil {
		key := fmt.Sprintf("proxy:%s", proxyID)
		s.Redis.Del(ctx, key)
	}

	return err
}
