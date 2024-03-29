// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package router

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"log"
)

func createTLSConfig(pemFile, pemCertFile, pemPrivateKeyFile string, insecureSkipVerify bool) *tls.Config {
	tlsConfig := tls.Config{}
	if insecureSkipVerify {
		// pem settings are irrelevant if we're skipping verification anyway
		tlsConfig.InsecureSkipVerify = true
	}
	if len(pemFile) > 0 {
		rootCerts, err := loadCertificatesFrom(pemFile)
		if err != nil {
			log.Fatalf("Couldn't load root certificate from %s. Got %s.", pemFile, err)
			return nil
		}
		tlsConfig.RootCAs = rootCerts
	}
	if len(pemCertFile) > 0 && len(pemPrivateKeyFile) > 0 {
		certs, err := tls.LoadX509KeyPair(pemCertFile, pemPrivateKeyFile)
		if err != nil {
			log.Fatalf("Couldn't setup client authentication. Got %s.", err)
			return nil
		}
		tlsConfig.Certificates = []tls.Certificate{certs}
	}

	return &tlsConfig
}

func loadCertificatesFrom(pemFile string) (*x509.CertPool, error) {
	caCert, err := ioutil.ReadFile(pemFile)
	if err != nil {
		return nil, err
	}
	certificates := x509.NewCertPool()
	certificates.AppendCertsFromPEM(caCert)
	return certificates, nil
}

func loadPrivateKeyFrom(pemCertFile, pemPrivateKeyFile string) (*tls.Certificate, error) {
	privateKey, err := tls.LoadX509KeyPair(pemCertFile, pemPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	return &privateKey, nil
}
