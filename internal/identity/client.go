package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"time"
)

func Enroll(ctx context.Context, bundle Enrollment, name string) (Credentials, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(bundle.CAPEM)) {
		return Credentials{}, errors.New("invalid hub CA")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Credentials{}, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		return Credentials{}, err
	}
	body, _ := json.Marshal(map[string]string{"code": bundle.Code, "csr": base64.RawStdEncoding.EncodeToString(csr)})
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: ServerName, MinVersion: tls.VersionTLS13}}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, bundle.Tunnel+"/v1/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return Credentials{}, errors.New("enrollment rejected: " + string(message))
	}
	var result struct{ HostID, Cert string }
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return Credentials{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return Credentials{HubURL: bundle.HubURL, Tunnel: bundle.Tunnel, CAPEM: bundle.CAPEM, CertPEM: result.Cert, KeyPEM: string(keyPEM), HostID: result.HostID}, nil
}

func (c Credentials) TLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(c.CAPEM)) {
		return nil, errors.New("invalid hub CA")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, ServerName: ServerName, MinVersion: tls.VersionTLS13}, nil
}
