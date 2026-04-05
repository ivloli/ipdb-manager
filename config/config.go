package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Nacos     NacosConfig     `yaml:"nacos"`
	IP2Region IP2RegionConfig `yaml:"ip2region"`
	API       APIConfig       `yaml:"api"`
}

type NacosConfig struct {
	Addr      string `yaml:"addr"`
	Namespace string `yaml:"namespace"`
	Group     string `yaml:"group"`
	DataID    string `yaml:"data_id"`
	DataIDV6  string `yaml:"data_id_v6"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type IP2RegionConfig struct {
	Dir          string        `yaml:"dir"`
	TXTPath      string        `yaml:"txt_path"`
	XDBPath      string        `yaml:"xdb_path"`
	TXTPathV6    string        `yaml:"txt_v6_path"`
	XDBPathV6    string        `yaml:"xdb_v6_path"`
	PollInterval time.Duration `yaml:"poll_interval"`
	GithubToken  string        `yaml:"github_token"`
	ReleasesURL  string        `yaml:"releases_url"`
}

type APIConfig struct {
	Listen string `yaml:"listen"`
	Token  string `yaml:"token"`
}

// Load reads a YAML config file and expands environment variables.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	// Expand ${VAR} / $VAR before unmarshalling.
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Nacos.Addr == "" {
		return fmt.Errorf("nacos.addr is required")
	}
	if c.Nacos.Group == "" {
		c.Nacos.Group = "subnet_mapping"
	}
	if c.Nacos.DataID == "" {
		c.Nacos.DataID = "subnet_map"
	}
	if c.Nacos.DataIDV6 == "" {
		c.Nacos.DataIDV6 = "subnet_map_v6"
	}
	if c.IP2Region.Dir == "" {
		c.IP2Region.Dir = "data/ip2region"
	}
	if c.IP2Region.TXTPath == "" {
		c.IP2Region.TXTPath = c.IP2Region.Dir + "/ipv4_source.txt"
	}
	if c.IP2Region.XDBPath == "" {
		c.IP2Region.XDBPath = c.IP2Region.Dir + "/ip2region_v4.xdb"
	}
	if c.IP2Region.TXTPathV6 == "" {
		c.IP2Region.TXTPathV6 = c.IP2Region.Dir + "/ipv6_source.txt"
	}
	if c.IP2Region.XDBPathV6 == "" {
		c.IP2Region.XDBPathV6 = c.IP2Region.Dir + "/ip2region_v6.xdb"
	}
	if c.IP2Region.PollInterval <= 0 {
		c.IP2Region.PollInterval = time.Hour
	}
	if c.IP2Region.ReleasesURL == "" {
		c.IP2Region.ReleasesURL = "https://api.github.com/repos/lionsoul2014/ip2region/releases/latest"
	}
	return nil
}
