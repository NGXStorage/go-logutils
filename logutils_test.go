package logutils

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestParseLevel(t *testing.T) {
	for input, want := range map[string]zapcore.Level{
		"debug": zapcore.DebugLevel, " INFO ": zapcore.InfoLevel,
		"warn": zapcore.WarnLevel, "error": zapcore.ErrorLevel,
		"dpanic": zapcore.DPanicLevel, "panic": zapcore.PanicLevel,
		"fatal": zapcore.FatalLevel,
	} {
		got, ok := ParseLevel(input)
		if !ok || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v", input, got, ok)
		}
	}
	if _, ok := ParseLevel("1"); ok {
		t.Fatal("numeric level accepted")
	}
}

func TestScopedPrefix(t *testing.T) {
	if got := ScopedPrefix("nfs", ""); got != "nfs" {
		t.Fatalf("got %q", got)
	}
	if got := ScopedPrefix("NFS", "201."); got != "nfs-201" {
		t.Fatalf("got %q", got)
	}
	if got := ScopedPrefix("NFS", `"201"`); got != "nfs-201" {
		t.Fatalf("quoted prefix = %q", got)
	}
	if _, err := New(Config{FilePrefix: ScopedPrefix("nfs", "../../escape")}); err == nil {
		t.Fatal("unsafe prefix accepted")
	}
}

func TestNewFromEnvironmentUsesDriverNamePrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(LogDirectoryEnv, dir)
	i, err := NewFromEnvironment("info", "worker-a", "nfs", "201")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	if base := filepath.Base(i.active); !strings.HasPrefix(base, "nfs-201-worker-a-") {
		t.Fatalf("active filename = %q", base)
	}
}

func TestNewConsoleOnlyDoesNotCreateTempFile(t *testing.T) {
	i, err := New(Config{FilePrefix: "nfs", ConsoleLevel: "info"})
	if err != nil {
		t.Fatal(err)
	}
	if i.file != nil {
		t.Fatal("console-only logger opened a file")
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewCreatesProtocolLogWithMode(t *testing.T) {
	dir := t.TempDir()
	i, err := New(Config{Directory: dir, FilePrefix: "fc", NodeID: "node-1", ConsoleLevel: "debug", Interval: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".log" {
		t.Fatalf("unexpected files: %+v", entries)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestPrefixScopedRetention(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "fc-node-old.log.gz")
	other := filepath.Join(dir, "iscsi-node-old.log.gz")
	if err := os.WriteFile(old, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(old, oldTime(), oldTime())
	_ = os.Chtimes(other, oldTime(), oldTime())
	if err := cleanOld(dir, "fc", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old fc archive remains: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other protocol archive changed: %v", err)
	}
}

func TestRotateCompressesOldAndPreservesFreshAndActive(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "nfs-node-old.log")
	fresh := filepath.Join(dir, "nfs-node-fresh.log")
	active := filepath.Join(dir, "nfs-node-active.log")
	for _, path := range []string{old, fresh, active} {
		if err := os.WriteFile(path, []byte(path), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(old, oldTime(), oldTime())
	_ = os.Chtimes(active, oldTime(), oldTime())
	if err := rotate(dir, "nfs", active); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old + ".gz"); err != nil {
		t.Fatalf("old log not archived: %v", err)
	}
	archive, err := os.Open(old + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		_ = archive.Close()
		t.Fatal(err)
	}
	contents, err := io.ReadAll(gz)
	_ = gz.Close()
	_ = archive.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != old {
		t.Fatalf("archive contents = %q", contents)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old log remains: %v", err)
	}
	for _, path := range []string{fresh, active} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s changed: %v", path, err)
		}
	}
}

func TestRetentionKeepsFreshArchiveAndPlainLog(t *testing.T) {
	dir := t.TempDir()
	freshArchive := filepath.Join(dir, "fc-node-fresh.log.gz")
	oldPlain := filepath.Join(dir, "fc-node-old.log")
	for _, path := range []string{freshArchive, oldPlain} {
		if err := os.WriteFile(path, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Chtimes(oldPlain, oldTime(), oldTime())
	if err := cleanOld(dir, "fc", 1); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{freshArchive, oldPlain} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retention changed %s: %v", path, err)
		}
	}
}

func TestRotationIsScopedByDriverNamePrefix(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "nfs-201-node-old.log")
	other := filepath.Join(dir, "nfs-205-node-old.log")
	for _, path := range []string{ours, other} {
		if err := os.WriteFile(path, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(path, oldTime(), oldTime())
	}
	if err := rotate(dir, "nfs-201", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ours + ".gz"); err != nil {
		t.Fatalf("owned log not archived: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other instance log changed: %v", err)
	}
}

func TestMaintenanceStopsAndCloseIsConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	i, err := New(Config{Directory: dir, FilePrefix: "iscsi-201", NodeID: "node-1", Interval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- i.Close() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCloseWaitsForInitialMaintenancePass(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "nfs-201-worker-old.log")
	if err := os.WriteFile(old, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(old, oldTime(), oldTime())
	i, err := New(Config{
		Directory: dir, FilePrefix: "nfs-201", NodeID: "worker-a",
		Interval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old + ".gz"); err != nil {
		t.Fatalf("Close returned before maintenance completed: %v", err)
	}
}

func TestMissingDirectoryAndInvalidNames(t *testing.T) {
	if err := rotate(filepath.Join(t.TempDir(), "missing"), "nfs", ""); !os.IsNotExist(err) {
		t.Fatalf("rotate missing dir = %v", err)
	}
	for _, config := range []Config{{FilePrefix: ""}, {FilePrefix: "../nfs"}, {FilePrefix: "nfs", NodeID: "../../node"}} {
		if _, err := New(config); err == nil {
			t.Fatalf("invalid config accepted: %+v", config)
		}
	}
}

func TestNewWritesJSONLog(t *testing.T) {
	dir := t.TempDir()
	i, err := New(Config{Directory: dir, FilePrefix: "fc-201", NodeID: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	i.Logger.Info("hello")
	if err := i.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"msg":"hello"`) {
		t.Fatalf("unexpected log: %s", raw)
	}
}

func ExampleScopedPrefix() {
	fmt.Println(ScopedPrefix("nfs", "201"))
	// Output: nfs-201
}

func oldTime() (t time.Time) { return time.Now().Add(-48 * time.Hour) }
