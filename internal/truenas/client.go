package truenas

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const readLimit = 64 << 20

var (
	ErrLoginRejected       = errors.New("TrueNAS rejected the API key")
	ErrFingerprintMismatch = errors.New("TrueNAS certificate fingerprint does not match")
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *RPCError) Error() string {
	var d struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(e.Data, &d) == nil && d.Reason != "" {
		return fmt.Sprintf("%s (%d): %s", e.Message, e.Code, d.Reason)
	}
	return fmt.Sprintf("%s (%d)", e.Message, e.Code)
}

type Options struct {
	URL                string
	APIKey             string
	Fingerprint        string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

type Client struct {
	opts   Options
	mu     sync.Mutex
	conn   *websocket.Conn
	nextID atomic.Uint64
}

func New(opts Options) *Client {
	return &Client{opts: opts}
}

func (c *Client) httpClient() *http.Client {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	switch {
	case c.opts.Fingerprint != "":
		want := c.opts.Fingerprint
		tlsConfig.InsecureSkipVerify = true //nolint:gosec
		tlsConfig.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrFingerprintMismatch
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if hex.EncodeToString(sum[:]) != want {
				return ErrFingerprintMismatch
			}
			return nil
		}
	case c.opts.InsecureSkipVerify:
		tlsConfig.InsecureSkipVerify = true //nolint:gosec
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}

func (c *Client) connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, c.opts.URL, &websocket.DialOptions{HTTPClient: c.httpClient()})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", c.opts.URL, err)
	}
	conn.SetReadLimit(readLimit)
	c.conn = conn
	var ok bool
	if err := c.call(ctx, "auth.login_with_api_key", []any{c.opts.APIKey}, &ok); err != nil {
		c.closeLocked()
		return fmt.Errorf("login: %w", err)
	}
	if !ok {
		c.closeLocked()
		return ErrLoginRejected
	}
	return nil
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
		c.conn = nil
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type response struct {
	ID     *uint64         `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	if params == nil {
		params = []any{}
	}
	id := c.nextID.Add(1)
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	b, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		return fmt.Errorf("%s: write: %w", method, err)
	}
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s: read: %w", method, err)
		}
		var resp response
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("%s: decode: %w", method, err)
		}
		if resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("%s: %w", method, resp.Error)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("%s: decode result: %w", method, err)
		}
		return nil
	}
}

// Call runs one API method, reconnecting and logging in again when the
// connection is missing or broken. API errors leave the connection open.
func (c *Client) Call(ctx context.Context, method string, params []any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			return err
		}
	}
	err := c.call(ctx, method, params, out)
	var rpcErr *RPCError
	if err != nil && !errors.As(err, &rpcErr) {
		c.closeLocked()
	}
	return err
}
