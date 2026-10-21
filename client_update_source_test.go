package seclai

import (
	"context"
	"net/http"
	"testing"
)

const testSourceJSON = `{"id":"s_1","name":"Docs"}`

func TestClient_UpdateSource_SendsOnlySetFields(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", testSourceJSON)

	name := "Docs"
	if _, err := c.UpdateSource(context.Background(), "s_1", UpdateSourceBody{Name: &name}); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	rec.expect(t, http.MethodPut, "/sources/s_1", "")
	if len(rec.body) != 1 || rec.body["name"] != "Docs" {
		t.Fatalf("expected only name in the body, got %s", rec.rawBody)
	}
	if _, sent := rec.body["retention_days"]; sent {
		t.Fatalf("retention_days must not be sent when unset: %s", rec.rawBody)
	}
}

func TestClient_UpdateSource_EmptyBodyIsNoOp(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", testSourceJSON)

	if _, err := c.UpdateSource(context.Background(), "s_1", UpdateSourceBody{}); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if string(rec.rawBody) != "{}" {
		t.Fatalf("expected an empty object, got %s", rec.rawBody)
	}
}

func TestClient_UpdateSource_SendsRetentionAndZeroValues(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", testSourceJSON)

	days, zero, empty := 30, 0, []string{}
	body := UpdateSourceBody{RetentionDays: &days, CloudDriveMaxFileMb: &zero, MediaTypes: &empty}
	if _, err := c.UpdateSource(context.Background(), "s_1", body); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if rec.body["retention_days"] != float64(30) || rec.body["cloud_drive_max_file_mb"] != float64(0) {
		t.Fatalf("unexpected body %s", rec.rawBody)
	}
	if mt, ok := rec.body["media_types"].([]any); !ok || len(mt) != 0 {
		t.Fatalf("expected an explicit empty media_types, got %s", rec.rawBody)
	}
}

func TestClient_ClearSourceRetention(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", testSourceJSON)

	if _, err := c.ClearSourceRetention(context.Background(), "s_1"); err != nil {
		t.Fatalf("ClearSourceRetention: %v", err)
	}
	rec.expect(t, http.MethodPut, "/sources/s_1", "")
	if string(rec.rawBody) != `{"retention_days":null}` {
		t.Fatalf("expected only an explicit null retention_days, got %s", rec.rawBody)
	}
}
