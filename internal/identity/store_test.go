package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnrollmentIsOneTimeAndRevocable(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "identity.json")}
	now := time.Now()
	bundleText, err := store.CreateEnrollment("agent", "http://hub:8080", "https://hub:8443", now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseEnrollment(bundleText)
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "agent"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	hostID, cert, err := store.SignEnrollment(bundle.Code, csr, now)
	if err != nil {
		t.Fatal(err)
	}
	if hostID == "" || len(cert) == 0 || !store.HostAllowed(hostID) {
		t.Fatal("issued host was not allowed")
	}
	if _, _, err := store.SignEnrollment(bundle.Code, csr, now); err == nil {
		t.Fatal("enrollment code was reusable")
	}
	if err := store.Revoke(hostID); err != nil {
		t.Fatal(err)
	}
	if store.HostAllowed(hostID) {
		t.Fatal("revoked host remained allowed")
	}
}

func TestCredentialsRetainMultipleHubsWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	first := Credentials{HubURL: "http://one", CAPEM: "ca", CertPEM: "cert", KeyPEM: "key"}
	second := Credentials{HubURL: "http://two", CAPEM: "ca2", CertPEM: "cert2", KeyPEM: "key2"}
	if err := SaveCredentials(path, first); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCredentialsForHub(path, first.HubURL)
	if err != nil || loaded.KeyPEM != "key" {
		t.Fatalf("first credentials = %#v, %v", loaded, err)
	}
	defaultCredentials, err := LoadCredentials(path)
	if err != nil || defaultCredentials.HubURL != second.HubURL {
		t.Fatalf("default credentials = %#v, %v", defaultCredentials, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode = %o", info.Mode().Perm())
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if directory.Mode().Perm() != 0o700 {
		t.Fatalf("credentials directory mode = %o", directory.Mode().Perm())
	}
}

func TestHubIdentityPersistsAndRenewsOnlyServerCertificate(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "identity.json")}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	firstCert, _, firstCA, err := store.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.update(func(value *state) error { value.ServerPEM = "invalid"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	renewed, _, renewedCA, err := store.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if string(firstCert) == string(renewed) {
		t.Fatal("server certificate was not renewed")
	}
	if string(firstCA) != string(renewedCA) {
		t.Fatal("server renewal replaced the private CA")
	}
	block, _ := pem.Decode(renewed)
	if block == nil {
		t.Fatal("renewed certificate is not PEM")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode = %o", info.Mode().Perm())
	}
}

func TestExpiredEnrollmentIsRejected(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "identity.json")}
	now := time.Now()
	bundleText, err := store.CreateEnrollment("agent", "http://hub", "https://hub:8443", now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseEnrollment(bundleText)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	csr, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if _, _, err := store.SignEnrollment(bundle.Code, csr, now.Add(11*time.Minute)); err == nil {
		t.Fatal("expired enrollment accepted")
	}
}

func TestMalformedEnrollmentAndCSRDoNotConsumeCode(t *testing.T) {
	for _, bundle := range []string{"", "gr1:not-base64", "wrong:abc"} {
		if _, err := ParseEnrollment(bundle); err == nil {
			t.Errorf("bundle %q accepted", bundle)
		}
	}
	store := Store{Path: filepath.Join(t.TempDir(), "identity.json")}
	now := time.Now()
	bundleText, err := store.CreateEnrollment("agent", "http://hub", "https://hub:8443", now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseEnrollment(bundleText)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SignEnrollment(bundle.Code, []byte("not a CSR"), now); err == nil {
		t.Fatal("malformed CSR accepted")
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	csr, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if _, _, err := store.SignEnrollment(bundle.Code, csr, now); err != nil {
		t.Fatalf("malformed CSR consumed enrollment: %v", err)
	}
}
