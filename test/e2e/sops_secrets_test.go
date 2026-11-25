// Copyright (c) The Conn3x Authors
// SPDX-License-Identifier: MPL-2.0

//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A value from a SOPS file reaches a host through conn3x_secrets and a
// write-only argument, and nowhere on disk on the way.
func TestSOPSSecrets(t *testing.T) {
	box := startHost(t, "rocky10")
	knownFile := shareKey(t, box)
	w := newWorkdir(t, "sops_secrets")
	w.write(chainVars{PortA: box.port, Key: box.keyFile, KnownHosts: knownFile})
	sopsFixture(t, w.dir)
	w.mustConn3x("init", "-no-color")

	w.mustConn3x("plan", "-no-color", "-out=p.tfplan")
	w.mustConn3x("apply", "-auto-approve", "-no-color", "p.tfplan")
	if got := box.exec("", "cat", "/etc/db_password"); got != "hunter2" {
		t.Fatalf("db_password = %q", got)
	}
	for _, f := range []string{"terraform.tfstate", "p.tfplan"} {
		raw, err := os.ReadFile(filepath.Join(w.dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "hunter2") {
			t.Errorf("%s holds the secret", f)
		}
	}
	if out := w.mustConn3x("plan", "-no-color"); !strings.Contains(out, "No changes") {
		t.Errorf("a second plan should be clean:\n%s", out)
	}

	t.Setenv("SOPS_AGE_KEY", "")
	if out, code := w.conn3x("plan", "-no-color"); code == 0 || !strings.Contains(out, "no age key found") {
		t.Errorf("without a key, the plan should say so (exit %d):\n%s", code, out)
	}
}

// A conn3x_secrets value is refused in an argument that would keep it in
// plan and state, at validate and at plan, before anything is written.
func TestSOPSSecretsRefusedWhereKept(t *testing.T) {
	for name, body := range map[string]string{
		"file content": `resource "conn3x_file" "f" {
  path    = "/etc/db_password"
  content = ephemeral.conn3x_secrets.app.values["db_password"]
  ssh {
    host = "127.0.0.1"
    user = "root"
  }
}`,
		"terraform_data input": `resource "terraform_data" "d" {
  input = ephemeral.conn3x_secrets.app.values["db_password"]
}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkdir(t, "sops_secrets_kept")
			w.write(body)
			sopsFixture(t, w.dir)
			w.mustConn3x("init", "-no-color")
			for _, args := range [][]string{{"validate", "-no-color"}, {"plan", "-no-color", "-out=p.tfplan"}} {
				out, code := w.conn3x(args...)
				if code == 0 || !strings.Contains(out, "Ephemeral value used in non-ephemeral context") {
					t.Errorf("%s should refuse the ephemeral value (exit %d):\n%s", args[0], code, out)
				}
			}
			if _, err := os.Stat(filepath.Join(w.dir, "p.tfplan")); err == nil {
				t.Error("plan saved a plan file")
			}
		})
	}
}

// sopsFixture copies the test SOPS file into dir and sets the age key that
// decrypts it.
func sopsFixture(t *testing.T, dir string) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	fixtures := filepath.Join(filepath.Dir(here), "..", "..", "internal", "conn3x", "secrets", "testdata")
	key, err := os.ReadFile(filepath.Join(fixtures, "test-key.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY", string(key))
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	src, err := os.ReadFile(filepath.Join(fixtures, "secrets.sops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.sops.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
}
