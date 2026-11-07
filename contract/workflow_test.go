package contract

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkflowRecordJSON(t *testing.T) {
	if JobWaiting != "waiting" || JobCancelled != "cancelled" {
		t.Fatalf("job states: %q %q", JobWaiting, JobCancelled)
	}

	states := []WorkflowState{WorkflowRunning, WorkflowCompleted, WorkflowFailed,
		WorkflowCompensating, WorkflowCompensated, WorkflowCompensationFailed, WorkflowCancelled}
	for _, s := range states {
		want := WorkflowRecord{
			ID: "w1", State: s, CreatedAt: 1, FinishedAt: 2,
			Tasks: []TaskRecord{{
				Name: "a", State: JobWaiting, Deps: []string{"b"}, QueueID: "q", JobID: "j",
				Always: true, Reason: "r", ResultSize: 3,
				Undo: &TaskRecord{Name: "a", State: JobCancelled},
			}},
		}
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}

		var got WorkflowRecord
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("state %s: got %+v want %+v", s, got, want)
		}
	}
}
