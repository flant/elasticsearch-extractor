// Package opensearch - клиент Elasticsearch/OpenSearch для extractor: только те вызовы,
// которые нужны восстановлению снапшотов, поиску и выгрузкам. Используемые API у
// Elasticsearch 7+ и OpenSearch совпадают.
package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Options - подключение к кластеру.
type Options struct {
	Name     string
	Host     string // с "/" в конце
	Username string
	Password string
	// TLS: свой CA, клиентский сертификат и отключение проверки
	CACert     string
	ClientCert string
	ClientKey  string
	Insecure   bool
	Timeout    time.Duration
}

type Client struct {
	name string
	base string
	user string
	pass string
	http *http.Client
}

// Error - ответ кластера с кодом не 2xx.
type Error struct {
	Status int
	Type   string
	Reason string
}

func (e *Error) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("opensearch: HTTP %d", e.Status)
	}
	return fmt.Sprintf("opensearch: %s", e.Reason)
}

// IsNotFound - кластер ответил 404 (нет индекса, репозитория, снапшота).
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func New(o Options) (*Client, error) {
	tlsConf, err := tlsConfig(o)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", o.Name, err)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		name: o.Name,
		base: o.Host,
		user: o.Username,
		pass: o.Password,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: timeout}).DialContext,
				TLSClientConfig:     tlsConf,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}, nil
}

func tlsConfig(o Options) (*tls.Config, error) {
	c := &tls.Config{InsecureSkipVerify: o.Insecure}
	if o.CACert != "" {
		pem, err := os.ReadFile(o.CACert)
		if err != nil {
			return nil, fmt.Errorf("ca_cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_cert %s: no certificates", o.CACert)
		}
		c.RootCAs = pool
	}
	if o.ClientCert != "" && o.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(o.ClientCert, o.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		c.Certificates = []tls.Certificate{cert}
	}
	return c, nil
}

func (c *Client) Name() string { return c.name }
func (c *Client) Host() string { return c.base }

// seg экранирует имя (репозиторий, снапшот, индекс) для пути: "/" и "?" в нём не
// должны менять запрос. Маски "*" и списки через "," остаются как есть.
func seg(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "%2A", "*")
}

// do выполняет запрос: body кодируется в JSON, ответ декодируется в out (если не nil).
func (c *Client) do(ctx context.Context, op, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opensearch %s: %w", c.name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("opensearch %s: %w", c.name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("opensearch %s: bad response to %s: %w", c.name, op, err)
	}
	return nil
}

func parseError(status int, raw []byte) error {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	out := &Error{Status: status}
	if json.Unmarshal(raw, &e) == nil && len(e.Error) > 0 {
		var obj struct {
			Type      string `json:"type"`
			Reason    string `json:"reason"`
			RootCause []struct {
				Reason string `json:"reason"`
			} `json:"root_cause"`
		}
		if json.Unmarshal(e.Error, &obj) == nil {
			out.Type, out.Reason = obj.Type, obj.Reason
			if out.Reason == "" && len(obj.RootCause) > 0 {
				out.Reason = obj.RootCause[0].Reason
			}
		} else {
			// бывает "error": "строка"
			_ = json.Unmarshal(e.Error, &out.Reason)
		}
	}
	return out
}
