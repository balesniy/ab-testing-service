package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/ab-testing-service/internal/models"
)

type Target struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Weight   float64 `json:"weight"`
	IsActive bool    `json:"is_active"`
}

type Config struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	ListenURLs           []ListenURL      `json:"listen_urls"`
	Mode                 models.ProxyMode `json:"mode"`
	Targets              []Target         `json:"targets"`
	Condition            *Condition       `json:"condition"`
	Tags                 []string         `json:"tags"`
	SavingCookiesFlg     bool             `json:"saving_cookies_flg"`
	QueryForwardingFlg   bool             `json:"query_forwarding_flg"`
	CookiesForwardingFlg bool             `json:"cookies_forwarding_flg"`
}

type ListenURL struct {
	ID        string  `json:"id"`
	ListenURL string  `json:"listen_url"`
	PathKey   *string `json:"path_key,omitempty"`
}

type Condition struct {
	Type      models.ConditionType `json:"type"`
	ParamName string               `json:"param_name"`
	Values    map[string]string    `json:"values"`
	Default   string               `json:"default"`
	Expr      string               `json:"expr,omitempty"`
}

type RedirectInfo struct {
	RID     string // Redirect ID (same for all users within proxy)
	RRID    string // Redirect Request ID (unique per click)
	RUID    string // Redirect User ID (unique per user)
	Query   url.Values
	Cookies []*http.Cookie
}

type Proxy struct {
	ID                   string
	Name                 string
	ListenURLs           []ListenURL
	Mode                 models.ProxyMode
	Targets              []Target
	Config               Config
	SavingCookiesFlg     bool
	QueryForwardingFlg   bool
	CookiesForwardingFlg bool
	mutex                sync.RWMutex
	metrics              *Metrics
	cookieName           string
	stats                *Stats
}

func NewProxy(cfg Config) (*Proxy, error) {
	totalWeight, err := validate(cfg)
	if err != nil {
		return nil, err
	}

	// Normalize weights if total is not 1
	if totalWeight != 1 && totalWeight != 0 {
		for i := range cfg.Targets {
			cfg.Targets[i].Weight = cfg.Targets[i].Weight / totalWeight
		}
	}

	proxy := &Proxy{
		ID:                   cfg.ID,
		Name:                 cfg.Name,
		ListenURLs:           cfg.ListenURLs,
		Mode:                 cfg.Mode,
		Targets:              cfg.Targets,
		Config:               cfg,
		SavingCookiesFlg:     cfg.SavingCookiesFlg,
		QueryForwardingFlg:   cfg.QueryForwardingFlg,
		CookiesForwardingFlg: cfg.CookiesForwardingFlg,
		metrics:              newProxyMetrics(cfg.ID),
		cookieName:           fmt.Sprintf("proxy_%s", cfg.ID),
		stats:                NewProxyStats(cfg.ID),
	}

	return proxy, nil
}

func (p *Proxy) UpdateConfig(cfg Config) {
	p.Config = cfg
	p.Name = cfg.Name
	p.ListenURLs = cfg.ListenURLs
	p.Mode = cfg.Mode
	p.Targets = cfg.Targets
	p.SavingCookiesFlg = cfg.SavingCookiesFlg
	p.QueryForwardingFlg = cfg.QueryForwardingFlg
	p.CookiesForwardingFlg = cfg.CookiesForwardingFlg
	//log.Printf("[debug] proxy config updated: %s", cfg)
}

func validate(cfg Config) (float64, error) {
	if cfg.ID == "" {
		return 0, fmt.Errorf("proxy ID is required")
	}
	if len(cfg.ListenURLs) == 0 {
		return 0, fmt.Errorf("at least one listen URL is required")
	}
	if len(cfg.Targets) == 0 {
		return 0, fmt.Errorf("at least one target is required")
	}

	// Validate and normalize target weights
	var totalWeight float64
	for _, t := range cfg.Targets {
		if t.Weight < 0 {
			return 0, fmt.Errorf("target weight must be non-negative")
		}
		totalWeight += t.Weight
	}
	return totalWeight, nil
}

func (p *Proxy) UpdateTargets(targets []Target) {
	p.mutex.Lock()
	p.Targets = targets
	p.mutex.Unlock()
}

func (p *Proxy) GetStats() *Stats {
	return p.stats
}
