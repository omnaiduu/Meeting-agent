// Package config loads settings from environment variables (and an optional .env file).
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting. See docs/PLAN.md §12.
type Config struct {
	HTTPAddr string
	DataDir  string
	LogLevel slog.Level

	GroqAPIKey       string
	GroqBaseURL      string
	GroqSTTModel     string
	GroqSummaryModel string
	STTProvider      string // "groq" or "fake"
	SummaryProvider  string // "groq" or "fake"
	Language         string // optional ISO-639-1 hint for Whisper, e.g. "en"

	ChromePath         string
	BotName            string
	MaxConcurrentBots  int
	LobbyTimeout       time.Duration
	AloneTimeout       time.Duration
	MaxMeetingDuration time.Duration
	ChunkSeconds       int
	// AllowedHosts lists meeting hosts the API accepts. Tests add a local fake-Meet host.
	AllowedHosts []string
	// DevCORS enables permissive CORS for the Vite dev server.
	DevCORS bool
}

// Load reads .env (if present, without overriding real env vars) and then the environment.
func Load() (Config, error) {
	_ = LoadDotEnv(".env")

	c := Config{
		HTTPAddr:           str("HTTP_ADDR", ":8080"),
		DataDir:            str("DATA_DIR", "./data"),
		GroqAPIKey:         str("GROQ_API_KEY", ""),
		GroqBaseURL:        str("GROQ_BASE_URL", "https://api.groq.com/openai/v1"),
		GroqSTTModel:       str("GROQ_STT_MODEL", "whisper-large-v3-turbo"),
		GroqSummaryModel:   str("GROQ_SUMMARY_MODEL", "openai/gpt-oss-120b"),
		STTProvider:        str("STT_PROVIDER", ""),
		SummaryProvider:    str("SUMMARY_PROVIDER", ""),
		Language:           str("STT_LANGUAGE", ""),
		ChromePath:         str("CHROME_PATH", ""),
		BotName:            str("BOT_NAME", "Meeting Agent (Notetaker)"),
		AllowedHosts:       list("ALLOWED_MEETING_HOSTS", []string{"meet.google.com"}),
		DevCORS:            boolean("DEV_CORS", false),
	}
	var err error
	if c.LogLevel, err = level(str("LOG_LEVEL", "info")); err != nil {
		return c, err
	}
	if c.MaxConcurrentBots, err = integer("MAX_CONCURRENT_BOTS", 2); err != nil {
		return c, err
	}
	if c.ChunkSeconds, err = integer("CHUNK_SECONDS", 20); err != nil {
		return c, err
	}
	if c.LobbyTimeout, err = duration("LOBBY_TIMEOUT", 10*time.Minute); err != nil {
		return c, err
	}
	if c.AloneTimeout, err = duration("ALONE_TIMEOUT", 2*time.Minute); err != nil {
		return c, err
	}
	if c.MaxMeetingDuration, err = duration("MAX_MEETING_DURATION", 2*time.Hour); err != nil {
		return c, err
	}

	// Default providers: real Groq when a key is set, otherwise fakes so the app still runs.
	if c.STTProvider == "" {
		c.STTProvider = pick(c.GroqAPIKey != "", "groq", "fake")
	}
	if c.SummaryProvider == "" {
		c.SummaryProvider = pick(c.GroqAPIKey != "", "groq", "fake")
	}
	return c, c.validate()
}

func (c Config) validate() error {
	if c.MaxConcurrentBots < 1 {
		return fmt.Errorf("MAX_CONCURRENT_BOTS must be >= 1")
	}
	if c.ChunkSeconds < 10 {
		return fmt.Errorf("CHUNK_SECONDS must be >= 10 (Groq bills at least 10 s per request)")
	}
	for _, p := range []string{c.STTProvider, c.SummaryProvider} {
		if p != "groq" && p != "fake" {
			return fmt.Errorf("unknown provider %q (want groq or fake)", p)
		}
	}
	if (c.STTProvider == "groq" || c.SummaryProvider == "groq") && c.GroqAPIKey == "" {
		return fmt.Errorf("GROQ_API_KEY is required when a provider is groq")
	}
	return nil
}

// LoadDotEnv sets KEY=VALUE pairs from path unless the variable is already set.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return s.Err()
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func list(key string, def []string) []string {
	v := str(key, "")
	if v == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func boolean(key string, def bool) bool {
	b, err := strconv.ParseBool(str(key, strconv.FormatBool(def)))
	if err != nil {
		return def
	}
	return b
}

func integer(key string, def int) (int, error) {
	v := str(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func duration(key string, def time.Duration) (time.Duration, error) {
	v := str(key, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func level(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return l, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	return l, nil
}
