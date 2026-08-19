package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const ServerName = "git-review-hub"

type Enrollment struct {
	HubURL  string `json:"hubUrl"`
	Tunnel  string `json:"tunnel"`
	CAPEM   string `json:"ca"`
	Code    string `json:"code"`
	Expires int64  `json:"expires"`
}

type Credentials struct {
	HubURL  string `json:"hubUrl"`
	Tunnel  string `json:"tunnel"`
	CAPEM   string `json:"ca"`
	CertPEM string `json:"cert"`
	KeyPEM  string `json:"key"`
	HostID  string `json:"hostId"`
}

type hostRecord struct {
	Name    string `json:"name"`
	Revoked bool   `json:"revoked,omitempty"`
}

type pendingRecord struct {
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Expires int64  `json:"expires"`
}

type state struct {
	CAPEM     string                   `json:"caCert"`
	CAKeyPEM  string                   `json:"caKey"`
	ServerPEM string                   `json:"serverCert"`
	ServerKey string                   `json:"serverKey"`
	Hosts     map[string]hostRecord    `json:"hosts"`
	Pending   map[string]pendingRecord `json:"pending"`
}

type Store struct{ Path string }

func DefaultStatePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "git-review-hub", "identity.json")
}

func (s Store) Ensure() error {
	return s.update(func(value *state) error { return nil })
}

func (s Store) TLSCertificate() (tlsCertPEM, tlsKeyPEM, caPEM []byte, err error) {
	value, err := s.read()
	if err != nil {
		return nil, nil, nil, err
	}
	return []byte(value.ServerPEM), []byte(value.ServerKey), []byte(value.CAPEM), nil
}

func (s Store) CreateEnrollment(name, hubURL, tunnel string, now time.Time) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}
	id := hex.EncodeToString(idBytes)
	expires := now.Add(10 * time.Minute)
	var ca string
	err := s.update(func(value *state) error {
		ca = value.CAPEM
		for key, item := range value.Pending {
			if item.Expires < now.Unix() {
				delete(value.Pending, key)
			}
		}
		hash := sha256.Sum256([]byte(code))
		value.Pending[id] = pendingRecord{Hash: hex.EncodeToString(hash[:]), Name: name, Expires: expires.Unix()}
		return nil
	})
	if err != nil {
		return "", err
	}
	bundle, _ := json.Marshal(Enrollment{HubURL: hubURL, Tunnel: tunnel, CAPEM: ca, Code: id + "." + code, Expires: expires.Unix()})
	return "gr1:" + base64.RawURLEncoding.EncodeToString(bundle), nil
}

func ParseEnrollment(bundle string) (Enrollment, error) {
	if len(bundle) < 5 || bundle[:4] != "gr1:" {
		return Enrollment{}, errors.New("invalid enrollment bundle")
	}
	data, err := base64.RawURLEncoding.DecodeString(bundle[4:])
	if err != nil {
		return Enrollment{}, errors.New("invalid enrollment bundle")
	}
	var value Enrollment
	if json.Unmarshal(data, &value) != nil || value.HubURL == "" || value.Tunnel == "" || value.Code == "" || value.CAPEM == "" {
		return Enrollment{}, errors.New("invalid enrollment bundle")
	}
	if time.Now().Unix() > value.Expires {
		return Enrollment{}, errors.New("enrollment bundle expired")
	}
	return value, nil
}

func (s Store) SignEnrollment(code string, csrDER []byte, now time.Time) (hostID string, certPEM []byte, err error) {
	parts := splitCode(code)
	if len(parts) != 2 {
		return "", nil, errors.New("invalid enrollment code")
	}
	err = s.update(func(value *state) error {
		pending, ok := value.Pending[parts[0]]
		hash := sha256.Sum256([]byte(parts[1]))
		if !ok || pending.Expires < now.Unix() || subtle.ConstantTimeCompare([]byte(pending.Hash), []byte(hex.EncodeToString(hash[:]))) != 1 {
			return errors.New("invalid or expired enrollment code")
		}
		delete(value.Pending, parts[0])
		csr, parseErr := x509.ParseCertificateRequest(csrDER)
		if parseErr != nil || csr.CheckSignature() != nil {
			return errors.New("invalid certificate request")
		}
		ca, caKey, parseErr := parseCA(value)
		if parseErr != nil {
			return parseErr
		}
		serial, parseErr := randomSerial()
		if parseErr != nil {
			return parseErr
		}
		hostID = serial.Text(16)
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: hostID, Organization: []string{"git-review agent"}}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		der, signErr := x509.CreateCertificate(rand.Reader, template, ca, csr.PublicKey, caKey)
		if signErr != nil {
			return signErr
		}
		certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		value.Hosts[hostID] = hostRecord{Name: pending.Name}
		return nil
	})
	return hostID, certPEM, err
}

func (s Store) HostAllowed(id string) bool {
	value, err := s.read()
	if err != nil {
		return false
	}
	host, ok := value.Hosts[id]
	return ok && !host.Revoked
}

func (s Store) Revoke(id string) error {
	return s.update(func(value *state) error {
		host, ok := value.Hosts[id]
		if !ok {
			return errors.New("host not found")
		}
		host.Revoked = true
		value.Hosts[id] = host
		return nil
	})
}

func (s Store) Hosts() (map[string]string, error) {
	value, err := s.read()
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for id, host := range value.Hosts {
		result[id] = host.Name + map[bool]string{true: " (revoked)"}[host.Revoked]
	}
	return result, nil
}

func (s Store) read() (state, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.Ensure(); err != nil {
			return state{}, err
		}
		data, err = os.ReadFile(s.Path)
	}
	if err != nil {
		return state{}, err
	}
	var value state
	if err := json.Unmarshal(data, &value); err != nil {
		return state{}, err
	}
	return value, nil
}

func (s Store) update(change func(*state) error) error {
	if s.Path == "" {
		return errors.New("identity state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	var value state
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		value, err = newState()
	} else if err == nil {
		err = json.Unmarshal(data, &value)
	}
	if err != nil {
		return err
	}
	if value.Hosts == nil {
		value.Hosts = map[string]hostRecord{}
	}
	if value.Pending == nil {
		value.Pending = map[string]pendingRecord{}
	}
	if needsServerRenewal(&value, time.Now()) {
		if err := renewServer(&value, time.Now()); err != nil {
			return err
		}
	}
	if err := change(&value); err != nil {
		return err
	}
	return atomicWrite(s.Path, value)
}

func splitCode(v string) []string {
	for i := 0; i < len(v); i++ {
		if v[i] == '.' {
			return []string{v[:i], v[i+1:]}
		}
	}
	return nil
}
