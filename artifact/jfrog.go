package artifact

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

type JFrogClient struct {
	baseURL    string
	repo       string
	creds      Credentials
	httpClient *http.Client
}

func NewJFrogClient(baseURL, repo string, creds Credentials, hc *http.Client) *JFrogClient {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &JFrogClient{baseURL: strings.TrimRight(baseURL, "/"), repo: repo, creds: creds, httpClient: hc}
}

func (c *JFrogClient) Type() string { return "jfrog" }

func (c *JFrogClient) UploadFile(ctx context.Context, objectPath, localFile string) (string, error) {
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

func (c *JFrogClient) ObjectExists(ctx context.Context, objectPath string) (bool, error) {
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

func (c *JFrogClient) objectURL(objectPath string) string {
	clean := strings.TrimLeft(path.Clean("/"+objectPath), "/")
	u := c.baseURL + "/"
	if c.repo != "" {
		u += strings.Trim(c.repo, "/") + "/"
	}
	u += clean
	return escapeURL(u)
}

func (c *JFrogClient) applyAuth(req *http.Request) {
	if c.creds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.Token)
		return
	}
	if c.creds.Username != "" {
		req.SetBasicAuth(c.creds.Username, c.creds.Password)
	}
}

func escapeURL(raw string) string {
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
