package job

import (
	"context"
	"testing"

	"athlete-log/internal/storage"
)

func TestRunner_ZipBytes_StoreAndRetrieve(t *testing.T) {
	r := &Runner{}
	payload := []byte("fake zip content")

	r.zipStore.Store("job-1", payload)

	got, ok := r.ZipBytes("job-1")
	if !ok {
		t.Fatal("ZipBytes() ok = false, want true")
	}
	if string(got) != string(payload) {
		t.Fatalf("ZipBytes() = %q, want %q", got, payload)
	}
}

func TestRunner_ZipBytes_MissingJob(t *testing.T) {
	r := &Runner{}
	_, ok := r.ZipBytes("nonexistent")
	if ok {
		t.Fatal("ZipBytes() ok = true for missing job, want false")
	}
}

func TestRunner_Start_StoresResult(t *testing.T) {
	r := &Runner{}
	done := make(chan struct{})

	r.Start("job-2", func(_ context.Context, _ func(storage.JobUpdate)) []byte {
		defer close(done)
		return []byte("result bytes")
	})

	<-done
	got, ok := r.ZipBytes("job-2")
	if !ok {
		t.Fatal("ZipBytes() ok = false after Start, want true")
	}
	if string(got) != "result bytes" {
		t.Fatalf("ZipBytes() = %q, want %q", got, "result bytes")
	}
}

func TestRunner_Start_NilResultNotStored(t *testing.T) {
	r := &Runner{}
	done := make(chan struct{})

	r.Start("job-3", func(_ context.Context, _ func(storage.JobUpdate)) []byte {
		defer close(done)
		return nil
	})

	<-done
	_, ok := r.ZipBytes("job-3")
	if ok {
		t.Fatal("ZipBytes() ok = true for nil result, want false")
	}
}
