package supervisor

import (
	"context"
	"log"
	"sort"

	"github.com/ab-testing-service/internal/proxy"
)

func (s *Supervisor) GetProxy(id string) *proxy.Proxy {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.proxies[id].Proxy
}

func (s *Supervisor) ListProxies(ctx context.Context, sortBy string, sortDesc bool) []proxy.Config {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var configs []proxy.Config

	for id, p := range s.proxies {
		tags, err := s.storage.GetTags(ctx, id)
		if err != nil {
			log.Printf("Failed to get tags for proxy %s: %v", id, err)
			continue
		}
		configs = append(configs, proxy.Config{
			ID:                   id,
			Name:                 p.Proxy.Name,
			ListenURLs:           p.Proxy.ListenURLs,
			Mode:                 p.Proxy.Mode,
			Targets:              p.Proxy.Targets,
			Condition:            p.Proxy.Config.Condition,
			Tags:                 tags,
			SavingCookiesFlg:     p.Proxy.SavingCookiesFlg,
			QueryForwardingFlg:   p.Proxy.QueryForwardingFlg,
			CookiesForwardingFlg: p.Proxy.CookiesForwardingFlg,
		})
	}

	// Sort the configs based on the sortBy parameter
	if sortBy != "" {
		sort.Slice(configs, func(i, j int) bool {
			var result bool
			switch sortBy {
			case "id":
				result = configs[i].ID < configs[j].ID
			case "listen_url":
				result = configs[i].ListenURLs[0].ListenURL < configs[j].ListenURLs[0].ListenURL
			case "name":
				result = configs[i].Name < configs[j].Name
			case "mode":
				result = configs[i].Mode < configs[j].Mode
			case "targets":
				result = len(configs[i].Targets) < len(configs[j].Targets)
			default:
				return !sortDesc // Default sort by ID
			}
			if sortDesc {
				return !result
			}
			return result
		})
	}

	return configs
}
