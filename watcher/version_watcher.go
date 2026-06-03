package watcher

import (
	"archive/tar"
	"compress/gzip"
	"context"
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

	"ipdb-manager/artifact"
	"ipdb-manager/config"
	"ipdb-manager/syncer"
)

// VersionWatcher polls the ip2region GitHub release tag.
// When the tag changes it downloads the new data files and syncs Nacos.
// State is zero-local: version judgment relies on artifact repo existence.
type VersionWatcher struct {
	TXTPath         string
	XDBPath         string
	TXTPathV6       string
	XDBPathV6       string
	PollInterval    time.Duration
	DownloadTimeout time.Duration
	GithubToken     string // optional; prevents hitting the 60 req/h anonymous limit
	ReleasesURL     string
	NacosClient     config_client.IConfigClient
	NacosGroup      string
	NacosDataID     string
	NacosDataIDV6   string

	ArtifactRepos []config.ArtifactRepoConfig
	NacosTargets  []config.NacosTargetConfig

	mu                 sync.Mutex
	githubHTTPClient   *http.Client
	artifactHTTPClient *http.Client
	targetNacosClients map[string]config_client.IConfigClient
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

// ReconcileResult contains details about what was done during a reconcile.
type ReconcileResult struct {
	Version            string `json:"version"`
	ArtifactUploaded   bool   `json:"artifact_uploaded"`
	NacosMetaPublished bool   `json:"nacos_meta_published"`
	Skipped            bool   `json:"skipped"`
	Error              string `json:"error,omitempty"`
}

// ReconcileByTag reconciles a specific version tag.
// It fetches the release from GitHub, checks artifact+nacos state, and fills gaps.
// Concurrency is managed externally (via PG optimistic lock); this method is not mutex-protected.
func (w *VersionWatcher) ReconcileByTag(tag string) *ReconcileResult {
	result := &ReconcileResult{Version: tag}

	httpClient := w.getGitHubHTTPClient()
	targets := w.syncTargets()

	// Check if already fully synced.
	allArtifactsExist, err := w.checkArtifactsExist(tag, targets)
	if err != nil {
		log.Printf("[watcher] reconcile-tag=%s artifact check error: %v", tag, err)
		allArtifactsExist = false
	}

	if allArtifactsExist {
		nacosFullySynced, err := w.checkNacosFullySynced(tag, targets)
		if err != nil {
			log.Printf("[watcher] reconcile-tag=%s nacos check error: %v", tag, err)
			nacosFullySynced = false
		}
		if nacosFullySynced {
			log.Printf("[watcher] reconcile-tag=%s fully synced, nothing to do", tag)
			result.Skipped = true
			return result
		}
	}

	// Fetch release by tag from GitHub.
	rel, err := w.fetchReleaseByTag(httpClient, tag)
	if err != nil {
		result.Error = fmt.Sprintf("fetch release: %v", err)
		return result
	}

	// Download and extract.
	if err := w.downloadAndExtractReleaseData(httpClient, rel, targets); err != nil {
		result.Error = fmt.Sprintf("download: %v", err)
		return result
	}

	// Upload artifacts + publish meta.
	if err := w.publishIP2RegionMeta(targets, tag); err != nil {
		result.Error = fmt.Sprintf("publish meta: %v", err)
		return result
	}
	result.ArtifactUploaded = true
	result.NacosMetaPublished = true

	log.Printf("[watcher] reconcile-tag=%s completed (artifacts+meta ready, use submap/publish to switch)", tag)
	return result
}

// SyncSubnetMapByTag downloads a specific version's release and rebuilds the subnet maps.
// It does NOT upload artifacts or publish ip2region_meta — only syncs subnet_map content.
func (w *VersionWatcher) SyncSubnetMapByTag(tag string) error {
	httpClient := w.getGitHubHTTPClient()
	targets := w.syncTargets()

	rel, err := w.fetchReleaseByTag(httpClient, tag)
	if err != nil {
		return fmt.Errorf("fetch release %s: %w", tag, err)
	}

	if err := w.downloadAndExtractReleaseData(httpClient, rel, targets); err != nil {
		return fmt.Errorf("download %s: %w", tag, err)
	}

	if err := w.runSyncTargets(targets, tag); err != nil {
		return fmt.Errorf("sync subnet maps %s: %w", tag, err)
	}

	log.Printf("[watcher] subnet map sync for version %s completed", tag)
	return nil
}

func (w *VersionWatcher) checkAndUpdateLocked(trigger string) error {
	if trigger == "" {
		trigger = "unknown"
	}
	log.Printf("[watcher] reconcile trigger=%s", trigger)

	httpClient := w.getGitHubHTTPClient()

	// 1. Fetch latest GitHub release tag.
	rel, err := w.fetchLatestRelease(httpClient)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	latestTag := rel.TagName
	log.Printf("[watcher] latest upstream release: %s", latestTag)

	targets := w.syncTargets()

	// 2. Check artifact repo + Nacos state to determine if this version is fully processed.
	allArtifactsExist, err := w.checkArtifactsExist(latestTag, targets)
	if err != nil {
		log.Printf("[watcher] artifact existence check failed (will reprocess): %v", err)
		allArtifactsExist = false
	}

	if allArtifactsExist {
		nacosFullySynced, err := w.checkNacosFullySynced(latestTag, targets)
		if err != nil {
			log.Printf("[watcher] nacos sync check failed (will reprocess): %v", err)
			nacosFullySynced = false
		}
		if nacosFullySynced {
			log.Printf("[watcher] version %s fully synced (artifacts + nacos meta + subnet maps), nothing to do", latestTag)
			return nil
		}
		// Nacos not fully synced — need local files to rebuild. Fall through to download.
		log.Printf("[watcher] version %s in artifact repo but nacos not fully synced, downloading to reconcile...", latestTag)
	} else {
		log.Printf("[watcher] version %s not fully in artifact repo, downloading release...", latestTag)
	}

	// 3. Download release, publish artifacts + meta, sync subnet maps.
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
	log.Printf("[watcher] update complete, current version: %s", latestTag)
	return nil
}

func (w *VersionWatcher) checkArtifactsExist(version string, targets []syncTarget) (bool, error) {
	if len(w.NacosTargets) == 0 || len(w.ArtifactRepos) == 0 {
		return false, nil
	}

	repoByID := make(map[string]config.ArtifactRepoConfig, len(w.ArtifactRepos))
	for _, r := range w.ArtifactRepos {
		repoByID[r.ID] = r
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, nacosTarget := range w.NacosTargets {
		if !nacosTarget.Enabled {
			continue
		}
		repo, ok := repoByID[nacosTarget.ArtifactRepoID]
		if !ok || !repo.Enabled {
			continue
		}

		creds, _, err := resolveArtifactCredentials(repo.Auth)
		if err != nil {
			return false, fmt.Errorf("target=%s resolve creds: %w", nacosTarget.ID, err)
		}
		client, err := artifact.NewClient(repo, creds, artifact.FactoryOptions{
			HTTPClient: w.getArtifactHTTPClient(),
		})
		if err != nil {
			return false, fmt.Errorf("target=%s new client: %w", nacosTarget.ID, err)
		}

		for _, st := range targets {
			tpl, _ := selectTargetFamilyConfig(st.name, nacosTarget)
			if tpl == "" {
				continue
			}
			objectPath := strings.ReplaceAll(strings.TrimSpace(tpl), "{{version}}", version)
			exists, err := client.ObjectExists(ctx, objectPath)
			if err != nil {
				return false, fmt.Errorf("target=%s family=%s check: %w", nacosTarget.ID, st.name, err)
			}
			if !exists {
				return false, nil
			}
		}
	}
	return true, nil
}

func (w *VersionWatcher) checkNacosFullySynced(version string, targets []syncTarget) (bool, error) {
	// Check each NacosTarget: versioned meta dataId must exist.
	for _, nacosTarget := range w.NacosTargets {
		if !nacosTarget.Enabled {
			continue
		}
		nacosUser := resolveSecret(nacosTarget.Auth.UsernameRef)
		nacosPass := resolveSecret(nacosTarget.Auth.PasswordRef)
		if nacosUser == "" || nacosPass == "" {
			continue
		}
		client, err := w.getOrCreateTargetNacosClient(nacosTarget.ServerAddr, nacosTarget.Namespace, nacosUser, nacosPass)
		if err != nil {
			return false, fmt.Errorf("target=%s nacos client: %w", nacosTarget.ID, err)
		}
		for _, st := range targets {
			_, ref := selectTargetFamilyConfig(st.name, nacosTarget)
			if ref.DataID == "" || ref.Group == "" {
				continue
			}
			versionedDataID := ref.DataID + "_" + version
			content, err := client.GetConfig(vo.ConfigParam{DataId: versionedDataID, Group: ref.Group})
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "config data not exist") {
					return false, nil
				}
				return false, fmt.Errorf("target=%s get %s/%s: %w", nacosTarget.ID, ref.Group, versionedDataID, err)
			}
			if strings.TrimSpace(content) == "" {
				return false, nil
			}
		}
	}

	return true, nil
}

func (w *VersionWatcher) getGitHubHTTPClient() *http.Client {
	if w.githubHTTPClient == nil {
		w.githubHTTPClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
	return w.githubHTTPClient
}

func (w *VersionWatcher) getArtifactHTTPClient() *http.Client {
	if w.artifactHTTPClient == nil {
		timeout := w.DownloadTimeout
		if timeout <= 0 {
			timeout = 300 * time.Second
		}
		w.artifactHTTPClient = &http.Client{Timeout: timeout}
	}
	return w.artifactHTTPClient
}

func (w *VersionWatcher) getDownloadHTTPClient() *http.Client {
	timeout := w.DownloadTimeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func (w *VersionWatcher) getOrCreateTargetNacosClient(addr, namespace, username, password string) (config_client.IConfigClient, error) {
	if w.targetNacosClients == nil {
		w.targetNacosClients = make(map[string]config_client.IConfigClient)
	}
	key := strings.Join([]string{addr, namespace, username, password}, "|")
	if client, ok := w.targetNacosClients[key]; ok {
		return client, nil
	}
	client, err := newNacosConfigClient(addr, namespace, username, password)
	if err != nil {
		return nil, err
	}
	w.targetNacosClients[key] = client
	return client, nil
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

func (w *VersionWatcher) fetchReleaseByTag(hc *http.Client, tag string) (*githubRelease, error) {
	baseURL := strings.TrimSuffix(w.ReleasesURL, "/latest")
	tagURL := baseURL + "/tags/" + tag

	req, err := http.NewRequest("GET", tagURL, nil)
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
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release tag %q not found on GitHub", tag)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d for tag %s", resp.StatusCode, tag)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	rel.TagName = strings.TrimSpace(rel.TagName)
	rel.TarballURL = strings.TrimSpace(rel.TarballURL)
	if rel.TagName == "" || rel.TarballURL == "" {
		return nil, fmt.Errorf("invalid release payload for tag %s", tag)
	}
	return &rel, nil
}

func (w *VersionWatcher) downloadAndExtractReleaseData(hc *http.Client, rel *githubRelease, targets []syncTarget) error {
	if len(targets) == 0 {
		return nil
	}

	dlClient := w.getDownloadHTTPClient()

	req, err := http.NewRequest("GET", rel.TarballURL, nil)
	if err != nil {
		return err
	}
	if w.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.GithubToken)
	}

	resp, err := dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", rel.TarballURL, resp.StatusCode)
	}

	tmpTar, err := os.CreateTemp("", "ip2region-*.tar.gz")
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
	stageDir, err := os.MkdirTemp("", "ip2region-extract-*")
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
