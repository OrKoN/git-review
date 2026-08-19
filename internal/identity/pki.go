package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"time"
)

func newState() (state, error) {
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return state{}, err
	}
	serial, _ := randomSerial()
	caTemplate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "git-review private CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return state{}, err
	}
	value := state{CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})), CAKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})), Hosts: map[string]hostRecord{}, Pending: map[string]pendingRecord{}}
	if err := renewServer(&value, now); err != nil {
		return state{}, err
	}
	return value, nil
}

func needsServerRenewal(value *state, now time.Time) bool {
	block, _ := pem.Decode([]byte(value.ServerPEM))
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return err != nil || cert.NotAfter.Before(now.Add(30*24*time.Hour))
}

func renewServer(value *state, now time.Time) error {
	ca, caKey, err := parseCA(value)
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: ServerName}, DNSNames: []string{ServerName}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	value.ServerPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	value.ServerKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return nil
}

func parseCA(value *state) (*x509.Certificate, *rsa.PrivateKey, error) {
	certBlock, _ := pem.Decode([]byte(value.CAPEM))
	keyBlock, _ := pem.Decode([]byte(value.CAKeyPEM))
	if certBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("invalid CA")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	return cert, key, err
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
