package logutils

import (
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// The SDK adapter must map every interface level onto the matching zap level
// and format the message lazily like the SDK's Logger contract expects.
func TestSDKLoggerLevelMapping(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	adapter := NewSDKLogger(zap.New(core))

	adapter.Debugf("debug %s", "x")
	adapter.Infof("info %s", "x")
	adapter.Warnf("warn %s", "x")
	adapter.Errorf("error %s", "x")

	entries := logs.All()
	if len(entries) != 4 {
		t.Fatalf("log entries = %d, want 4", len(entries))
	}
	wantLevels := []zapcore.Level{zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel}
	wantMessages := []string{"debug x", "info x", "warn x", "error x"}
	for i, entry := range entries {
		if entry.Level != wantLevels[i] {
			t.Fatalf("entry %d level = %s, want %s", i, entry.Level, wantLevels[i])
		}
		if entry.Message != wantMessages[i] {
			t.Fatalf("entry %d message = %q, want %q", i, entry.Message, wantMessages[i])
		}
	}
}

// The adapter is shared by concurrent retry/failover paths in the SDK and
// must stay race-free under -race.
func TestSDKLoggerConcurrentUse(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	adapter := NewSDKLogger(zap.New(core))

	const writers = 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			adapter.Infof("entry %d", i)
		}(i)
	}
	wg.Wait()

	if logs.Len() != writers {
		t.Fatalf("log entries = %d, want %d", logs.Len(), writers)
	}
}
