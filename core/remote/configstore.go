package remote

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
)

// ConfigStore is a config.Store over the server's GET /config: config.X.Get() answers the
// server's value on a server. It only has the server's side keys, what the server shows this
// login (a risky key reads as the default for a non-admin). Read-only, Refresh reads it again.
type ConfigStore struct {
	c      *Client
	mu     sync.Mutex
	values map[string]string
	rev    int64
}

func (s *ConfigStore) All() (map[string]string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values, s.rev, nil
}

func (s *ConfigStore) Revision() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rev, nil
}

func (s *ConfigStore) Set(string, *string, bool, string) error {
	return errors.New("a server's settings aren't written here")
}

func (s *ConfigStore) Imported() (bool, error) { return true, nil }

// Refresh reads the server's settings again, config answers them after it.
func (s *ConfigStore) Refresh(ctx context.Context) error {
	entries, err := remoteConfig{s.c}.List(ctx)
	if err != nil {
		return err
	}
	values := make(map[string]string, len(entries))
	for _, e := range entries {
		if config.Side(e.Key) != config.SideServer || e.Secret {
			continue
		}
		text, err := json.Marshal(e.Value)
		if err != nil {
			return err
		}
		values[e.Key] = string(text)
	}
	s.mu.Lock()
	s.values = values
	s.rev++
	s.mu.Unlock()
	config.Refresh()
	return nil
}

// ReadConfig makes config answer the server's keys. Settings that don't load leave the
// defaults: every action is the server's anyway, it only changes what's shown.
func (c *Client) ReadConfig(ctx context.Context) error {
	s := &ConfigStore{c: c}
	if err := s.Refresh(ctx); err != nil {
		log.Warn("the settings of %s: %s", Name(c.URL), err)
	}
	c.Config = s
	return config.UseServer(s, Name(c.URL))
}
