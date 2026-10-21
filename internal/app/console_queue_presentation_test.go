package app

import "testing"

func TestConsoleQueuePresentationTransportInputs(t *testing.T) {
	for _, state := range []string{"succeeded", "failed", "cancelled", "denied", "expired"} {
		if !QueuePresentationTerminal(state) {
			t.Errorf("terminal %s denied", state)
		}
	}
	for _, state := range []string{"", "pending", "running", "unknown"} {
		if QueuePresentationTerminal(state) {
			t.Errorf("nonterminal %s accepted", state)
		}
	}
	for _, input := range [][2]string{{"succeeded", "unarchived"}, {"failed", "archived"}, {"all", "all"}} {
		if _, err := ConsoleQueuePreferencesInput(input[0], input[1]); err != nil {
			t.Errorf("valid preferences %v: %v", input, err)
		}
	}
	for _, input := range [][2]string{{"unknown", "all"}, {"all", "unknown"}, {"", ""}} {
		if _, err := ConsoleQueuePreferencesInput(input[0], input[1]); err == nil {
			t.Errorf("invalid preferences %v accepted", input)
		}
	}
	requeue := ConsoleQueueRequeueInput("item", "digest", "identity")
	archive := ConsoleQueueArchiveInput("item", "digest", "identity", true, 7)
	if archive.QueueItem != requeue.QueueItem || archive.RequestID != requeue.IdempotencyKey || !archive.Archived || archive.ExpectedRevision != 7 {
		t.Fatal("transport inputs lost exact pins or CAS")
	}
}
