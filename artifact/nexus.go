package artifact

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

type NexusClient struct {
	baseURL    string
	repo       string
	creds      Credentials
	httpClient *http.Client
}

func NewNexusClient(baseURL, repo string, creds Credentials, hc *http.Client) *NexusClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &NexusClient{baseURL: strings.TrimRight(baseURL, "/"), repo: repo, creds: creds, httpClient: hc}
}

func (c *NexusClient) Type() string { return "nexus" }

func (c *NexusClient) UploadFile(ctx context.Context, objectPath, localFile string) (string, error) {
	f, err := os.Open(localFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	u := c.objectURL(objectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, f)
	if err != nil {
		return "", err
	}
	c.applyAuth(req)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("upload failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return u, nil
}

func (c *NexusClient) ObjectExists(ctx context.Context, objectPath string) (bool, error) {
	u := c.objectURL(objectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	c.applyAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, fmt.Errorf("head failed: status=%d", resp.StatusCode)
}

func (c *NexusClient) objectURL(objectPath string) string {
	clean := strings.TrimLeft(path.Clean("/"+objectPath), "/")
	base := c.baseURL
	if !strings.Contains(base, "/repository/") {
		base = strings.TrimRight(base, "/") + "/repository"
	}
	return base + "/" + strings.Trim(c.repo, "/") + "/" + clean
}

func (c *NexusClient) applyAuth(req *http.Request) {
	if c.creds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.Token)
		return
	}
	if c.creds.Username != "" {
		req.SetBasicAuth(c.creds.Username, c.creds.Password)
	}
}
