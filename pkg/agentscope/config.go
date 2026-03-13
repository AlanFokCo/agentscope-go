package agentscope

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Config corresponds to the Python _ConfigCls and holds global runtime settings.
type Config struct {
	RunID        string
	Project      string
	Name         string
	CreatedAt    time.Time
	TraceEnabled bool

	LoggingPath  string
	LoggingLevel string

	StudioURL  string
	TracingURL string
}

var (
	globalCfg   Config
	globalCfgMu sync.RWMutex
	logger      = log.New(os.Stdout, "[agentscope] ", log.LstdFlags|log.Lmicroseconds)
)

// Option is the functional option type for Init.
type Option func(*Config)

func WithProject(project string) Option {
	return func(c *Config) {
		c.Project = project
	}
}

func WithName(name string) Option {
	return func(c *Config) {
		c.Name = name
	}
}

func WithRunID(runID string) Option {
	return func(c *Config) {
		c.RunID = runID
	}
}

func WithLogging(path, level string) Option {
	return func(c *Config) {
		c.LoggingPath = path
		c.LoggingLevel = level
	}
}

func WithStudioURL(url string) Option {
	return func(c *Config) {
		c.StudioURL = url
	}
}

func WithTracingURL(url string) Option {
	return func(c *Config) {
		c.TracingURL = url
	}
}

// Init initializes the global agentscope configuration and logging.
// It mirrors Python agentscope.init but uses Go-style options.
func Init(opts ...Option) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	setupLogger(&cfg)
	// TODO: Studio registration and tracing initialization can be wired here later.

	globalCfgMu.Lock()
	globalCfg = cfg
	globalCfgMu.Unlock()

	logger.Printf("initialized: project=%s name=%s run_id=%s", cfg.Project, cfg.Name, cfg.RunID)
}

func defaultConfig() Config {
	now := time.Now()
	return Config{
		RunID:        uuid.NewString(),
		Project:      "UnnamedProject_" + now.Format("20060102"),
		Name:         now.Format("150405"),
		CreatedAt:    now,
		LoggingPath:  "",
		LoggingLevel: "INFO",
	}
}

func setupLogger(cfg *Config) {
	var out *os.File
	if cfg.LoggingPath == "" {
		// Default to stdout; logger is already initialized at package level.
		out = os.Stdout
	} else {
		f, err := os.OpenFile(cfg.LoggingPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			logger.Printf("failed to open log file %s: %v", cfg.LoggingPath, err)
			out = os.Stdout
		} else {
			out = f
		}
	}

	logger.SetOutput(out)
	logrus.SetOutput(out)

	levelStr := strings.ToLower(cfg.LoggingLevel)
	level, err := logrus.ParseLevel(levelStr)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
}

// ConfigSnapshot returns a copy of the current global configuration.
func ConfigSnapshot() Config {
	globalCfgMu.RLock()
	defer globalCfgMu.RUnlock()
	return globalCfg
}

// Logger returns the global logger instance.
func Logger() *log.Logger {
	return logger
}

// Log returns the global logrus logger with level support.
func Log() *logrus.Logger {
	return logrus.StandardLogger()
}
