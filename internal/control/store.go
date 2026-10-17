package control

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/neluckoff/proxyfleet/internal/model"
)

type store struct {
	mu    sync.RWMutex
	path  string
	state model.State
}

func openStore(path string) (*store, error) {
	s := &store{path: path, state: model.State{Nodes: []*model.Node{}, Users: []*model.ProxyUser{}, PushSubscriptions: []*model.PushSubscription{}, Events: []*model.Event{}, Activity: []*model.ActivitySample{}, Traffic: []*model.TrafficSample{}}}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, s.persistLocked()
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
	}
	if s.state.Nodes == nil {
		s.state.Nodes = []*model.Node{}
	}
	if s.state.Users == nil {
		s.state.Users = []*model.ProxyUser{}
	}
	migrated := false
	for _, node := range s.state.Nodes {
		if node.WebProxyPort == 0 {
			if node.LegacyMTProtoPort != 0 {
				node.WebProxyPort = node.LegacyMTProtoPort
			} else {
				node.WebProxyPort = 8443
			}
			migrated = true
		}
		if node.LegacyMTProtoPort != 0 {
			node.LegacyMTProtoPort = 0
			migrated = true
		}
	}
	for _, user := range s.state.Users {
		if user.Source == "" {
			user.Source = "managed"
		}
		if user.Protocol == "mtproto" {
			user.Protocol = "webproxy"
			if user.WebProxySecretCiphertext == "" {
				user.WebProxySecretCiphertext = user.LegacyMTProtoSecret
			}
			for _, nodeID := range user.NodeIDs {
				if node := findNode(&s.state, nodeID); node != nil {
					node.ConfigVersion++
				}
			}
			migrated = true
		}
		if user.LegacyMTProtoSecret != "" {
			user.LegacyMTProtoSecret = ""
			migrated = true
		}
		if user.Protocol != "http" && user.Protocol != "socks5" && user.Protocol != "webproxy" {
			// Legacy credentials predate the one-proxy/one-protocol model. Keep them
			// usable through the HTTP listener instead of exposing two endpoints.
			user.Protocol = "http"
		}
	}
	if s.state.PushSubscriptions == nil {
		s.state.PushSubscriptions = []*model.PushSubscription{}
	}
	if s.state.Events == nil {
		s.state.Events = []*model.Event{}
	}
	if s.state.Activity == nil {
		s.state.Activity = []*model.ActivitySample{}
	}
	if s.state.Traffic == nil {
		s.state.Traffic = []*model.TrafficSample{}
	}
	if s.state.TrafficBytesUp == 0 && s.state.TrafficBytesDown == 0 {
		for _, node := range s.state.Nodes {
			s.state.TrafficBytesUp += node.BytesUp
			s.state.TrafficBytesDown += node.BytesDown
		}
	}
	if migrated {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *store) snapshot() model.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, _ := json.Marshal(s.state)
	var result model.State
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *store) update(fn func(*model.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	if len(s.state.Events) > 500 {
		s.state.Events = s.state.Events[len(s.state.Events)-500:]
	}
	return s.persistLocked()
}

func (s *store) persistLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
