package contract

import (
	"encoding/json"
	"testing"
)

// A writer's record (BeatAt 0, Lost false) has the spec's field names and no
// beat_at or lost (spec/redis-storage.md, section 8.2).
func TestInstanceRecordJSON(t *testing.T) {
	r := InstanceRecord{
		ID: "pod-a", Host: "pod-a", PID: 7, Version: "v1.11.0",
		StartedAt: 1, IntervalMS: 5000,
		Workers: []InstanceWorker{{
			Name: "emails", Status: "Running", Concurrency: 4,
			NumProcessing: 1, NumIdleWorkers: 3, Completed: 9, Successful: 8, Failed: 1,
			QueueIDs: []string{"redisq:prio:{emails}"},
		}},
	}
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"pod-a","host":"pod-a","pid":7,"version":"v1.11.0","started_at":1,"interval_ms":5000,` +
		`"workers":[{"name":"emails","status":"Running","concurrency":4,"num_processing":1,"num_idle_workers":3,"completed":9,"successful":8,"failed":1,"queue_ids":["redisq:prio:{emails}"]}]}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
