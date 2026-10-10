package config

//go:generate go tool configulator -type Config

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type Config struct {
	LogLevel LogLevel `name:"log-level" description:"Logging level for the application. One of debug, info, warn, or error" default:"info"`
	HTTP     HTTP     `name:"http" description:"HTTP server configuration"`
	TrueNAS  TrueNAS  `name:"truenas" description:"TrueNAS connection"`
	Poll     Poll     `name:"poll" description:"How often each group of data is fetched from TrueNAS"`
}

type HTTP struct {
	Bind string `name:"bind" description:"Address to listen on. The default, [::], listens on all interfaces" default:"[::]"`
	Port int    `name:"port" description:"Port to serve /metrics and /healthz on" default:"9814"`
}

type TrueNAS struct {
	URL                string        `name:"url" description:"TrueNAS JSON-RPC websocket URL, for example wss://truenas.local/api/current. Must be wss: TrueNAS revokes API keys that are sent over plain ws" required:"true"`
	APIKey             string        `name:"api-key" description:"TrueNAS API key. A user with the POOL_READ, DISK_READ, REPORTING_READ and ALERT_LIST_READ roles is enough" secret:"true" required:"true"`
	TLSFingerprint     string        `name:"tls-fingerprint" description:"SHA-256 fingerprint of the TrueNAS certificate, hex with or without colons. When set, the certificate is pinned instead of verified against the system CAs, which suits the default self-signed certificate"`
	InsecureSkipVerify bool          `name:"insecure-skip-verify" description:"Skip TLS certificate verification. Prefer tls-fingerprint" default:"false"`
	Timeout            time.Duration `name:"timeout" description:"Timeout for connecting and for each API call" default:"30s"`
}

type Poll struct {
	Interval      time.Duration `name:"interval" description:"How often pools, disk temperatures, alerts and ARC are fetched" default:"1m"`
	SMARTInterval time.Duration `name:"smart-interval" description:"How often SMART self-test results are fetched, or 0 to turn SMART collection off. smart.test.results fails for API clients on TrueNAS 25.04 (NAS-136478)" default:"15m"`
}

var (
	ErrInvalidLogLevel    = errors.New("invalid log level provided")
	ErrInvalidURL         = errors.New("truenas.url must start with wss://")
	ErrInvalidFingerprint = errors.New("truenas.tls-fingerprint must be 64 hex characters")
	ErrInvalidInterval    = errors.New("poll.interval must be at least 10s and poll.smart-interval 0 or at least 10s")
)

var fingerprintRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func (t TrueNAS) Fingerprint() string {
	return strings.ToLower(strings.ReplaceAll(t.TLSFingerprint, ":", ""))
}

func (c Config) Validate() error {
	if c.LogLevel != LogLevelDebug &&
		c.LogLevel != LogLevelInfo &&
		c.LogLevel != LogLevelWarn &&
		c.LogLevel != LogLevelError {
		return ErrInvalidLogLevel
	}
	if !strings.HasPrefix(c.TrueNAS.URL, "wss://") {
		return ErrInvalidURL
	}
	if c.TrueNAS.TLSFingerprint != "" && !fingerprintRe.MatchString(c.TrueNAS.Fingerprint()) {
		return ErrInvalidFingerprint
	}
	if c.Poll.Interval < 10*time.Second || (c.Poll.SMARTInterval != 0 && c.Poll.SMARTInterval < 10*time.Second) {
		return ErrInvalidInterval
	}
	return nil
}
