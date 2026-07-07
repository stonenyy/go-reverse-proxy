package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Routes      []RouteConfig     `yaml:"routes"`
	RateLimit   RateLimitConfig   `yaml:"rate_limit"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
	TLS         *TLSConfig        `yaml:"tls"`
}

type ServerConfig struct {
	Port         int `yaml:"port"`
	ReadTimeout  int `yaml:"read_timeout"`
	WriteTimeout int `yaml:"write_timeout"`
	IdleTimeout  int `yaml:"idle_timeout"`
}

type RouteConfig struct {
	Path            string          `yaml:"path"`
	Backends        []BackendConfig `yaml:"backends"`
	LoadBalance     string          `yaml:"load_balance"`
	RequiredHeaders []HeaderRule    `yaml:"required_headers"`
}

// HeaderRule Header 校验规则
// Name:  Header 名称（必填）
// Value: 期望值，非空时精确匹配；为空时只检查 Header 是否存在
type HeaderRule struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type BackendConfig struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight"`
}

type RateLimitConfig struct {
	Enabled           bool    `yaml:"enabled"`
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

type HealthCheckConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Interval int    `yaml:"interval"`
	Timeout  int    `yaml:"timeout"`
	Path     string `yaml:"path"`
}

type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

func (s *ServerConfig) ReadTimeoutDuration() time.Duration {
	return time.Duration(s.ReadTimeout) * time.Second
}

func (s *ServerConfig) WriteTimeoutDuration() time.Duration {
	return time.Duration(s.WriteTimeout) * time.Second
}

func (s *ServerConfig) IdleTimeoutDuration() time.Duration {
	return time.Duration(s.IdleTimeout) * time.Second
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 30
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 30
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 60
	}
	if cfg.HealthCheck.Path == "" {
		cfg.HealthCheck.Path = "/health"
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("routes must contain at least one route")
	}
	for i, route := range c.Routes {
		if strings.TrimSpace(route.Path) == "" {
			return fmt.Errorf("routes[%d].path must not be empty", i)
		}
		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("routes[%d].path must start with /", i)
		}
		if len(route.Backends) == 0 {
			return fmt.Errorf("routes[%d] must contain at least one backend", i)
		}
		switch route.LoadBalance {
		case "", "roundrobin", "weighted", "random":
		default:
			return fmt.Errorf("routes[%d].load_balance invalid: %s", i, route.LoadBalance)
		}
		for j, backend := range route.Backends {
			if strings.TrimSpace(backend.URL) == "" {
				return fmt.Errorf("routes[%d].backends[%d].url must not be empty", i, j)
			}
			u, err := url.Parse(backend.URL)
			if err != nil {
				return fmt.Errorf("routes[%d].backends[%d].url invalid: %w", i, j, err)
			}
			if u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("routes[%d].backends[%d].url must include scheme and host", i, j)
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return fmt.Errorf("routes[%d].backends[%d].url unsupported scheme: %s", i, j, u.Scheme)
			}
			if backend.Weight < 0 {
				return fmt.Errorf("routes[%d].backends[%d].weight must be non-negative", i, j)
			}
		}
		for j, rule := range route.RequiredHeaders {
			if strings.TrimSpace(rule.Name) == "" {
				return fmt.Errorf("routes[%d].required_headers[%d].name must not be empty", i, j)
			}
		}
	}
	if c.RateLimit.Enabled {
		if c.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("rate_limit.requests_per_second must be > 0")
		}
		if c.RateLimit.Burst <= 0 {
			return fmt.Errorf("rate_limit.burst must be > 0")
		}
	}
	if c.HealthCheck.Enabled {
		if c.HealthCheck.Interval <= 0 {
			return fmt.Errorf("health_check.interval must be > 0")
		}
		if c.HealthCheck.Timeout <= 0 {
			return fmt.Errorf("health_check.timeout must be > 0")
		}
		if strings.TrimSpace(c.HealthCheck.Path) == "" {
			return fmt.Errorf("health_check.path must not be empty")
		}
		if !strings.HasPrefix(c.HealthCheck.Path, "/") {
			return fmt.Errorf("health_check.path must start with /")
		}
	}
	if c.TLS != nil {
		if strings.TrimSpace(c.TLS.CertFile) == "" || strings.TrimSpace(c.TLS.KeyFile) == "" {
			return fmt.Errorf("tls.cert_file and tls.key_file must both be provided")
		}
	}
	return nil
}
