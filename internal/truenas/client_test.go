package truenas_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/truenas-exporter/internal/truenas"
	"github.com/coder/websocket"
)

type rpcReq struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func fakeNAS(t *testing.T, logins *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var req rpcReq
			if err := json.Unmarshal(data, &req); err != nil {
				return
			}
			var resp map[string]any
			switch req.Method {
			case "auth.login_with_api_key":
				logins.Add(1)
				resp = map[string]any{"result": strings.Contains(string(req.Params), goodKey)}
			case "alert.list":
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","method":"collection_update","params":{}}`))
				resp = map[string]any{"result": []map[string]any{{"klass": "SMART", "level": "CRITICAL", "dismissed": false}}}
			case "pool.query":
				resp = map[string]any{"error": map[string]any{"code": -32001, "message": "Method call error", "data": map[string]any{"reason": "denied"}}}
			case "hang.up":
				return
			}
			resp["jsonrpc"], resp["id"] = "2.0", req.ID
			b, _ := json.Marshal(resp)
			if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}))
}

const goodKey = "good"

func fingerprint(srv *httptest.Server) string {
	sum := sha256.Sum256(srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

func wsURL(srv *httptest.Server) string {
	return "wss" + strings.TrimPrefix(srv.URL, "https") + "/api/current"
}

func TestClient(t *testing.T) {
	t.Parallel()
	var logins atomic.Int32
	srv := fakeNAS(t, &logins)
	defer srv.Close()
	c := truenas.New(truenas.Options{URL: wsURL(srv), APIKey: goodKey, Fingerprint: fingerprint(srv), Timeout: 5 * time.Second})
	defer c.Close()
	ctx := context.Background()

	alerts, err := c.Alerts(ctx)
	if err != nil || len(alerts) != 1 || alerts[0].Klass != "SMART" {
		t.Fatalf("alerts %v %v", alerts, err)
	}
	_, err = c.Pools(ctx)
	var rpcErr *truenas.RPCError
	if !errors.As(err, &rpcErr) || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("pool error %v", err)
	}
	if _, err := c.Alerts(ctx); err != nil {
		t.Fatal(err)
	}
	if n := logins.Load(); n != 1 {
		t.Fatalf("API errors should keep the connection, logins %d", n)
	}
	if err := c.Call(ctx, "hang.up", nil, nil); err == nil {
		t.Fatal("expected an error when the server hangs up")
	}
	if _, err := c.Alerts(ctx); err != nil {
		t.Fatal(err)
	}
	if n := logins.Load(); n != 2 {
		t.Fatalf("expected a reconnect, logins %d", n)
	}
}

func TestClientRejects(t *testing.T) {
	t.Parallel()
	var logins atomic.Int32
	srv := fakeNAS(t, &logins)
	defer srv.Close()
	ctx := context.Background()

	bad := truenas.New(truenas.Options{URL: wsURL(srv), APIKey: "bad", Fingerprint: fingerprint(srv), Timeout: 5 * time.Second})
	if _, err := bad.Alerts(ctx); !errors.Is(err, truenas.ErrLoginRejected) {
		t.Fatalf("bad key: %v", err)
	}
	pinned := truenas.New(truenas.Options{URL: wsURL(srv), APIKey: goodKey, Fingerprint: strings.Repeat("0", 64), Timeout: 5 * time.Second})
	if _, err := pinned.Alerts(ctx); err == nil || logins.Load() != 1 {
		t.Fatalf("wrong fingerprint should fail before login: %v, logins %d", err, logins.Load())
	}
	unverified := truenas.New(truenas.Options{URL: wsURL(srv), APIKey: goodKey, Timeout: 5 * time.Second})
	if _, err := unverified.Alerts(ctx); err == nil {
		t.Fatal("self-signed certificate should fail without a pin")
	}
}
