package cli

import "testing"

func TestUnresolvedCheckpointKeepsCreatingNativeJournalBlocking(t *testing.T) {
	record := checkpointRecord{Kind: checkpointKindAWSAMI}
	record.Native.ImageID = "journal-name"
	record.Native.State = "creating"
	if !unresolvedCheckpoint(record) {
		t.Fatal("a native record still in creating state must keep blocking source admission")
	}
	record.Native.State = "available"
	if unresolvedCheckpoint(record) {
		t.Fatal("an available native record with a delete identity is resolved")
	}
}
