package config_test

import (
	"errors"
	"testing"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/truenas-exporter/internal/config"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	def, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	def.TrueNAS.URL, def.TrueNAS.APIKey = "wss://nas/api/current", "key"
	if err := def.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	tests := []struct {
		name string
		mod  func(*config.Config)
		want error
	}{
		{"log level", func(c *config.Config) { c.LogLevel = "loud" }, config.ErrInvalidLogLevel},
		{"plain ws", func(c *config.Config) { c.TrueNAS.URL = "ws://nas/api/current" }, config.ErrInvalidURL},
		{"short fingerprint", func(c *config.Config) { c.TrueNAS.TLSFingerprint = "abcd" }, config.ErrInvalidFingerprint},
		{"fast poll", func(c *config.Config) { c.Poll.Interval = 1 }, config.ErrInvalidInterval},
		{"fast smart", func(c *config.Config) { c.Poll.SMARTInterval = 1 }, config.ErrInvalidInterval},
		{"smart off", func(c *config.Config) { c.Poll.SMARTInterval = 0 }, nil},
		{"colon fingerprint", func(c *config.Config) {
			c.TrueNAS.TLSFingerprint = "E6:15:3D:F5:B7:5E:92:C2:51:AB:B3:2A:FE:0C:1F:54:89:93:A1:BD:74:CB:E1:A9:AA:F6:35:4F:92:DD:E1:2A"
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := def
			tt.mod(&c)
			if err := c.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}
