package watcher

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"ipdb-manager/config"
	"ipdb-manager/syncer"
)

// VersionWatcher polls the ip2region GitHub release tag.
// When the tag changes it downloads the new data files and syncs Nacos.
type VersionWatcher struct {
	TXTPath       string
	XDBPath       string
	TXTPathV6     string
	XDBPathV6     string
	VersionFile   string // persisted local upstream release tag
	LegacyVersion string // old local version file path for compatibility migration
	PollInterval  time.Duration
	GithubToken   string // optional; prevents hitting the 60 req/h anonymous limit
	ReleasesURL   string
	NacosClient   config_client.IConfigClient
	NacosGroup    string
	NacosDataID   string
	NacosDataIDV6 string

	ArtifactRepos []config.ArtifactRepoConfig
	NacosTargets  []config.NacosTargetConfig

	mu sync.Mutex
}

type syncTarget struct {
	name      string
	txtPath   string
	xdbPath   string
	dataID    string
	version   *xdb.Version
	txtSuffix string
	xdbSuffix string
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	TarballURL string `json:"tarball_url"`
}

// Start checks once on startup, then polls on PollInterval. Blocks forever.
func (w *VersionWatcher) Start() {
	if err := w.CheckAndUpdate("startup"); err != nil {
		log.Printf("[watcher] startup check failed: %v", err)
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := w.CheckAndUpdate("scheduled"); err != nil {
			log.Printf("[watcher] check failed: %v", err)
		}
	}
}

// CheckAndUpdate executes one reconcile cycle.
func (w *VersionWatcher) CheckAndUpdate(trigger string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.checkAndUpdateLocked(trigger)
}

// TryCheckAndUpdate executes one reconcile cycle if no other run is active.
// Returns false,nil when another run is already in progress.
func (w *VersionWatcher) TryCheckAndUpdate(trigger string) (bool, error) {
	if !w.mu.TryLock() {
		return false, nil
	}
	defer w.mu.Unlock()
	return true, w.checkAndUpdateLocked(trigger)
}

// TryStartBackground starts one reconcile run in background if idle.
func (w *VersionWatcher) TryStartBackground(trigger string) bool {
	if !w.mu.TryLock() {
		return false
	}
	go func() {
		defer w.mu.Unlock()
		if err := w.checkAndUpdateLocked(trigger); err != nil {
			log.Printf("[watcher] reconcile trigger=%s failed: %v", trigger, err)
		}
	}()
	return true
}

func (w *VersionWatcher) checkAndUpdateLocked(trigger string) error {
	if trigger == "" {
		trigger = "unknown"
	}
	if err := w.migrateLegacyVersionFile(); err != nil {
		return fmt.Errorf("prepare local release tag file: %w", err)
	}
	log.Printf("[watcher] reconcile trigger=%s", trigger)

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// 1. Fetch latest GitHub release tag.
	rel, err := w.fetchLatestRelease(httpClient)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	latestTag := rel.TagName

	// 2. Check local version.
	localTag := w.readLocalVersion()

	targets := w.syncTargets()

	// manual 模式：跳过下载，直接用现有文件同步 Nacos，版本号保持 manual 不变。
	if localTag == "manual" {
		log.Printf("[watcher] manual mode, latest upstream=%s, syncing nacos with existing files...", latestTag)
		if err := w.publishIP2RegionMeta(targets, latestTag); err != nil {
			return err
		}
		return w.runSyncTargets(targets, "")
	}

	versionChanged := localTag != latestTag
	missingTargets := make([]syncTarget, 0, len(targets))
	for _, t := range targets {
		if targetFilesMissing(t) {
			missingTargets = append(missingTargets, t)
		}
	}

	if !versionChanged && len(missingTargets) == 0 {
		log.Printf("[watcher] already at latest (%s), running reconcile for nacos meta and subnet maps", latestTag)
		if err := w.publishIP2RegionMeta(targets, latestTag); err != nil {
			return err
		}
		if err := w.runSyncTargets(targets, latestTag); err != nil {
			return err
		}
		log.Printf("[watcher] reconcile complete at latest version: %s", latestTag)
		return nil
	}

	if versionChanged {
		log.Printf("[watcher] version %q → %q, downloading full release files...", localTag, latestTag)
		if err := w.downloadAndExtractReleaseData(httpClient, rel, targets); err != nil {
			return err
		}
		log.Printf("[watcher] release files downloaded and extracted")

		if err := w.publishIP2RegionMeta(targets, latestTag); err != nil {
			return err
		}

		if err := w.runSyncTargets(targets, latestTag); err != nil {
			return err
		}

		if err := w.writeLocalVersion(latestTag); err != nil {
			log.Printf("[watcher] warning: write local version failed: %v", err)
		}
		log.Printf("[watcher] update complete, current version: %s", latestTag)
		return nil
	}

	missingNames := make([]string, 0, len(missingTargets))
	for _, t := range missingTargets {
		missingNames = append(missingNames, t.name)
	}
	sort.Strings(missingNames)
	log.Printf("[watcher] local files missing for %s at version %s, repairing from release...",
		strings.Join(missingNames, ","), latestTag)

	if err := w.downloadAndExtractReleaseData(httpClient, rel, missingTargets); err != nil {
		return err
	}
	log.Printf("[watcher] missing release files repaired")

	if err := w.publishIP2RegionMeta(missingTargets, latestTag); err != nil {
		return err
	}

	if err := w.runSyncTargets(missingTargets, latestTag); err != nil {
		return err
	}
	log.Printf("[watcher] missing data sync complete at version: %s", latestTag)
	return nil
}

func newNacosConfigClient(addr, namespace, username, password string) (config_client.IConfigClient, error) {
	host, port := splitHostPort(addr)
	sc := []constant.ServerConfig{*constant.NewServerConfig(host, port)}
	cc := *constant.NewClientConfig(
		constant.WithNamespaceId(namespace),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("warn"),
		constant.WithUsername(username),
		constant.WithPassword(password),
	)
	return clients.NewConfigClient(vo.NacosClientParam{ClientConfig: &cc, ServerConfigs: sc})
}

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

func (w *VersionWatcher) migrateLegacyVersionFile() error {
	if w.VersionFile == "" {
		return fmt.Errorf("version file path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(w.VersionFile), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(w.VersionFile); err == nil {
		return nil
	}
	if w.LegacyVersion == "" || w.LegacyVersion == w.VersionFile {
		return nil
	}
	data, err := os.ReadFile(w.LegacyVersion)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	tag := strings.TrimSpace(string(data))
	if tag == "" {
		return nil
	}
	if err := os.WriteFile(w.VersionFile, []byte(tag+"\n"), 0644); err != nil {
		return err
	}
	log.Printf("[watcher] migrated legacy release tag file %s -> %s", w.LegacyVersion, w.VersionFile)
	return nil
}

func (w *VersionWatcher) syncTargets() []syncTarget {
	return []syncTarget{
		{
			name:      "v4",
			txtPath:   w.TXTPath,
			xdbPath:   w.XDBPath,
			dataID:    w.NacosDataID,
			version:   xdb.IPv4,
			txtSuffix: "data/ipv4_source.txt",
			xdbSuffix: "data/ip2region_v4.xdb",
		},
		{
			name:      "v6",
			txtPath:   w.TXTPathV6,
			xdbPath:   w.XDBPathV6,
			dataID:    w.NacosDataIDV6,
			version:   xdb.IPv6,
			txtSuffix: "data/ipv6_source.txt",
			xdbSuffix: "data/ip2region_v6.xdb",
		},
	}
}

func targetFilesMissing(t syncTarget) bool {
	if _, err := os.Stat(t.txtPath); err != nil {
		return true
	}
	if _, err := os.Stat(t.xdbPath); err != nil {
		return true
	}
	return false
}

func (w *VersionWatcher) runSyncTargets(targets []syncTarget, versionTag string) error {
	for _, t := range targets {
		if err := w.runSyncOne(t, versionTag); err != nil {
			return fmt.Errorf("sync %s nacos: %w", t.name, err)
		}
	}
	return nil
}

func (w *VersionWatcher) runSyncOne(t syncTarget, versionTag string) error {
	if _, err := os.Stat(t.txtPath); os.IsNotExist(err) {
		log.Printf("[watcher] skip sync for %s: txt not found", t.dataID)
		return nil
	}
	if _, err := os.Stat(t.xdbPath); os.IsNotExist(err) {
		log.Printf("[watcher] skip sync for %s: xdb not found", t.dataID)
		return nil
	}

	s := &syncer.Syncer{
		NacosClient: w.NacosClient,
		NacosGroup:  w.NacosGroup,
		NacosDataID: t.dataID,
		MetaDataID:  t.dataID + "_meta",
		TXTPath:     t.txtPath,
		XDBPath:     t.xdbPath,
		XDBVersion:  t.version,
		VersionTag:  versionTag,
	}
	if err := s.Sync(); err != nil {
		return fmt.Errorf("sync nacos: %w", err)
	}
	return nil
}

func (w *VersionWatcher) fetchLatestRelease(hc *http.Client) (*githubRelease, error) {
	req, err := http.NewRequest("GET", w.ReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if w.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.GithubToken)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	rel.TagName = strings.TrimSpace(rel.TagName)
	rel.TarballURL = strings.TrimSpace(rel.TarballURL)
	if rel.TagName == "" || rel.TarballURL == "" {
		return nil, fmt.Errorf("invalid latest release payload")
	}
	return &rel, nil
}

func (w *VersionWatcher) readLocalVersion() string {
	data, _ := os.ReadFile(w.VersionFile)
	return strings.TrimSpace(string(data))
}

func (w *VersionWatcher) writeLocalVersion(tag string) error {
	return os.WriteFile(w.VersionFile, []byte(tag+"\n"), 0644)
}

func (w *VersionWatcher) downloadAndExtractReleaseData(hc *http.Client, rel *githubRelease, targets []syncTarget) error {
	if len(targets) == 0 {
		return nil
	}

	req, err := http.NewRequest("GET", rel.TarballURL, nil)
	if err != nil {
		return err
	}
	if w.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.GithubToken)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", rel.TarballURL, resp.StatusCode)
	}

	tmpTar, err := os.CreateTemp(filepath.Dir(w.VersionFile), "ip2region-*.tar.gz")
	if err != nil {
		return err
	}
	tmpTarPath := tmpTar.Name()
	defer os.Remove(tmpTarPath)

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpTar, h), resp.Body); err != nil {
		tmpTar.Close()
		return err
	}
	if err := tmpTar.Close(); err != nil {
		return err
	}
	log.Printf("[watcher] release tarball sha256:%x", h.Sum(nil))

	tarFile, err := os.Open(tmpTarPath)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gzr, err := gzip.NewReader(tarFile)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	needed := make(map[string]string, len(targets)*2)
	for _, t := range targets {
		needed[t.txtSuffix] = t.txtPath
		needed[t.xdbSuffix] = t.xdbPath
	}
	stageDir, err := os.MkdirTemp(filepath.Dir(w.VersionFile), "ip2region-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	stageFiles := make(map[string]string, len(needed))
	for suffix, destPath := range needed {
		stageFiles[suffix] = filepath.Join(stageDir, filepath.Base(destPath))
	}

	written := make(map[string]bool, len(needed))

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		name := strings.TrimPrefix(hdr.Name, "./")
		for suffix := range needed {
			if !strings.HasSuffix(name, suffix) {
				continue
			}
			if written[suffix] {
				continue
			}
			if err := writeReaderToFile(tr, stageFiles[suffix]); err != nil {
				return fmt.Errorf("extract %s: %w", suffix, err)
			}
			written[suffix] = true
			log.Printf("[watcher] extracted %s -> %s", suffix, stageFiles[suffix])
			break
		}
	}

	if len(written) != len(needed) {
		missing := make([]string, 0, len(needed)-len(written))
		for suffix := range needed {
			if !written[suffix] {
				missing = append(missing, suffix)
			}
		}
		sort.Strings(missing)
		return fmt.Errorf("release tarball missing required files: %s", strings.Join(missing, ", "))
	}

	for suffix, destPath := range needed {
		srcPath := stageFiles[suffix]
		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("open staged %s: %w", suffix, err)
		}
		if err := writeReaderToFile(src, destPath); err != nil {
			src.Close()
			return fmt.Errorf("install %s: %w", suffix, err)
		}
		src.Close()
		log.Printf("[watcher] installed %s -> %s", suffix, destPath)
	}

	return nil
}

func writeReaderToFile(r io.Reader, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, h), r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	log.Printf("[watcher] %s sha256:%x", filepath.Base(destPath), h.Sum(nil))
	return os.Rename(tmp, destPath)
}
