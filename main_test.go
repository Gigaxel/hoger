package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListAndLookup(t *testing.T) {
	path := writeTempHosts(t, `# local hosts
127.0.0.1 localhost
10.10.0.5 api.local web.local # dev services
`)
	var out bytes.Buffer
	a := app{out: &out, err: &bytes.Buffer{}}

	if err := a.run([]string{"-hosts", path, "list"}); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "10.10.0.5  api.local web.local  # dev services  (line 3)") {
		t.Fatalf("list output missing entry:\n%s", got)
	}

	out.Reset()
	if err := a.run([]string{"-hosts", path, "lookup", "api.local", "missing.local"}); err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	got = out.String()
	if !strings.Contains(got, "api.local -> 10.10.0.5 (line 3)") {
		t.Fatalf("lookup output missing hit:\n%s", got)
	}
	if !strings.Contains(got, "missing.local -> not found") {
		t.Fatalf("lookup output missing miss:\n%s", got)
	}
}

func TestAddSetAndRemove(t *testing.T) {
	path := writeTempHosts(t, `# local hosts
127.0.0.1 localhost
10.10.0.5 api.local web.local # dev services
`)
	a := app{out: &bytes.Buffer{}, err: &bytes.Buffer{}}

	if err := a.run([]string{"-hosts", path, "add", "10.10.0.5", "admin.local", "api.local"}); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	assertFileContains(t, path, "10.10.0.5\tapi.local\tweb.local\tadmin.local\t# dev services")

	if err := a.run([]string{"-hosts", path, "set", "api.local", "192.168.1.20"}); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	data := readFile(t, path)
	if strings.Contains(data, "10.10.0.5\tapi.local") {
		t.Fatalf("set left api.local on old IP:\n%s", data)
	}
	assertFileContains(t, path, "192.168.1.20\tapi.local")

	if err := a.run([]string{"-hosts", path, "remove", "web.local", "api.local"}); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	data = readFile(t, path)
	if strings.Contains(data, "web.local") || strings.Contains(data, "api.local") {
		t.Fatalf("remove left hosts behind:\n%s", data)
	}
	assertFileContains(t, path, "10.10.0.5\tadmin.local\t# dev services")
}

func TestRejectsInvalidIP(t *testing.T) {
	path := writeTempHosts(t, "127.0.0.1 localhost\n")
	a := app{out: &bytes.Buffer{}, err: &bytes.Buffer{}}

	err := a.run([]string{"-hosts", path, "add", "not-an-ip", "bad.local"})
	if err == nil {
		t.Fatal("expected invalid IP error")
	}
	if !strings.Contains(err.Error(), "invalid IP address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTempHosts(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp hosts: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data := readFile(t, path)
	if !strings.Contains(data, want) {
		t.Fatalf("file %s missing %q:\n%s", path, want, data)
	}
}
