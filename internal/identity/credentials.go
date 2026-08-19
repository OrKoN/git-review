package identity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type credentialSet struct {
	Default string                 `json:"default"`
	Hubs    map[string]Credentials `json:"hubs"`
}

func DefaultCredentialsPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "git-review", "credentials.json")
}

func LoadCredentials(path string) (Credentials, error) { return LoadCredentialsForHub(path, "") }

func LoadCredentialsForHub(path, hubURL string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var set credentialSet
	if json.Unmarshal(data, &set) != nil || len(set.Hubs) == 0 {
		return Credentials{}, errors.New("invalid credentials")
	}
	if hubURL == "" {
		hubURL = set.Default
	}
	value, ok := set.Hubs[hubURL]
	if !ok || value.CertPEM == "" || value.KeyPEM == "" || value.CAPEM == "" {
		return Credentials{}, errors.New("no enrolled credentials for hub")
	}
	return value, nil
}

func SaveCredentials(path string, value Credentials) error {
	set := credentialSet{Hubs: map[string]Credentials{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &set)
		if set.Hubs == nil {
			set.Hubs = map[string]Credentials{}
		}
	}
	set.Default = value.HubURL
	set.Hubs[value.HubURL] = value
	return atomicWrite(path, set)
}
