package job

import (
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

	t.Run("NewJob", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)
		job := gj.NewJob(42)
		if job.Data() != 42 {
			t.Errorf("expected 42, got %d", job.Data())
		}
	})

	t.Run("FanInResult", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)

		go func() {
			time.Sleep(100 * time.Millisecond)
			gj.SendResult(42)
		}()

		result := <-gj.Results()
		if result.Data != 42 {
			t.Errorf("expected 42, got %d", result.Data)
		}
	})

	t.Run("Drain", func(t *testing.T) {
		gj := NewGroupJob[int, int](1)

		gj.SendResult(42)
		gj.Drain()

		select {
		case <-gj.Results():
		case <-time.After(5 * time.Second):
			t.Fatal("Result channel not drained")
		}
	})
}
