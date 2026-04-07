package artifact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ipdb-manager/config"
)

func TestFactoryCreatesJfrogAndNexus(t *testing.T) {
	repoJ := config.ArtifactRepoConfig{Type: "jfrog", BaseURL: "http://example.com", Repo: "x"}
	repoN := config.ArtifactRepoConfig{Type: "nexus", BaseURL: "http://example.com", Repo: "x"}
	j, err := NewClient(repoJ, Credentials{}, FactoryOptions{})
	if err != nil || j.Type() != "jfrog" {
		t.Fatalf("jfrog client create failed: %v", err)
	}
	n, err := NewClient(repoN, Credentials{}, FactoryOptions{})
	if err != nil || n.Type() != "nexus" {
		t.Fatalf("nexus client create failed: %v", err)
	}
}

func TestJfrogUploadAndExistsWithToken(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	tmp := t.TempDir()
	f := filepath.Join(tmp, "a.xdb")
	if err := os.WriteFile(f, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewJFrogClient(ts.URL, "ipdb-local", Credentials{Token: "tok"}, ts.Client())
	if _, err := c.UploadFile(context.Background(), "ip2region/v1/a.xdb", f); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", gotMethod)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if !strings.Contains(gotPath, "/ipdb-local/ip2region/v1/a.xdb") {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	ok, err := c.ObjectExists(context.Background(), "ip2region/v1/a.xdb")
	if err != nil || !ok {
		t.Fatalf("exists expected true got ok=%v err=%v", ok, err)
	}
}

func TestNexusUploadAndExistsWithBasicAuth(t *testing.T) {
	var gotAuth, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	tmp := t.TempDir()
	f := filepath.Join(tmp, "b.xdb")
	if err := os.WriteFile(f, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewNexusClient(ts.URL, "raw-hosted", Credentials{Username: "u", Password: "p"}, ts.Client())
	if _, err := c.UploadFile(context.Background(), "ip2region/v1/b.xdb", f); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("expected basic auth header, got %s", gotAuth)
	}
	if !strings.Contains(gotPath, "/repository/raw-hosted/ip2region/v1/b.xdb") {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	ok, err := c.ObjectExists(context.Background(), "ip2region/v1/b.xdb")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if ok {
		t.Fatalf("expected not found false")
	}
}
