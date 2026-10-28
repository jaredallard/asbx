// Copyright (C) 2026 asbx contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: GPL-3.0

package stdiobridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// KeyEnv is the environment variable that holds the client's key, which
// [Session.Env] returns. It's only set when a bridge is enabled.
const KeyEnv = "ASBX_BRIDGE_KEY"

// serverName is the name the host's certificate is for. It's never
// resolved, it only has to match on both ends.
const serverName = "asbx-host"

// validity is how long a session's certificates last (7d).
const validity = (24 * time.Hour) * 7

// Session is the keys of one run of the sandbox: a certificate
// authority that exists only in memory, and the certificates it signed
// for the host and for the container. It's never written to disk, and
// only the container's half leaves the host, in [KeyEnv].
type Session struct {
	roots      *x509.CertPool
	serverCert tls.Certificate
	bundle     string
}

// serial returns a random certificate serial number.
func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// signed returns the certificate that template describes, signed by
// parent with parentKey, and the private key it was made for. A nil
// parent makes template self-signed.
func signed(template, parent *x509.Certificate, parentKey ed25519.PrivateKey) (*x509.Certificate, ed25519.PrivateKey, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	if template.SerialNumber, err = serial(); err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template.NotBefore = now.Add(-time.Minute)
	template.NotAfter = now.Add(validity)

	if parent == nil {
		parent, parentKey = template, key
	}

	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, parentKey)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

// encodeCert returns cert as PEM.
func encodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// encodeKey returns key as PEM.
func encodeKey(key ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// NewSession returns a new [Session], with keys that nothing else has.
func NewSession() (*Session, error) {
	ca, caKey, err := signed(&x509.Certificate{
		Subject:               pkix.Name{CommonName: "asbx session"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create session ca: %w", err)
	}

	server, serverKey, err := signed(&x509.Certificate{
		Subject:     pkix.Name{CommonName: serverName},
		DNSNames:    []string{serverName},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, caKey)
	if err != nil {
		return nil, fmt.Errorf("create host certificate: %w", err)
	}

	client, clientKey, err := signed(&x509.Certificate{
		Subject:     pkix.Name{CommonName: "asbx container"},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, ca, caKey)
	if err != nil {
		return nil, fmt.Errorf("create container certificate: %w", err)
	}

	clientKeyPEM, err := encodeKey(clientKey)
	if err != nil {
		return nil, fmt.Errorf("encode container key: %w", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	bundle := append(encodeCert(ca), encodeCert(client)...)
	bundle = append(bundle, clientKeyPEM...)

	return &Session{
		roots:      roots,
		serverCert: tls.Certificate{Certificate: [][]byte{server.Raw}, PrivateKey: serverKey},
		bundle:     base64.StdEncoding.EncodeToString(bundle),
	}, nil
}

// ServerConfig returns the host's TLS configuration, which only serves
// clients that this session signed a certificate for.
func (s *Session) ServerConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{s.serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    s.roots,
	}
}

// Env returns the value of [KeyEnv] that lets a container talk to the
// host: the authority's certificate, and the container's own
// certificate and key, on one line.
func (s *Session) Env() string {
	return s.bundle
}

// ClientConfig returns the container's TLS configuration from value, as
// [Session.Env] returned it, which only trusts this session's host.
func ClientConfig(value string) (*tls.Config, error) {
	if value == "" {
		return nil, fmt.Errorf("%s isn't set, the asbx bridge can't be trusted without it", KeyEnv)
	}

	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s isn't valid: %w", KeyEnv, err)
	}

	// The authority's certificate comes first, then the container's own,
	// then its key.
	var authority, cert, key []byte
	for {
		var block *pem.Block
		if block, raw = pem.Decode(raw); block == nil {
			break
		}

		switch {
		case block.Type == "CERTIFICATE" && authority == nil:
			authority = pem.EncodeToMemory(block)
		case block.Type == "CERTIFICATE":
			cert = pem.EncodeToMemory(block)
		case block.Type == "PRIVATE KEY":
			key = pem.EncodeToMemory(block)
		}
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(authority) {
		return nil, errors.New(KeyEnv + " has no certificate authority")
	}

	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, fmt.Errorf("%s has no usable certificate: %w", KeyEnv, err)
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ServerName:   serverName,
		RootCAs:      roots,
		Certificates: []tls.Certificate{pair},
	}, nil
}
