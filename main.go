package main

import (
	"flag"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/robfig/cron/v3"

	"ipdb-manager/api"
	"ipdb-manager/config"
	"ipdb-manager/watcher"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Ensure ip2region data directory exists.
	if err := os.MkdirAll(cfg.IP2Region.Dir, 0755); err != nil {
		log.Fatalf("create data dir %s: %v", cfg.IP2Region.Dir, err)
	}

	host, port := splitHostPort(cfg.Nacos.Addr)
	sc := []constant.ServerConfig{
		*constant.NewServerConfig(host, port),
	}
	cc := *constant.NewClientConfig(
		constant.WithNamespaceId(cfg.Nacos.Namespace),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("warn"),
		constant.WithUsername(cfg.Nacos.Username),
		constant.WithPassword(cfg.Nacos.Password),
	)
	nacosClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &cc,
		ServerConfigs: sc,
	})
	if err != nil {
		log.Fatalf("init nacos client: %v", err)
	}

	w := &watcher.VersionWatcher{
		TXTPath:       cfg.IP2Region.TXTPath,
		XDBPath:       cfg.IP2Region.XDBPath,
		TXTPathV6:     cfg.IP2Region.TXTPathV6,
		XDBPathV6:     cfg.IP2Region.XDBPathV6,
		VersionFile:   cfg.LocalState.UpstreamReleaseTagFile,
		LegacyVersion: cfg.LocalState.LegacyVersionFile,
		PollInterval:  cfg.IP2Region.PollInterval,
		GithubToken:   cfg.IP2Region.GithubToken,
		ReleasesURL:   cfg.IP2Region.ReleasesURL,
		NacosClient:   nacosClient,
		NacosGroup:    cfg.Nacos.Group,
		NacosDataID:   cfg.Nacos.DataID,
		NacosDataIDV6: cfg.Nacos.DataIDV6,
	}

	apiServer := &api.Server{
		ListenAddr: cfg.API.Listen,
		Token:      cfg.API.Token,
		Watcher:    w,
	}
	go func() {
		if err := apiServer.Start(); err != nil {
			log.Fatalf("start api server: %v", err)
		}
	}()

	if cfg.Scheduler.Cron != "" {
		if err := runCronMode(w, cfg.Scheduler.Cron); err != nil {
			log.Fatalf("start cron scheduler: %v", err)
		}
		return
	}

	log.Printf("ipdb-manager starting poll mode (poll_interval=%s, nacos=%s, release_tag_file=%s)",
		cfg.IP2Region.PollInterval, cfg.Nacos.Addr, cfg.LocalState.UpstreamReleaseTagFile)
	w.Start() // blocks forever
}

func runCronMode(w *watcher.VersionWatcher, spec string) error {
	log.Printf("ipdb-manager starting cron mode (spec=%s)", spec)
	if err := w.CheckAndUpdate("startup"); err != nil {
		log.Printf("[watcher] startup reconcile failed: %v", err)
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	c := cron.New(cron.WithLocation(loc))
	if _, err := c.AddFunc(spec, func() {
		if err := w.CheckAndUpdate("scheduled"); err != nil {
			log.Printf("[watcher] scheduled reconcile failed: %v", err)
		}
	}); err != nil {
		return err
	}
	c.Start()
	defer c.Stop()
	select {}
}

// splitHostPort splits "host:port", returning port=8848 on any parse error.
func splitHostPort(addr string) (host string, port uint64) {
	port = 8848
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, port
	}
	host = addr[:idx]
	if p, err := strconv.ParseUint(addr[idx+1:], 10, 64); err == nil {
		port = p
	}
	return
}
