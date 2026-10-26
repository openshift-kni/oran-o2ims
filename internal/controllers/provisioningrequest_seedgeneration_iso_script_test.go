/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type seedGenerationISOScriptFixture struct {
	root, remote, termination, script string
	env                               []string
}

func writeISOTestFile(t *testing.T, filename, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func newSeedGenerationISOScriptFixture(t *testing.T) *seedGenerationISOScriptFixture {
	t.Helper()
	root := t.TempDir()
	f := &seedGenerationISOScriptFixture{root: root, remote: root + "/remote/seedgen/run-123", termination: root + "/termination"}
	credentials := root + "/credentials"
	for name, value := range map[string]string{
		"upload/host": "example.test", "upload/username": "uploader", "upload/remotePath": root + "/remote",
		"upload/privateKey": "test-private-key-must-be-redacted", "upload/knownHosts": "example.test pinned-key",
		"config/config.json":                          `{"auths":{"example.test":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("testuser:testpassword")) + `"}}}`,
		"config/image-based-installation-config.yaml": "pullSecret: sensitive-config-value\n",
	} {
		writeISOTestFile(t, credentials+"/"+name, value, 0o600)
	}
	bin := root + "/bin"
	scripts := map[string]string{
		"ssh": `command="${!#}"
if [[ "$TEST_FAILURE" == ssh && "$command" == true ]]; then exit 1; fi
if [[ "$TEST_FAILURE" == remote-digest && "$command" == sha256sum* ]]; then echo invalid; exit 0; fi
exec /bin/bash -c "$command"`,
		"oc": `for arg in "$@"; do
 if [[ "$arg" == --to=* ]]; then cp "$TEST_INSTALLER" "${arg#--to=}/openshift-install"; fi
done`,
		"scp": `if [[ "$TEST_FAILURE" == upload ]]; then echo 'copy failed'; exit 1; fi
if [[ "$TEST_FAILURE" == killed ]]; then
 touch "$TEST_READY"
 sleep 60
fi
if [[ "$TEST_FAILURE" == blocked ]]; then
 touch "$TEST_READY"
 while [[ ! -f "$TEST_RELEASE" ]]; do sleep 0.01; done
fi
args=("$@")
last=$(($#-1)); previous=$(($#-2))
cp "${args[$previous]}" "${args[$last]#*:}"`,
		"curl": `if [[ "$TEST_FAILURE" == https ]]; then echo 'retrieval failed' >&2; exit 22; fi
if [[ "$TEST_FAILURE" == https-digest ]]; then echo wrong-content; exit 0; fi
cat "$TEST_REMOTE/rhcos-ibi.iso"`,
	}
	for name, body := range scripts {
		writeISOTestFile(t, bin+"/"+name, "#!/bin/bash\nset -eu\n"+body+"\n", 0o755)
	}
	installer := root + "/installer"
	writeISOTestFile(t, installer, `#!/bin/bash
set -eu
if [[ "$TEST_FAILURE" == installer ]]; then
 cat "$TEST_CREDENTIALS/config/config.json"
 cat "$TEST_CREDENTIALS/config/image-based-installation-config.yaml"
 cat "$TEST_CREDENTIALS/upload/privateKey"
 echo 'testuser:testpassword'
 echo 'testpassword'
 echo 'installation failed: useful context'
 exit 1
fi
for arg in "$@"; do
 if [[ "$arg" == --dir=* ]]; then echo test-iso-content > "${arg#--dir=}/rhcos-ibi.iso"; fi
done
`, 0o755)
	script := strings.NewReplacer("/workspace", root+"/workspace", "/var/run/seedgen", credentials,
		"/tmp/seedgen", root+"/temp", "/dev/termination-log", f.termination).Replace(seedGenerationISOJobScript())
	f.script = root + "/job.sh"
	writeISOTestFile(t, f.script, script, 0o700)
	f.env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "ISO_PR_UID=run-123",
		"ISO_URL=https://example.test/seedgen/run-123/rhcos-ibi.iso", "ISO_RELEASE_IMAGE=example.test/release@sha256:digest",
		"TEST_INSTALLER="+installer, "TEST_REMOTE="+f.remote, "TEST_CREDENTIALS="+credentials, "TEST_READY="+root+"/ready", "TEST_RELEASE="+root+"/release")
	return f
}

func (f *seedGenerationISOScriptFixture) command(ctx context.Context, failure string) *exec.Cmd {
	command := exec.CommandContext(ctx, "/bin/bash", f.script) //nolint:gosec // Executes the embedded script in a private test directory.
	command.Env = append(append([]string{}, f.env...), "TEST_FAILURE="+failure)
	return command
}

func TestSeedGenerationISOJobScriptExecution(t *testing.T) {
	for _, tc := range []struct{ failure, stage string }{
		{"", ""}, {"installer", "building-iso"}, {"upload", "uploading-iso"}, {"ssh", "connecting-to-upload-server"},
		{"remote-digest", "checking-remote-digest"}, {"https", "verifying-https-content"}, {"https-digest", "checking-https-digest"},
	} {
		t.Run("failure="+tc.failure, func(t *testing.T) {
			f := newSeedGenerationISOScriptFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			output, err := f.command(ctx, tc.failure).CombinedOutput()
			termination, readErr := os.ReadFile(f.termination)
			if readErr != nil {
				t.Fatalf("termination: %v, output: %s", readErr, output)
			}
			if tc.failure == "" {
				if err != nil {
					t.Fatalf("script failed: %v: %s", err, output)
				}
				if !strings.HasPrefix(string(termination), "https://example.test/seedgen/run-123/rhcos-ibi.iso\nsha256:") {
					t.Fatalf("unexpected result: %s", termination)
				}
				for name, want := range map[string]os.FileMode{filepath.Dir(f.remote): 0o755, f.remote: 0o755, f.remote + "/rhcos-ibi.iso": 0o644} {
					info, err := os.Stat(name)
					if err != nil {
						t.Fatal(err)
					}
					if info.Mode().Perm() != want {
						t.Fatalf("%s mode %o, expected %o", name, info.Mode().Perm(), want)
					}
				}
			} else {
				if err == nil || strings.TrimSpace(string(termination)) != "SEEDGEN_FAILURE="+tc.stage {
					t.Fatalf("failure result: err=%v, termination=%s output=%s", err, termination, output)
				}
				if _, err := os.Stat(f.remote); !os.IsNotExist(err) {
					t.Fatalf("failed run left remote artifacts: %v", err)
				}
			}
			for _, secret := range []string{"test-private-key-must-be-redacted", "testuser:testpassword", "testpassword", "sensitive-config-value", base64.StdEncoding.EncodeToString([]byte("testuser:testpassword"))} {
				if strings.Contains(string(output), secret) {
					t.Fatalf("raw Pod output leaked a credential")
				}
			}
			if tc.failure == "installer" && !strings.Contains(string(output), "useful context") {
				t.Fatalf("lost useful diagnostics: %s", output)
			}
		})
	}
}

func TestSeedGenerationISOUploadPreservesForeignFiles(t *testing.T) {
	f := newSeedGenerationISOScriptFixture(t)
	writeISOTestFile(t, f.remote+"/rhcos-ibi.iso", "foreign ISO", 0o644)
	writeISOTestFile(t, f.remote+"/"+seedGenerationISOUploadOwnerFile, "different-run", 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := f.command(ctx, "").CombinedOutput(); err == nil {
		t.Fatalf("overwrote an existing run: %s", output)
	}
	data, err := os.ReadFile(f.remote + "/rhcos-ibi.iso")
	if err != nil || string(data) != "foreign ISO" {
		t.Fatalf("foreign ISO changed: %v", err)
	}
	artifact := &seedGenerationISOArtifact{RemoteDirectory: f.remote}
	output, err := exec.CommandContext(ctx, "/bin/bash", "-c", seedGenerationISOUploadCleanupCommand(artifact, "run-123")).CombinedOutput() //nolint:gosec // Test-owned upload path and constant run UID.
	if err != nil || strings.TrimSpace(string(output)) != "unowned" {
		t.Fatalf("foreign cleanup: %v %s", err, output)
	}
	data, err = os.ReadFile(f.remote + "/rhcos-ibi.iso")
	if err != nil || string(data) != "foreign ISO" {
		t.Fatalf("cleanup changed foreign ISO: %v", err)
	}
}

func TestSeedGenerationISOUploadReservationFailurePreservesExistingRun(t *testing.T) {
	f := newSeedGenerationISOScriptFixture(t)
	writeISOTestFile(t, f.remote+"/rhcos-ibi.iso", "previous attempt", 0o644)
	writeISOTestFile(t, f.remote+"/"+seedGenerationISOUploadOwnerFile, "run-123", 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := f.command(ctx, "").CombinedOutput(); err == nil {
		t.Fatalf("reserved an existing directory: %s", output)
	}
	if data, err := os.ReadFile(f.remote + "/rhcos-ibi.iso"); err != nil || string(data) != "previous attempt" {
		t.Fatalf("failed reservation changed the previous attempt's ISO: %s, %v", data, err)
	}
}

func TestSeedGenerationISORepeatedExecutionReusesVerifiedResult(t *testing.T) {
	f := newSeedGenerationISOScriptFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := f.command(ctx, "").CombinedOutput(); err != nil {
		t.Fatalf("first attempt failed: %v: %s", err, output)
	}
	result, err := os.ReadFile(f.termination)
	if err != nil {
		t.Fatal(err)
	}
	// Extraction or building on the second attempt would fail; it must return
	// the verified result without repeating either operation or the upload.
	if output, err := f.command(ctx, "installer").CombinedOutput(); err != nil {
		t.Fatalf("duplicate attempt did not reuse the verified result: %v: %s", err, output)
	}
	if duplicate, err := os.ReadFile(f.termination); err != nil || !bytes.Equal(duplicate, result) {
		t.Fatalf("duplicate result changed: %s, %v", duplicate, err)
	}
	if _, err := os.Stat(f.remote + "/rhcos-ibi.iso"); err != nil {
		t.Fatalf("duplicate attempt removed the published ISO: %v", err)
	}
}

func TestSeedGenerationISOConcurrentAttemptsShareVerifiedResult(t *testing.T) {
	f := newSeedGenerationISOScriptFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, second := f.command(ctx, "blocked"), f.command(ctx, "installer")
	configureISOTestProcessGroup(first)
	configureISOTestProcessGroup(second)
	t.Cleanup(func() {
		killISOTestProcessGroup(first)
		killISOTestProcessGroup(second)
		_ = first.Wait()
		_ = second.Wait()
	})
	var firstOutput, secondOutput bytes.Buffer
	first.Stdout, first.Stderr = &firstOutput, &firstOutput
	second.Stdout, second.Stderr = &secondOutput, &secondOutput
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(f.root + "/ready"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("first upload did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	// Confirm another process cannot take the workspace lock during the upload.
	lock, err := os.OpenFile(f.root+"/workspace/.oran-seedgen-build.lock", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != syscall.EWOULDBLOCK {
		t.Fatalf("workspace was not locked during the active upload: %v", err)
	}
	writeISOTestFile(t, f.root+"/release", "", 0o600)
	if err := first.Wait(); err != nil {
		t.Fatalf("first attempt failed: %v: %s", err, firstOutput.String())
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("duplicate attempt failed: %v: %s", err, secondOutput.String())
	}
	if _, err := os.Stat(f.remote + "/rhcos-ibi.iso"); err != nil {
		t.Fatalf("concurrent attempts removed the published ISO: %v", err)
	}
}

func TestSeedGenerationISOCachedResultRejectsOtherRuns(t *testing.T) {
	for name, result := range map[string]string{
		"different UID":  "other-run\nhttps://example.test/seedgen/run-123/rhcos-ibi.iso\nsha256:" + strings.Repeat("a", 64) + "\n",
		"different URL":  "run-123\nhttps://other.test/image.iso\nsha256:" + strings.Repeat("a", 64) + "\n",
		"invalid digest": "run-123\nhttps://example.test/seedgen/run-123/rhcos-ibi.iso\ninvalid\n",
		"incomplete":     "run-123\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newSeedGenerationISOScriptFixture(t)
			writeISOTestFile(t, f.root+"/workspace/.oran-seedgen-result", result, 0o600)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if output, err := f.command(ctx, "").CombinedOutput(); err == nil {
				t.Fatalf("invalid cached result accepted: %s", output)
			}
		})
	}
}

func TestSeedGenerationISOUploadCleanupAfterHardKill(t *testing.T) {
	f := newSeedGenerationISOScriptFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := f.command(ctx, "killed")
	// Kill the process group to simulate loss of the entire Pod, including scp.
	configureISOTestProcessGroup(command)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killISOTestProcessGroup(command); _ = command.Wait() })
	for {
		if _, err := os.Stat(f.root + "/ready"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("upload did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	killISOTestProcessGroup(command)
	_ = command.Wait()
	artifact := &seedGenerationISOArtifact{RemoteDirectory: f.remote}
	if _, err := os.Stat(f.remote + "/" + seedGenerationISOUploadOwnerFile); err != nil {
		t.Fatal("hard kill did not leave the expected recovery marker", err)
	}
	output, err := exec.CommandContext(ctx, "/bin/bash", "-c", seedGenerationISOUploadCleanupCommand(artifact, "run-123")).CombinedOutput() //nolint:gosec // Test-owned upload path and constant run UID.
	if err != nil {
		t.Fatalf("recovery cleanup failed: %v: %s", err, output)
	}
	if _, err := os.Stat(f.remote); !os.IsNotExist(err) {
		t.Fatalf("hard-killed upload was not removed: %v", err)
	}
}

func configureISOTestProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
func killISOTestProcessGroup(command *exec.Cmd) {
	if command.Process != nil && command.ProcessState == nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
