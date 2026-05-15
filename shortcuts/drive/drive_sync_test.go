// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package drive

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larksuite/cli/extension/fileio"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/credential"
	"github.com/larksuite/cli/internal/httpmock"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
	"github.com/spf13/cobra"
)

func newDriveSyncRuntime(t *testing.T, localDir, folderToken string) (*common.RuntimeContext, *cmdutil.Factory) {
	t.Helper()
	f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, localDir, folderToken)
	return runtime, f
}

func newDriveSyncRuntimeWithFactory(t *testing.T, f *cmdutil.Factory, localDir, folderToken string) *common.RuntimeContext {
	t.Helper()
	cmd := &cobra.Command{Use: "drive +sync"}
	cmd.Flags().String("local-dir", "", "")
	cmd.Flags().String("folder-token", "", "")
	cmd.Flags().String("on-conflict", "", "")
	cmd.Flags().String("on-duplicate-remote", "", "")
	cmd.Flags().Bool("quick", false, "")
	if localDir != "" {
		if err := cmd.Flags().Set("local-dir", localDir); err != nil {
			t.Fatalf("set --local-dir: %v", err)
		}
	}
	if folderToken != "" {
		if err := cmd.Flags().Set("folder-token", folderToken); err != nil {
			t.Fatalf("set --folder-token: %v", err)
		}
	}
	runtime := common.TestNewRuntimeContextWithCtx(context.Background(), cmd, driveTestConfig())
	runtime.Factory = f
	return runtime
}

type failSaveProvider struct {
	inner      fileio.Provider
	failSuffix string
	err        error
}

func (p *failSaveProvider) Name() string { return "fail-save" }

func (p *failSaveProvider) ResolveFileIO(ctx context.Context) fileio.FileIO {
	return &failSaveFileIO{inner: p.inner.ResolveFileIO(ctx), failSuffix: p.failSuffix, err: p.err}
}

type failSaveFileIO struct {
	inner      fileio.FileIO
	failSuffix string
	err        error
}

func (f *failSaveFileIO) Open(name string) (fileio.File, error)     { return f.inner.Open(name) }
func (f *failSaveFileIO) Stat(name string) (fileio.FileInfo, error) { return f.inner.Stat(name) }
func (f *failSaveFileIO) ResolvePath(path string) (string, error)   { return f.inner.ResolvePath(path) }

func (f *failSaveFileIO) Save(path string, opts fileio.SaveOptions, body io.Reader) (fileio.SaveResult, error) {
	if strings.HasSuffix(path, f.failSuffix) {
		return nil, f.err
	}
	return f.inner.Save(path, opts, body)
}

type deleteOnCloseProvider struct {
	inner      fileio.Provider
	targetPath string
	deletePath string
}

func (p *deleteOnCloseProvider) Name() string { return "delete-on-close" }

func (p *deleteOnCloseProvider) ResolveFileIO(ctx context.Context) fileio.FileIO {
	return &deleteOnCloseFileIO{inner: p.inner.ResolveFileIO(ctx), targetPath: p.targetPath, deletePath: p.deletePath}
}

type deleteOnCloseFileIO struct {
	inner      fileio.FileIO
	targetPath string
	deletePath string
}

func (f *deleteOnCloseFileIO) Open(name string) (fileio.File, error) {
	file, err := f.inner.Open(name)
	if err != nil {
		return nil, err
	}
	if name != f.targetPath {
		return file, nil
	}
	return &deleteOnCloseFile{File: file, deletePath: f.deletePath}, nil
}

func (f *deleteOnCloseFileIO) Stat(name string) (fileio.FileInfo, error) { return f.inner.Stat(name) }
func (f *deleteOnCloseFileIO) ResolvePath(path string) (string, error) {
	return f.inner.ResolvePath(path)
}
func (f *deleteOnCloseFileIO) Save(path string, opts fileio.SaveOptions, body io.Reader) (fileio.SaveResult, error) {
	return f.inner.Save(path, opts, body)
}

type deleteOnCloseFile struct {
	fileio.File
	deletePath string
}

func (f *deleteOnCloseFile) Close() error {
	err := f.File.Close()
	_ = os.Remove(f.deletePath)
	return err
}

type failAfterSaveProvider struct {
	inner      fileio.Provider
	failSuffix string
	err        error
	afterSave  func(path string)
}

func (p *failAfterSaveProvider) Name() string { return "fail-after-save" }

func (p *failAfterSaveProvider) ResolveFileIO(ctx context.Context) fileio.FileIO {
	return &failAfterSaveFileIO{inner: p.inner.ResolveFileIO(ctx), failSuffix: p.failSuffix, err: p.err, afterSave: p.afterSave}
}

type failAfterSaveFileIO struct {
	inner      fileio.FileIO
	failSuffix string
	err        error
	afterSave  func(path string)
}

func (f *failAfterSaveFileIO) Open(name string) (fileio.File, error)     { return f.inner.Open(name) }
func (f *failAfterSaveFileIO) Stat(name string) (fileio.FileInfo, error) { return f.inner.Stat(name) }
func (f *failAfterSaveFileIO) ResolvePath(path string) (string, error) {
	return f.inner.ResolvePath(path)
}

func (f *failAfterSaveFileIO) Save(path string, opts fileio.SaveOptions, body io.Reader) (fileio.SaveResult, error) {
	res, err := f.inner.Save(path, opts, body)
	if strings.HasSuffix(path, f.failSuffix) {
		if f.afterSave != nil {
			f.afterSave(path)
		}
		return res, f.err
	}
	return res, err
}

type driveSyncReadThenError struct {
	stage int
}

func (r *driveSyncReadThenError) Read(p []byte) (int, error) {
	if r.stage == 0 {
		r.stage++
		copy(p, []byte("local "))
		return 6, nil
	}
	return 0, fmt.Errorf("read failure")
}

// TestDriveSyncRemoteWinsPullsNewRemoteAndPushesNewLocal verifies the basic
// two-way sync flow: new_remote files are pulled, new_local files are pushed,
// and modified files use --on-conflict=remote-wins (the default) to pull the
// remote version.
func TestDriveSyncRemoteWinsPullsNewRemoteAndPushesNewLocal(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-remote-wins", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	// Local layout:
	//   local/b.txt  — only local → push
	//   local/a.txt  — both sides, different content → conflict (remote-wins → pull)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	if err := os.WriteFile("local/b.txt", []byte("local-b"), 0o644); err != nil {
		t.Fatalf("WriteFile b.txt: %v", err)
	}

	// Remote listing: a.txt (modified), d.txt (new_remote)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
					map[string]interface{}{"token": "tok_d", "name": "d.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for hash comparison (exact mode)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Download d.txt (new_remote → pull)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_d/download",
		Status:  200,
		Body:    []byte("remote-d"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Download a.txt again (conflict: remote-wins → pull remote over local)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Upload b.txt (new_local → push)
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"file_token": "tok_b_uploaded",
			},
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "remote-wins",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"action": "downloaded"`) {
		t.Errorf("output missing downloaded action\noutput: %s", out)
	}
	if !strings.Contains(out, `"action": "uploaded"`) {
		t.Errorf("output missing uploaded action\noutput: %s", out)
	}
	if !strings.Contains(out, `"direction": "pull"`) {
		t.Errorf("output missing pull direction\noutput: %s", out)
	}
	if !strings.Contains(out, `"direction": "push"`) {
		t.Errorf("output missing push direction\noutput: %s", out)
	}

	// Verify local file was overwritten with remote content
	data, err := os.ReadFile("local/a.txt")
	if err != nil {
		t.Fatalf("ReadFile a.txt: %v", err)
	}
	if string(data) != "remote-a" {
		t.Errorf("a.txt content = %q, want %q", string(data), "remote-a")
	}

	// Verify d.txt was downloaded
	data, err = os.ReadFile("local/d.txt")
	if err != nil {
		t.Fatalf("ReadFile d.txt: %v", err)
	}
	if string(data) != "remote-d" {
		t.Errorf("d.txt content = %q, want %q", string(data), "remote-d")
	}
}

// TestDriveSyncLocalWinsPushesOverRemote verifies that --on-conflict=local-wins
// pushes the local version over the remote file.
func TestDriveSyncLocalWinsPushesOverRemote(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-local-wins", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for hash comparison (exact mode)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Upload a.txt with overwrite (local-wins → push over remote)
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"file_token": "tok_a",
				"version":    "v2",
			},
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "local-wins",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"action": "overwritten"`) {
		t.Errorf("output missing overwritten action\noutput: %s", out)
	}
	if !strings.Contains(out, `"direction": "push"`) {
		t.Errorf("output missing push direction\noutput: %s", out)
	}
}

// TestDriveSyncKeepBothRenamesLocalAndPullsRemote verifies that
// --on-conflict=keep-both renames the local file with a hash suffix
// and then downloads the remote version to the original path.
func TestDriveSyncKeepBothRenamesLocalAndPullsRemote(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-keep-both", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for hash comparison
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Download a.txt again (keep-both: pull remote to original path after rename)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "keep-both",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"action": "renamed_local"`) {
		t.Errorf("output missing renamed_local action\noutput: %s", out)
	}
	if !strings.Contains(out, `"action": "downloaded"`) {
		t.Errorf("output missing downloaded action\noutput: %s", out)
	}

	// Original path should now have remote content
	data, err := os.ReadFile("local/a.txt")
	if err != nil {
		t.Fatalf("ReadFile a.txt: %v", err)
	}
	if string(data) != "remote-a" {
		t.Errorf("a.txt content = %q, want %q", string(data), "remote-a")
	}

	// There should be a renamed file with __lark_ suffix
	entries, err := os.ReadDir("local")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "__lark_") && strings.HasSuffix(e.Name(), ".txt") {
			found = true
			renamedData, err := os.ReadFile("local/" + e.Name())
			if err != nil {
				t.Fatalf("ReadFile renamed: %v", err)
			}
			if string(renamedData) != "local-a" {
				t.Errorf("renamed file content = %q, want %q", string(renamedData), "local-a")
			}
		}
	}
	if !found {
		t.Errorf("expected a file with __lark_ suffix in local/, got entries: %v", entries)
	}
}

// TestDriveSyncKeepBothRollsBackRenameOnPullFailure verifies that keep-both
// restores the original local path if the remote download fails after the
// local file has been renamed.
func TestDriveSyncKeepBothRollsBackRenameOnPullFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-keep-both-rollback", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for the exact diff phase.
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "keep-both",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected +sync keep-both to fail when the post-rename pull has no stub\nstdout: %s", stdout.String())
	}

	data, readErr := os.ReadFile("local/a.txt")
	if readErr != nil {
		t.Fatalf("ReadFile a.txt after rollback: %v", readErr)
	}
	if string(data) != "local-a" {
		t.Fatalf("a.txt content after rollback = %q, want %q", string(data), "local-a")
	}

	entries, readDirErr := os.ReadDir("local")
	if readDirErr != nil {
		t.Fatalf("ReadDir local: %v", readDirErr)
	}
	if len(entries) != 1 || entries[0].Name() != "a.txt" {
		t.Fatalf("expected rollback to restore only local/a.txt, got entries: %v", entries)
	}
}

// TestDriveSyncAskConflictFailsBeforeWritesWithoutStdin verifies that
// --on-conflict=ask fails before any sync writes start when stdin is not
// available and the diff contains modified entries.
func TestDriveSyncAskConflictFailsBeforeWritesWithoutStdin(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-ask-eof", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	if err := os.WriteFile("local/b.txt", []byte("local-b"), 0o644); err != nil {
		t.Fatalf("WriteFile b.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
					map[string]interface{}{"token": "tok_d", "name": "d.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for the exact diff phase.
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "ask",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected +sync --on-conflict=ask to fail on EOF\nstdout: %s", stdout.String())
	}
	if !strings.Contains(err.Error(), "interactive stdin") {
		t.Fatalf("expected interactive stdin validation error, got: %v", err)
	}

	data, readErr := os.ReadFile("local/a.txt")
	if readErr != nil {
		t.Fatalf("ReadFile a.txt after ask failure: %v", readErr)
	}
	if string(data) != "local-a" {
		t.Fatalf("a.txt content after ask failure = %q, want %q", string(data), "local-a")
	}
	if _, statErr := os.Stat("local/d.txt"); !os.IsNotExist(statErr) {
		t.Fatalf("new_remote download should not start before ask preflight; stat err=%v", statErr)
	}
}

func TestDriveSyncFailsOnDuplicateRemoteFiles(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, driveTestConfig())

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	registerDuplicateRemoteFiles(reg)

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--as", "bot",
	}, f, stdout)
	assertDuplicateRemotePathError(t, err, "dup.txt", duplicateRemoteFileIDFirst, duplicateRemoteFileIDSecond)
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty on duplicate_remote_path, got: %s", stdout.String())
	}
}

// TestDriveSyncUsesResolvedDuplicateTargetForDiff verifies that +sync computes
// the diff against the same duplicate-remote selection used during execution.
func TestDriveSyncUsesResolvedDuplicateTargetForDiff(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-duplicate-resolution", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("same-as-oldest"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_old", "name": "a.txt", "type": "file", "created_time": "100", "modified_time": "100"},
					map[string]interface{}{"token": "tok_new", "name": "a.txt", "type": "file", "created_time": "200", "modified_time": "200"},
				},
				"has_more": false,
			},
		},
	})

	// The chosen --on-duplicate-remote=oldest target is tok_old. The test omits
	// any tok_new download stub so a stale last-seen overwrite bug would fail.
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_old/download",
		Status:  200,
		Body:    []byte("same-as-oldest"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-duplicate-remote", "oldest",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"pushed": 0`) || !strings.Contains(out, `"pulled": 0`) {
		t.Fatalf("expected unchanged duplicate target to produce no sync actions\noutput: %s", out)
	}
	if !strings.Contains(out, `"file_token": "tok_old"`) {
		t.Fatalf("expected diff to reference the oldest duplicate target token\noutput: %s", out)
	}
}

// TestDriveSyncLocalWinsNestedFileUsesParentFolderToken verifies that local-wins
// overwrites on nested files keep parent_node aligned with the file's parent.
func TestDriveSyncLocalWinsNestedFileUsesParentFolderToken(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-local-wins-nested", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local/sub", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/sub/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "fld_sub", "name": "sub", "type": "folder"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=fld_sub",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Diff phase exact hash download.
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	uploadStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"file_token": "tok_a",
				"version":    "v2",
			},
		},
	}
	reg.Register(uploadStub)

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "local-wins",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	body := decodeDriveMultipartBody(t, uploadStub)
	if got := body.Fields["file_token"]; got != "tok_a" {
		t.Fatalf("upload_all file_token = %q, want tok_a", got)
	}
	if got := body.Fields["parent_node"]; got != "fld_sub" {
		t.Fatalf("upload_all parent_node = %q, want fld_sub", got)
	}
}

// TestDriveSyncNewLocalDisappearanceIsReported verifies that files discovered
// during diff but removed before the push phase are surfaced as skipped items
// instead of being silently dropped.
func TestDriveSyncNewLocalDisappearanceIsReported(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-new-local-disappeared", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/ephemeral.txt", []byte("temp"), 0o644); err != nil {
		t.Fatalf("WriteFile ephemeral.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		OnMatch: func(_ *http.Request) {
			if err := os.Remove("local/ephemeral.txt"); err != nil && !os.IsNotExist(err) {
				t.Fatalf("Remove ephemeral.txt in OnMatch: %v", err)
			}
		},
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files":    []interface{}{},
				"has_more": false,
			},
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"skipped": 1`) {
		t.Fatalf("expected skipped=1 when new_local disappears during execution\noutput: %s", out)
	}
	if !strings.Contains(out, `"rel_path": "ephemeral.txt"`) || !strings.Contains(out, `"local file disappeared during sync"`) {
		t.Fatalf("expected vanished new_local file to be reported in items\noutput: %s", out)
	}
}

// TestDriveSyncQuickModeUsesModifiedTime verifies that --quick mode
// classifies files by modified_time instead of SHA-256 hash.
func TestDriveSyncQuickModeUsesModifiedTime(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-quick", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	if err := os.WriteFile("local/b.txt", []byte("local-b"), 0o644); err != nil {
		t.Fatalf("WriteFile b.txt: %v", err)
	}

	// Set a.txt mtime to match remote → unchanged in quick mode
	matchTime := time.Unix(1715594880, 0)
	if err := os.Chtimes("local/a.txt", matchTime, matchTime); err != nil {
		t.Fatalf("Chtimes a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file", "modified_time": "1715594880"},
					map[string]interface{}{"token": "tok_d", "name": "d.txt", "type": "file", "modified_time": "1715595000"},
				},
				"has_more": false,
			},
		},
	})

	// Download d.txt (new_remote → pull)
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_d/download",
		Status:  200,
		Body:    []byte("remote-d"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	// Upload b.txt (new_local → push)
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"file_token": "tok_b_uploaded",
			},
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--quick",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"detection": "quick"`) {
		t.Errorf("output missing detection=quick\noutput: %s", out)
	}
	// a.txt should be unchanged (mtime matches), not downloaded or uploaded
	// It should appear in diff.unchanged but NOT in items[] with a pull/push action
	for _, item := range []string{`"downloaded"`, `"uploaded"`, `"overwritten"`} {
		if strings.Count(out, item) > 0 {
			// Check that a.txt is not the one being downloaded/uploaded
			// by verifying it only appears in the diff section, not items
			break
		}
	}
	// More precise: a.txt should not appear in items[] at all since it's unchanged
	itemsSection := out[strings.Index(out, `"items"`):]
	if strings.Contains(itemsSection, `"rel_path": "a.txt"`) {
		t.Errorf("a.txt should not appear in items[] (mtime matches remote, should be unchanged)\noutput: %s", out)
	}
}

// TestDriveSyncQuickModeMTimeMismatchStillTriggersWrites verifies the best-effort
// nature of --quick: a timestamp mismatch alone is enough to drive a real sync
// action even when the file bytes are already identical.
func TestDriveSyncQuickModeMTimeMismatchStillTriggersWrites(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-quick-mismatch", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("same-content"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	localTime := time.Unix(1715594880, 0)
	if err := os.Chtimes("local/a.txt", localTime, localTime); err != nil {
		t.Fatalf("Chtimes a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file", "modified_time": "1715594999"},
				},
				"has_more": false,
			},
		},
	})

	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("same-content"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--quick",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"detection": "quick"`) {
		t.Fatalf("expected detection=quick\noutput: %s", out)
	}
	if !strings.Contains(out, `"modified":`) || !strings.Contains(out, `"action": "downloaded"`) {
		t.Fatalf("expected quick mtime mismatch to trigger a real pull action\noutput: %s", out)
	}
}

// TestDriveSyncNoChangesReportsEmptyItems verifies that when local and remote
// are identical, +sync reports zero pulled/pushed items.
func TestDriveSyncNoChangesReportsEmptyItems(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-no-changes", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("same"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	// Download a.txt for hash comparison → same content → unchanged
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("same"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"pulled": 0`) {
		t.Errorf("expected pulled=0\noutput: %s", out)
	}
	if !strings.Contains(out, `"pushed": 0`) {
		t.Errorf("expected pushed=0\noutput: %s", out)
	}
	if !strings.Contains(out, `"failed": 0`) {
		t.Errorf("expected failed=0\noutput: %s", out)
	}
}

func TestDriveSyncValidateRejectsInvalidInputs(t *testing.T) {
	t.Run("missing local-dir", func(t *testing.T) {
		runtime, _ := newDriveSyncRuntime(t, "", "folder_root")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "--local-dir is required") {
			t.Fatalf("Validate() error = %v, want missing --local-dir", err)
		}
	})

	t.Run("missing folder-token", func(t *testing.T) {
		runtime, _ := newDriveSyncRuntime(t, "local", "")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "--folder-token is required") {
			t.Fatalf("Validate() error = %v, want missing --folder-token", err)
		}
	})

	t.Run("malformed folder-token", func(t *testing.T) {
		tmpDir := t.TempDir()
		withDriveWorkingDir(t, tmpDir)
		if err := os.MkdirAll("local", 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		runtime, _ := newDriveSyncRuntime(t, "local", "tok\nwithnewline")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "--folder-token") {
			t.Fatalf("Validate() error = %v, want malformed folder-token error", err)
		}
	})

	t.Run("absolute local-dir", func(t *testing.T) {
		runtime, _ := newDriveSyncRuntime(t, "/etc", "folder_root")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "--local-dir") {
			t.Fatalf("Validate() error = %v, want invalid local-dir error", err)
		}
	})

	t.Run("missing local-dir path", func(t *testing.T) {
		tmpDir := t.TempDir()
		withDriveWorkingDir(t, tmpDir)
		runtime, _ := newDriveSyncRuntime(t, "missing", "folder_root")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("Validate() error = %v, want missing-path error", err)
		}
	})

	t.Run("local-dir is file", func(t *testing.T) {
		tmpDir := t.TempDir()
		withDriveWorkingDir(t, tmpDir)
		if err := os.WriteFile("not-a-dir.txt", []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		runtime, _ := newDriveSyncRuntime(t, "not-a-dir.txt", "folder_root")
		err := DriveSync.Validate(context.Background(), runtime)
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("Validate() error = %v, want not-a-directory error", err)
		}
	})
}

func TestDriveSyncDryRunUsesFolderToken(t *testing.T) {
	runtime, _ := newDriveSyncRuntime(t, "local", "folder_root")
	dry := DriveSync.DryRun(context.Background(), runtime)
	if dry == nil {
		t.Fatal("DryRun returned nil")
	}

	data, err := json.Marshal(dry)
	if err != nil {
		t.Fatalf("marshal dry run: %v", err)
	}
	if !strings.Contains(string(data), `"folder_token":"folder_root"`) {
		t.Fatalf("dry run missing folder_token, got: %s", string(data))
	}
}

func TestDriveSyncExecuteRejectsUnsafeLocalDir(t *testing.T) {
	runtime, _ := newDriveSyncRuntime(t, "/etc", "folder_root")
	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "--local-dir") {
		t.Fatalf("Execute() error = %v, want unsafe local-dir validation error", err)
	}
}

func TestDriveSyncAskConflictParsesChoices(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "blank line defaults remote wins", input: "\n", want: driveSyncOnConflictRemoteWins},
		{name: "local short form", input: "L\n", want: driveSyncOnConflictLocalWins},
		{name: "keep both long form", input: "keep-both\n", want: driveSyncOnConflictKeepBoth},
		{name: "skip returns empty resolution", input: "skip\n", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
			f.IOStreams.In = strings.NewReader(tt.input)

			runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
			runtime.Factory = f

			got, err := driveSyncAskConflict("a.txt", runtime)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("driveSyncAskConflict() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("driveSyncAskConflict() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("driveSyncAskConflict() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDriveSyncAskConflictRejectsMissingStdin(t *testing.T) {
	f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
	runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
	runtime.Factory = f

	_, err := driveSyncAskConflict("a.txt", runtime)
	if err == nil || !strings.Contains(err.Error(), "stdin is not available") {
		t.Fatalf("driveSyncAskConflict() error = %v, want stdin availability error", err)
	}
}

func TestDriveSyncAskConflictHandlesEOFAndReadErrors(t *testing.T) {
	t.Run("blank EOF without answer fails", func(t *testing.T) {
		f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
		f.IOStreams.In = strings.NewReader("")

		runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
		runtime.Factory = f

		_, err := driveSyncAskConflict("a.txt", runtime)
		if err == nil || !strings.Contains(err.Error(), "stdin reached EOF") {
			t.Fatalf("driveSyncAskConflict() error = %v, want EOF failure", err)
		}
	})

	t.Run("partial token before EOF is still accepted", func(t *testing.T) {
		f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
		f.IOStreams.In = strings.NewReader("local")

		runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
		runtime.Factory = f

		got, err := driveSyncAskConflict("a.txt", runtime)
		if err != nil {
			t.Fatalf("driveSyncAskConflict() unexpected error: %v", err)
		}
		if got != driveSyncOnConflictLocalWins {
			t.Fatalf("driveSyncAskConflict() = %q, want %q", got, driveSyncOnConflictLocalWins)
		}
	})

	t.Run("unknown answer returns validation error", func(t *testing.T) {
		f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
		f.IOStreams.In = strings.NewReader("what\n")

		runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
		runtime.Factory = f

		_, err := driveSyncAskConflict("a.txt", runtime)
		if err == nil || !strings.Contains(err.Error(), "invalid conflict choice") {
			t.Fatalf("driveSyncAskConflict() error = %v, want invalid-choice failure", err)
		}
	})

	t.Run("non EOF read failure returns wrapped error", func(t *testing.T) {
		f, _, _, _ := cmdutil.TestFactory(t, driveTestConfig())
		f.IOStreams.In = bufio.NewReader(&driveSyncReadThenError{})

		runtime := common.TestNewRuntimeContext(&cobra.Command{Use: "drive"}, driveTestConfig())
		runtime.Factory = f

		_, err := driveSyncAskConflict("a.txt", runtime)
		if err == nil || !strings.Contains(err.Error(), "cannot read conflict choice") {
			t.Fatalf("driveSyncAskConflict() error = %v, want wrapped read failure", err)
		}
	})
}

func TestDriveSyncRollbackRenamedLocalRestoresRenamedFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldAbsPath := tmpDir + "/a.txt"
	newAbsPath := tmpDir + "/a__lark.txt"

	if err := os.WriteFile(oldAbsPath, []byte("partial remote"), 0o644); err != nil {
		t.Fatalf("WriteFile oldAbsPath: %v", err)
	}
	if err := os.WriteFile(newAbsPath, []byte("original local"), 0o644); err != nil {
		t.Fatalf("WriteFile newAbsPath: %v", err)
	}

	if err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath); err != nil {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v", err)
	}

	data, err := os.ReadFile(oldAbsPath)
	if err != nil {
		t.Fatalf("ReadFile restored oldAbsPath: %v", err)
	}
	if got := string(data); got != "original local" {
		t.Fatalf("restored content = %q, want %q", got, "original local")
	}
	if _, err := os.Stat(newAbsPath); !os.IsNotExist(err) {
		t.Fatalf("expected renamed path to be removed after rollback, stat err = %v", err)
	}
}

func TestDriveSyncRollbackRenamedLocalWithoutPartialRestore(t *testing.T) {
	tmpDir := t.TempDir()
	oldAbsPath := tmpDir + "/a.txt"
	newAbsPath := tmpDir + "/a__lark.txt"

	if err := os.WriteFile(newAbsPath, []byte("original local"), 0o644); err != nil {
		t.Fatalf("WriteFile newAbsPath: %v", err)
	}

	if err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath); err != nil {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v", err)
	}

	data, err := os.ReadFile(oldAbsPath)
	if err != nil {
		t.Fatalf("ReadFile restored oldAbsPath: %v", err)
	}
	if got := string(data); got != "original local" {
		t.Fatalf("restored content = %q, want %q", got, "original local")
	}
}

func TestDriveSyncRollbackRenamedLocalRejectsDirectoryAtOriginalPath(t *testing.T) {
	tmpDir := t.TempDir()
	oldAbsPath := tmpDir + "/a.txt"
	newAbsPath := tmpDir + "/a__lark.txt"

	if err := os.Mkdir(oldAbsPath, 0o755); err != nil {
		t.Fatalf("Mkdir oldAbsPath: %v", err)
	}
	if err := os.WriteFile(newAbsPath, []byte("original local"), 0o644); err != nil {
		t.Fatalf("WriteFile newAbsPath: %v", err)
	}

	err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath)
	if err == nil || !strings.Contains(err.Error(), "became a directory") {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v, want directory error", err)
	}
}

func TestDriveSyncRollbackRenamedLocalSurfacesRenameFailure(t *testing.T) {
	tmpDir := t.TempDir()
	oldAbsPath := tmpDir + "/a.txt"
	newAbsPath := tmpDir + "/missing.txt"

	err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath)
	if err == nil || !strings.Contains(err.Error(), "restore renamed local file") {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v, want rename failure", err)
	}
}

func TestDriveSyncRollbackRenamedLocalSurfacesRemoveFailure(t *testing.T) {
	tmpDir := t.TempDir()
	oldAbsPath := filepath.Join(tmpDir, "a.txt")
	newAbsPath := filepath.Join(tmpDir, "a__lark.txt")

	if err := os.WriteFile(oldAbsPath, []byte("partial remote"), 0o644); err != nil {
		t.Fatalf("WriteFile oldAbsPath: %v", err)
	}
	if err := os.WriteFile(newAbsPath, []byte("original local"), 0o644); err != nil {
		t.Fatalf("WriteFile newAbsPath: %v", err)
	}
	if err := os.Chmod(tmpDir, 0o555); err != nil {
		t.Fatalf("Chmod read-only dir: %v", err)
	}
	defer func() {
		_ = os.Chmod(tmpDir, 0o755)
	}()

	err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath)
	if err == nil || !strings.Contains(err.Error(), "remove partial restored path") {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v, want remove failure", err)
	}
}

func TestDriveSyncRollbackRenamedLocalSurfacesStatFailure(t *testing.T) {
	tmpDir := t.TempDir()
	blockedDir := filepath.Join(tmpDir, "blocked")
	oldAbsPath := filepath.Join(blockedDir, "a.txt")
	newAbsPath := filepath.Join(blockedDir, "a__lark.txt")

	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll blockedDir: %v", err)
	}
	if err := os.WriteFile(newAbsPath, []byte("original local"), 0o644); err != nil {
		t.Fatalf("WriteFile newAbsPath: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o000); err != nil {
		t.Fatalf("Chmod blockedDir: %v", err)
	}
	defer func() {
		_ = os.Chmod(blockedDir, 0o755)
	}()

	err := driveSyncRollbackRenamedLocal(oldAbsPath, newAbsPath)
	if err == nil || !strings.Contains(err.Error(), "stat original path") {
		t.Fatalf("driveSyncRollbackRenamedLocal() error = %v, want stat failure", err)
	}
}

func TestDriveSyncAskConflictEOFDuringExecuteReportsFailedItem(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-ask-exec-eof", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.IOStreams.In = strings.NewReader("")

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "ask",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected EOF failure during ask execution\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || !strings.Contains(items[0].Error, "stdin reached EOF") {
		t.Fatalf("expected failed ask item, got detail: %#v", exitErr.Detail.Detail)
	}
	data, readErr := os.ReadFile("local/a.txt")
	if readErr != nil {
		t.Fatalf("ReadFile a.txt: %v", readErr)
	}
	if string(data) != "local-a" {
		t.Fatalf("a.txt content = %q, want local-a", string(data))
	}
}

func TestDriveSyncAskConflictEOFDuringPlanningPreventsAnyWrites(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-ask-plan-eof", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.IOStreams.In = strings.NewReader("")

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	if err := os.WriteFile("local/b.txt", []byte("local-b"), 0o644); err != nil {
		t.Fatalf("WriteFile b.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
					map[string]interface{}{"token": "tok_d", "name": "d.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "ask",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected EOF failure during ask planning\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	if exitErr.Detail.Type != "partial_failure" || !strings.Contains(exitErr.Error(), "stdin reached EOF") {
		t.Fatalf("expected planning failure detail mentioning EOF, got: %#v", exitErr.Detail)
	}
	if data, readErr := os.ReadFile("local/a.txt"); readErr != nil || string(data) != "local-a" {
		t.Fatalf("a.txt should remain untouched, readErr=%v content=%q", readErr, string(data))
	}
	if data, readErr := os.ReadFile("local/b.txt"); readErr != nil || string(data) != "local-b" {
		t.Fatalf("b.txt should remain untouched, readErr=%v content=%q", readErr, string(data))
	}
	if _, statErr := os.Stat("local/d.txt"); !os.IsNotExist(statErr) {
		t.Fatalf("new_remote file must not be downloaded before ask decisions, stat err=%v", statErr)
	}
}

func TestDriveSyncDryRunQuickAcceptsMetadataOnlyScope(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, driveTestConfig())
	f.Credential = credential.NewCredentialProvider(nil, nil, &driveStatusScopedTokenResolver{scopes: "drive:drive.metadata:readonly"}, nil)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--quick",
		"--dry-run",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("expected quick dry-run to succeed without write scopes, got: %v\nstdout: %s", err, stdout.String())
	}
	if strings.Contains(strings.ToLower(stdout.String()), "missing_scope") {
		t.Fatalf("dry-run should not surface missing_scope, got: %s", stdout.String())
	}
}

func TestDriveSyncExactRemoteWinsAcceptsDownloadOnlyScope(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-download-scope-only", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.Credential = credential.NewCredentialProvider(nil, nil, &driveStatusScopedTokenResolver{scopes: "drive:drive.metadata:readonly drive:file:download"}, nil)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "remote-wins",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("expected exact remote-wins to succeed with download-only scope, got: %v\nstdout: %s", err, stdout.String())
	}
	if strings.Contains(strings.ToLower(stdout.String()), "missing_scope") {
		t.Fatalf("should not surface missing_scope, got: %s", stdout.String())
	}
}

func TestDriveSyncAskConflictSkipReportsSkippedItem(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-ask-skip", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.IOStreams.In = strings.NewReader("skip\n")

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "ask",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"action": "skipped"`) || !strings.Contains(out, "user skipped") {
		t.Fatalf("expected skipped conflict item, got: %s", out)
	}
	if !strings.Contains(out, `"skipped": 1`) {
		t.Fatalf("expected skipped summary count, got: %s", out)
	}
}

func TestDriveSyncReportsNewRemoteDownloadFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-new-remote-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.FileIOProvider = &failSaveProvider{inner: f.FileIOProvider, failSuffix: filepath.Join("local", "d.txt"), err: fmt.Errorf("save failed")}

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_d", "name": "d.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_d/download",
		Status:  200,
		Body:    []byte("remote-d"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "remote-wins",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected download failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "pull" || !strings.Contains(items[0].Error, "save failed") {
		t.Fatalf("expected failed pull item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncReportsNewLocalEnsureFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-new-local-ensure-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll(filepath.Join("local", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join("local", "sub", "a.txt"), []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{"files": []interface{}{}, "has_more": false},
		},
	})
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/create_folder",
		Body: map[string]interface{}{
			"code": 9999,
			"msg":  "create parent failed",
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected ensure failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "push" || !strings.Contains(items[0].Error, "create parent failed") {
		t.Fatalf("expected failed push item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncReportsNewLocalUploadFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-new-local-upload-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/b.txt", []byte("local-b"), 0o644); err != nil {
		t.Fatalf("WriteFile b.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{"files": []interface{}{}, "has_more": false},
		},
	})
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 9999,
			"msg":  "upload failed",
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected upload failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "push" || !strings.Contains(items[0].Error, "upload failed") {
		t.Fatalf("expected failed upload item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncLocalWinsReportsUploadFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-local-wins-upload-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/upload_all",
		Body: map[string]interface{}{
			"code": 9999,
			"msg":  "overwrite failed",
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "local-wins",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected local-wins upload failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "push" || !strings.Contains(items[0].Error, "overwrite failed") {
		t.Fatalf("expected failed overwrite item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncKeepBothReportsRenameFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-keep-both-rename-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	suffixedRel, err := relPathWithUniqueFileTokenSuffix("a.txt", "tok_a", map[string]struct{}{"a.txt": {}})
	if err != nil {
		t.Fatalf("relPathWithUniqueFileTokenSuffix: %v", err)
	}
	if err := os.Mkdir(filepath.Join("local", suffixedRel), 0o755); err != nil {
		t.Fatalf("Mkdir suffixed path: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err = mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "keep-both",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected keep-both rename failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "conflict" || !strings.Contains(items[0].Error, "rename local") {
		t.Fatalf("expected failed rename item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncExecuteReturnsRemoteListError(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runtime, _ := newDriveSyncRuntime(t, "local", "folder_root")

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "API call failed") {
		t.Fatalf("Execute() error = %v, want remote list error", err)
	}
}

func TestDriveSyncExecuteReturnsLocalWalkError(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runtime, _ := newDriveSyncRuntime(t, "local", "folder_root")
	if err := os.RemoveAll("local"); err != nil {
		t.Fatalf("RemoveAll local: %v", err)
	}

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "walk") {
		t.Fatalf("Execute() error = %v, want local walk error", err)
	}
}

func TestDriveSyncExecuteWrapsInvalidDuplicateStrategyForPullViews(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	if err := runtime.Cmd.Flags().Set("on-duplicate-remote", "invalid-strategy"); err != nil {
		t.Fatalf("set --on-duplicate-remote: %v", err)
	}
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
					map[string]interface{}{"token": "tok_b", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "unsupported duplicate remote strategy") {
		t.Fatalf("Execute() error = %v, want pull views strategy error", err)
	}
}

func TestDriveSyncExecuteWrapsUnsupportedPushDuplicateStrategy(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	if err := runtime.Cmd.Flags().Set("on-duplicate-remote", driveDuplicateRemoteRename); err != nil {
		t.Fatalf("set --on-duplicate-remote: %v", err)
	}
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
					map[string]interface{}{"token": "tok_b", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "unsupported duplicate remote strategy") {
		t.Fatalf("Execute() error = %v, want push views strategy error", err)
	}
}

func TestDriveSyncExecuteSurfacesHashLocalError(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o000); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	defer func() { _ = os.Chmod("local/a.txt", 0o644) }()

	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "cannot read file") {
		t.Fatalf("Execute() error = %v, want hashLocal error", err)
	}
}

func TestDriveSyncExecuteSurfacesHashRemoteError(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("Execute() error = %v, want hashRemote error", err)
	}
}

func TestDriveSyncExecuteReturnsPushWalkErrorAfterDiff(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
		OnMatch: func(req *http.Request) {
			_ = os.RemoveAll("local")
		},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err == nil || !strings.Contains(err.Error(), "walk") {
		t.Fatalf("Execute() error = %v, want push walk error", err)
	}
}

func TestDriveSyncExecuteUnknownConflictStrategySkipsModifiedFile(t *testing.T) {
	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}
	f, _, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	runtime := newDriveSyncRuntimeWithFactory(t, f, "local", "folder_root")
	if err := runtime.Cmd.Flags().Set("on-conflict", "mystery-mode"); err != nil {
		t.Fatalf("set --on-conflict: %v", err)
	}
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := DriveSync.Execute(context.Background(), runtime)
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
}

func TestDriveSyncModifiedFileDisappearingBeforeExecuteIsSkipped(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-modified-disappears", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.FileIOProvider = &deleteOnCloseProvider{
		inner:      f.FileIOProvider,
		targetPath: filepath.Join("local", "a.txt"),
		deletePath: filepath.Join("local", "a.txt"),
	}

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_a/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "remote-wins",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstdout: %s", err, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"direction": "conflict"`) || !strings.Contains(out, "local file disappeared during sync") {
		t.Fatalf("expected modified file disappearance to be reported, got: %s", out)
	}
	if !strings.Contains(out, `"skipped": 1`) {
		t.Fatalf("expected skipped summary count, got: %s", out)
	}
}

func TestDriveSyncRemoteWinsReportsModifiedPullFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-remote-wins-pull-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)
	f.FileIOProvider = &failSaveProvider{inner: f.FileIOProvider, failSuffix: filepath.Join("local", "a.txt"), err: fmt.Errorf("save failed")}

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:   "GET",
		URL:      "/open-apis/drive/v1/files/tok_a/download",
		Status:   200,
		Body:     []byte("remote-a"),
		Headers:  http.Header{"Content-Type": []string{"application/octet-stream"}},
		Reusable: true,
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "remote-wins",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected modified pull failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || items[0].Direction != "pull" || !strings.Contains(items[0].Error, "save failed") {
		t.Fatalf("expected failed modified pull item, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncKeepBothReportsRollbackFailureAfterPullError(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-keep-both-rollback-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	f.FileIOProvider = &failAfterSaveProvider{
		inner:      f.FileIOProvider,
		failSuffix: filepath.Join("local", "a.txt"),
		err:        fmt.Errorf("save failed"),
		afterSave: func(path string) {
			_ = os.Chmod(filepath.Dir(path), 0o555)
		},
	}
	defer func() {
		_ = os.Chmod(filepath.Join(tmpDir, "local"), 0o755)
	}()

	if err := os.MkdirAll("local", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("local/a.txt", []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_a", "name": "a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:   "GET",
		URL:      "/open-apis/drive/v1/files/tok_a/download",
		Status:   200,
		Body:     []byte("remote-a"),
		Headers:  http.Header{"Content-Type": []string{"application/octet-stream"}},
		Reusable: true,
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "keep-both",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected keep-both rollback failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || !strings.Contains(items[0].Error, "rollback failed") {
		t.Fatalf("expected rollback failure in item error, got detail: %#v", exitErr.Detail.Detail)
	}
}

func TestDriveSyncStatusRemoteFilesUsesStableTokens(t *testing.T) {
	remoteFiles := driveSyncStatusRemoteFiles(map[string]drivePullTarget{
		"item-token.txt": {
			DownloadToken: "download_token_should_not_win",
			ItemFileToken: "item_file_token",
			ModifiedTime:  "111",
		},
		"download-token.txt": {
			DownloadToken: "download_only_token",
			ModifiedTime:  "222",
		},
	})

	if got := remoteFiles["item-token.txt"].FileToken; got != "item_file_token" {
		t.Fatalf("item-token.txt file_token = %q, want item_file_token", got)
	}
	if got := remoteFiles["download-token.txt"].FileToken; got != "download_only_token" {
		t.Fatalf("download-token.txt file_token = %q, want download_only_token", got)
	}
	if got := remoteFiles["download-token.txt"].ModifiedTime; got != "222" {
		t.Fatalf("download-token.txt modified_time = %q, want 222", got)
	}
}

func TestDriveSyncLocalWinsNestedFileReportsParentEnsureFailure(t *testing.T) {
	syncTestConfig := &core.CliConfig{
		AppID: "drive-sync-local-wins-parent-fail", AppSecret: "test-secret", Brand: core.BrandFeishu,
	}
	f, stdout, _, reg := cmdutil.TestFactory(t, syncTestConfig)

	tmpDir := t.TempDir()
	withDriveWorkingDir(t, tmpDir)
	if err := os.MkdirAll(filepath.Join("local", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join("local", "sub", "a.txt"), []byte("local-a"), 0o644); err != nil {
		t.Fatalf("WriteFile a.txt: %v", err)
	}

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "folder_token=folder_root",
		Body: map[string]interface{}{
			"code": 0, "msg": "ok",
			"data": map[string]interface{}{
				"files": []interface{}{
					map[string]interface{}{"token": "tok_nested", "name": "sub/a.txt", "type": "file"},
				},
				"has_more": false,
			},
		},
	})
	reg.Register(&httpmock.Stub{
		Method:  "GET",
		URL:     "/open-apis/drive/v1/files/tok_nested/download",
		Status:  200,
		Body:    []byte("remote-a"),
		Headers: http.Header{"Content-Type": []string{"application/octet-stream"}},
	})
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/drive/v1/files/create_folder",
		Body: map[string]interface{}{
			"code": 9999,
			"msg":  "create parent failed",
		},
	})

	err := mountAndRunDrive(t, DriveSync, []string{
		"+sync",
		"--local-dir", "local",
		"--folder-token", "folder_root",
		"--on-conflict", "local-wins",
		"--as", "bot",
	}, f, stdout)
	if err == nil {
		t.Fatalf("expected parent ensure failure\nstdout: %s", stdout.String())
	}
	var exitErr *output.ExitError
	if !errors.As(err, &exitErr) || exitErr.Detail == nil {
		t.Fatalf("expected structured ExitError, got: %v", err)
	}
	detailMap, _ := exitErr.Detail.Detail.(map[string]interface{})
	items, _ := detailMap["items"].([]driveSyncItem)
	if len(items) == 0 || !strings.Contains(items[0].Error, "create parent failed") {
		t.Fatalf("expected failed item with create_folder error, got detail: %#v", exitErr.Detail.Detail)
	}
}
