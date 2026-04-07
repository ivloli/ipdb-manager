package artifact

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"ipdb-manager/config"
)

type Client interface {
	Type() string
	UploadFile(ctx context.Context, objectPath, localFile string) (string, error)
	ObjectExists(ctx context.Context, objectPath string) (bool, error)
}

type Credentials struct {
	Token    string
	Username string
	Password string
}

type FactoryOptions struct {
	HTTPClient *http.Client
}

func NewClient(repo config.ArtifactRepoConfig, creds Credentials, opts FactoryOptions) (Client, error) {
	t := strings.ToLower(strings.TrimSpace(repo.Type))
	switch t {
	case "jfrog":
		return NewJFrogClient(repo.BaseURL, repo.Repo, creds, opts.HTTPClient), nil
	case "nexus":
		return NewNexusClient(repo.BaseURL, repo.Repo, creds, opts.HTTPClient), nil
	default:
		return nil, fmt.Errorf("unsupported artifact repo type: %s", repo.Type)
	}
}
