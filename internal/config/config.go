package config

import (
	"fmt"
	"os"
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

	return cfg, nil
}
