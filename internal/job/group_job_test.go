package job

import (
	"errors"
	"testing"
	"time"
)

func TestGroupJob(t *testing.T) {
	t.Run("NewGroupJob", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		if gj == nil {
			t.Fatal("expected non-nil GroupJob")
		}
	})

	t.Run("NewGroupVoidJob", func(t *testing.T) {
		gj := NewGroupVoidJob[int](1)
		if gj == nil {
			t.Fatal("expected non-nil GroupJob")
		}
	})

	t.Run("NewJob", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		job := gj.NewJob(42)
		if job.Data != 42 {
			t.Errorf("expected 42, got %d", job.Data)
		}
	})

	t.Run("FanInResult", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		gj.FanInResult(1)

		go func() {
			time.Sleep(100 * time.Millisecond)
			gj.ResultChannel.Data <- 42
		}()

		result := <-gj.Results()
		if result.Data != 42 {
			t.Errorf("expected 42, got %d", result.Data)
		}
	})

	t.Run("FanInVoidResult", func(t *testing.T) {
		gj := NewGroupVoidJob[int](1)
		gj.FanInVoidResult(1)

		go func() {
			time.Sleep(100 * time.Millisecond)
			gj.ResultChannel.Err <- errors.New("test error")
		}()

		result := <-gj.Results()
		if result.Err == nil || result.Err.Error() != "test error" {
			t.Errorf("expected test error, got %v", result.Err)
		}
	})

	t.Run("Drain", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		gj.FanInResult(1)

		gj.ResultChannel.Data <- 42
		gj.Drain()

		select {
		case <-gj.Results():
		case <-time.After(5 * time.Second):
			t.Fatal("Result channel not drained")
		}
	})

	t.Run("Errors", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		gj.FanInResult(1)

		go func() {
			time.Sleep(100 * time.Millisecond)
			gj.ResultChannel.Err <- errors.New("test error")
		}()

		err := <-gj.Errors()
		if err == nil || err.Error() != "test error" {
			t.Errorf("expected test error, got %v", err)
		}
	})
}
