package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"ipdb-manager/artifact"
	"ipdb-manager/config"
)

type ip2regionMeta struct {
	Version     string `json:"version"`
	XDBURL      string `json:"xdb_url"`
	XDBSHA256   string `json:"xdb_sha256"`
	XDBAuthUser string `json:"xdb_auth_user,omitempty"`
}

func (w *VersionWatcher) publishIP2RegionMeta(targets []syncTarget, version string) error {
	if len(w.NacosTargets) == 0 {
		return nil
	}
	if len(targets) == 0 {
		return nil
	}

	repoByID := make(map[string]config.ArtifactRepoConfig, len(w.ArtifactRepos))
	for _, r := range w.ArtifactRepos {
		repoByID[r.ID] = r
	}

	var errs []string
	for _, nacosTarget := range w.NacosTargets {
		if !nacosTarget.Enabled {
			continue
		}
		repo, ok := repoByID[nacosTarget.ArtifactRepoID]
		if !ok {
			errs = append(errs, fmt.Sprintf("target=%s repo=%s not found", nacosTarget.ID, nacosTarget.ArtifactRepoID))
			continue
		}
		if !repo.Enabled {
			errs = append(errs, fmt.Sprintf("target=%s repo=%s disabled", nacosTarget.ID, nacosTarget.ArtifactRepoID))
			continue
		}

		if err := w.publishOneTarget(targets, version, nacosTarget, repo); err != nil {
			errs = append(errs, fmt.Sprintf("target=%s: %v", nacosTarget.ID, err))
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("publish xdb meta failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (w *VersionWatcher) publishOneTarget(targets []syncTarget, version string, nacosTarget config.NacosTargetConfig, repo config.ArtifactRepoConfig) error {
	repoCreds, authUser, err := resolveArtifactCredentials(repo.Auth)
	if err != nil {
		return err
	}
	artifactClient, err := artifact.NewClient(repo, repoCreds, artifact.FactoryOptions{
		HTTPClient: &http.Client{Timeout: 90 * time.Second},
	})
	if err != nil {
		return err
	}

	nacosUser := resolveSecret(nacosTarget.Auth.UsernameRef)
	nacosPass := resolveSecret(nacosTarget.Auth.PasswordRef)
	if nacosUser == "" || nacosPass == "" {
		return fmt.Errorf("nacos auth env not set: username_ref=%s password_ref=%s", nacosTarget.Auth.UsernameRef, nacosTarget.Auth.PasswordRef)
	}
	nacosClient, err := newNacosConfigClient(nacosTarget.ServerAddr, nacosTarget.Namespace, nacosUser, nacosPass)
	if err != nil {
		return err
	}

	for _, st := range targets {
		if err := publishOneFamily(version, st, nacosTarget, repo, artifactClient, nacosClient, authUser); err != nil {
			return err
		}
	}
	return nil
}

func publishOneFamily(version string, st syncTarget, nacosTarget config.NacosTargetConfig, repo config.ArtifactRepoConfig, artifactClient artifact.Client, nacosClient config_client.IConfigClient, authUser string) error {
	tpl, ref := selectTargetFamilyConfig(st.name, nacosTarget)
	if tpl == "" || ref.Group == "" || ref.DataID == "" {
		return fmt.Errorf("family=%s publish config is incomplete", st.name)
	}

	if _, err := os.Stat(st.xdbPath); err != nil {
		return fmt.Errorf("family=%s missing xdb file %s: %w", st.name, st.xdbPath, err)
	}

	objectPath := strings.ReplaceAll(strings.TrimSpace(tpl), "{{version}}", version)
	if objectPath == "" {
		return fmt.Errorf("family=%s artifact path template resolved empty", st.name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	artifactURL, err := ensureArtifactFile(ctx, artifactClient, repo, objectPath, st.xdbPath)
	if err != nil {
		return fmt.Errorf("family=%s upload artifact: %w", st.name, err)
	}
	sha, err := fileSHA256(st.xdbPath)
	if err != nil {
		return fmt.Errorf("family=%s sha256: %w", st.name, err)
	}

	payload := ip2regionMeta{Version: version, XDBURL: artifactURL, XDBSHA256: sha, XDBAuthUser: authUser}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("family=%s marshal meta: %w", st.name, err)
	}
	ok, err := nacosClient.PublishConfig(vo.ConfigParam{
		DataId:  ref.DataID,
		Group:   ref.Group,
		Content: string(b),
	})
	if err != nil {
		return fmt.Errorf("family=%s publish nacos %s/%s: %w", st.name, ref.Group, ref.DataID, err)
	}
	if !ok {
		return fmt.Errorf("family=%s publish nacos %s/%s returned false", st.name, ref.Group, ref.DataID)
	}
	log.Printf("[watcher] published ip2region_meta target=%s family=%s version=%s url=%s", nacosTarget.ID, st.name, version, artifactURL)
	return nil
}

func selectTargetFamilyConfig(family string, t config.NacosTargetConfig) (string, config.NacosPublishMetaRef) {
	if family == "v6" {
		return t.ArtifactPathTemplates.V6, t.Publish.V6
	}
	return t.ArtifactPathTemplates.V4, t.Publish.V4
}

func resolveArtifactCredentials(auth config.ArtifactAuthConfig) (artifact.Credentials, string, error) {
	if auth.TokenRef != "" {
		token := resolveSecret(auth.TokenRef)
		if token == "" {
			return artifact.Credentials{}, "", fmt.Errorf("artifact token env not set: token_ref=%s", auth.TokenRef)
		}
		return artifact.Credentials{Token: token}, "", nil
	}
	user := resolveSecret(auth.UsernameRef)
	pass := resolveSecret(auth.PasswordRef)
	if user == "" || pass == "" {
		return artifact.Credentials{}, "", fmt.Errorf("artifact basic auth env not set: username_ref=%s password_ref=%s", auth.UsernameRef, auth.PasswordRef)
	}
	return artifact.Credentials{Username: user, Password: pass}, user, nil
}

func resolveSecret(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv(ref)); v != "" {
		return v
	}
	return ref
}

func ensureArtifactFile(ctx context.Context, c artifact.Client, repo config.ArtifactRepoConfig, objectPath, localFile string) (string, error) {
	exists, err := c.ObjectExists(ctx, objectPath)
	if err != nil {
		return "", err
	}
	if exists {
		return buildArtifactURL(repo, objectPath), nil
	}
	return c.UploadFile(ctx, objectPath, localFile)
}

func buildArtifactURL(repo config.ArtifactRepoConfig, objectPath string) string {
	clean := strings.TrimLeft(path.Clean("/"+objectPath), "/")
	base := strings.TrimRight(repo.BaseURL, "/")
	switch strings.ToLower(strings.TrimSpace(repo.Type)) {
	case "nexus":
		if !strings.Contains(base, "/repository/") {
			base += "/repository"
		}
		return base + "/" + strings.Trim(repo.Repo, "/") + "/" + clean
	default:
		u := base + "/"
		if repo.Repo != "" {
			u += strings.Trim(repo.Repo, "/") + "/"
		}
		u += clean
		return escapeURLPath(u)
	}
}

func escapeURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parts := strings.Split(u.Path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	u.Path = strings.Join(parts, "/")
	return u.String()
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
