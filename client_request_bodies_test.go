package seclai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/seclai/seclai-go/generated"
)

// requestBodies holds the zero value of the type behind every JSON request body
// in the spec, keyed by schema name. TestRequestBodies_CoverTheSpec keeps it
// complete across spec syncs.
var requestBodies = map[string]any{
	"AddConversationTurnRequest":                                     generated.AddConversationTurnRequest{},
	"AddEmailDomainRequest":                                          generated.AddEmailDomainRequest{},
	"AgentRunRequest":                                                generated.AgentRunRequest{},
	"AgentRunStreamRequest":                                          generated.AgentRunStreamRequest{},
	"BlockEmailSenderRequest":                                        generated.BlockEmailSenderRequest{},
	"ChangeStatusRequest":                                            generated.ChangeStatusRequest{},
	"CloudDriveUpdateRequest":                                        generated.CloudDriveUpdateRequest{},
	"CreateAlertConfigRequest":                                       generated.CreateAlertConfigRequest{},
	"CreateEvaluationCriteriaRequest":                                generated.CreateEvaluationCriteriaRequest{},
	"CreateEvaluationResultRequest":                                  generated.CreateEvaluationResultRequest{},
	"CreateKnowledgeBaseBody":                                        generated.CreateKnowledgeBaseBody{},
	"CreateMemoryBankBody":                                           generated.CreateMemoryBankBody{},
	"CreateSolutionRequest":                                          generated.CreateSolutionRequest{},
	"CreateSourceBody":                                               generated.CreateSourceBody{},
	"GenerateAgentStepsRequest":                                      generated.GenerateAgentStepsRequest{},
	"GenerateStepConfigRequest":                                      generated.GenerateStepConfigRequest{},
	"InlineTextReplaceRequest":                                       generated.InlineTextReplaceRequest{},
	"InlineTextUploadRequest":                                        generated.InlineTextUploadRequest{},
	"LinkResourcesRequest":                                           generated.LinkResourcesRequest{},
	"MarkAiSuggestionRequest":                                        generated.MarkAiSuggestionRequest{},
	"MarkConversationTurnRequest":                                    generated.MarkConversationTurnRequest{},
	"PlaygroundCreateRequest":                                        generated.PlaygroundCreateRequest{},
	"SetAcceptedAttachmentsRequest":                                  generated.SetAcceptedAttachmentsRequest{},
	"SetAutoBlockModeRequest":                                        generated.SetAutoBlockModeRequest{},
	"StandaloneTestCompactionRequest":                                generated.StandaloneTestCompactionRequest{},
	"StartSourceEmbeddingMigrationRequest":                           generated.StartSourceEmbeddingMigrationRequest{},
	"TestCompactionRequest":                                          generated.TestCompactionRequest{},
	"TestDraftEvaluationRequest":                                     generated.TestDraftEvaluationRequest{},
	"UnlinkResourcesRequest":                                         generated.UnlinkResourcesRequest{},
	"UpdateAgentDefinitionRequest":                                   generated.UpdateAgentDefinitionRequest{},
	"UpdateAlertConfigRequest":                                       generated.UpdateAlertConfigRequest{},
	"UpdateApiVersionRequest":                                        generated.UpdateApiVersionRequest{},
	"UpdateEvaluationCriteriaRequest":                                generated.UpdateEvaluationCriteriaRequest{},
	"UpdateKnowledgeBaseBody":                                        generated.UpdateKnowledgeBaseBody{},
	"UpdateMemoryBankBody":                                           generated.UpdateMemoryBankBody{},
	"UpdateSolutionRequest":                                          generated.UpdateSolutionRequest{},
	"UpdateSourceBody":                                               generated.UpdateSourceBody{},
	"routers__api__agents__AgentImportPreviewRequest":                generated.RoutersApiAgentsAgentImportPreviewRequest{},
	"routers__api__agents__AgentTraceSearchRequest":                  generated.RoutersApiAgentsAgentTraceSearchRequest{},
	"routers__api__agents__CreateAgentRequest":                       generated.RoutersApiAgentsCreateAgentRequest{},
	"routers__api__agents__SetEmailTriggerConfigRequest":             generated.RoutersApiAgentsSetEmailTriggerConfigRequest{},
	"routers__api__agents__UpdateAgentRequest":                       generated.RoutersApiAgentsUpdateAgentRequest{},
	"routers__api__ai_assistant__AiAssistantFeedbackRequest":         generated.RoutersApiAiAssistantAiAssistantFeedbackRequest{},
	"routers__api__alerts__AddCommentRequest":                        generated.RoutersApiAlertsAddCommentRequest{},
	"routers__api__alerts__UpdateOrganizationAlertPreferenceRequest": generated.RoutersApiAlertsUpdateOrganizationAlertPreferenceRequest{},
	"routers__api__governance__GovernanceAiAssistantRequest":         generated.RoutersApiGovernanceGovernanceAiAssistantRequest{},
	"routers__api__memory_banks__MemoryBankAcceptRequest":            generated.RoutersApiMemoryBanksMemoryBankAcceptRequest{},
	"routers__api__memory_banks__MemoryBankAiAssistantRequest":       generated.RoutersApiMemoryBanksMemoryBankAiAssistantRequest{},
	"routers__api__solutions__AiAssistantAcceptRequest":              generated.RoutersApiSolutionsAiAssistantAcceptRequest{},
	"routers__api__solutions__AiAssistantGenerateRequest":            generated.RoutersApiSolutionsAiAssistantGenerateRequest{},
	"routers__api__source_exports__CreateExportRequest":              generated.RoutersApiSourceExportsCreateExportRequest{},
	"routers__api__source_exports__EstimateExportRequest":            generated.RoutersApiSourceExportsEstimateExportRequest{},
}

// explicitNullFields are the request fields the API requires, whose null has a
// meaning of its own and so is sent rather than omitted.
var explicitNullFields = map[string]bool{
	"SetAcceptedAttachmentsRequest.accepted_attachments": true,
}

func TestRequestBodies_CoverTheSpec(t *testing.T) {
	raw, err := os.ReadFile("openapi/seclai.openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	var missing []string
	for _, ops := range spec.Paths {
		for _, op := range ops {
			var o struct {
				RequestBody struct {
					Content map[string]struct {
						Schema struct {
							Ref string `json:"$ref"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
			}
			if json.Unmarshal(op, &o) != nil {
				continue
			}
			media, ok := o.RequestBody.Content["application/json"]
			if !ok {
				continue
			}
			name := media.Schema.Ref[strings.LastIndex(media.Schema.Ref, "/")+1:]
			if _, ok := requestBodies[name]; !ok {
				missing = append(missing, name)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("add these request schemas to requestBodies: %v", missing)
	}
}

// A zero-value request body must not send a null for any optional field the
// caller did not set: the API reads some nulls as "clear this setting". A
// required list or map left nil is the caller's to fill and is not checked.
func TestRequestBodies_ZeroValueSendsNoNulls(t *testing.T) {
	for name, body := range requestBodies {
		optional := map[string]bool{}
		typ := reflect.TypeOf(body)
		for i := 0; i < typ.NumField(); i++ {
			if f := typ.Field(i); f.Type.Kind() == reflect.Pointer {
				jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
				optional[jsonName] = true
			}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		for k, v := range fields {
			if string(v) == "null" && optional[k] && !explicitNullFields[name+"."+k] {
				t.Errorf("%s sends %q as null when unset: %s", name, k, raw)
			}
		}
	}
}

// Every nilable field reachable from a request body, nested ones included,
// must be omitted when nil.
func TestRequestBodies_NilableFieldsAreOmitEmpty(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] || typ.PkgPath() != reflect.TypeOf(generated.UpdateSourceBody{}).PkgPath() {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag := f.Tag.Get("json")
			name, opts, _ := strings.Cut(tag, ",")
			if name == "" || name == "-" {
				continue
			}
			switch f.Type.Kind() {
			case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
				if !strings.Contains(opts, "omitempty") && !explicitNullFields[typ.Name()+"."+name] {
					// A required list or map sent as [] or {} is fine; only a
					// pointer marks an optional field that a nil would null.
					if f.Type.Kind() == reflect.Pointer {
						t.Errorf("%s.%s (%s) has no omitempty", path, f.Name, typ.Name())
					}
				}
			}
			walk(path+"."+f.Name, f.Type)
		}
	}
	for name, body := range requestBodies {
		walk(name, reflect.TypeOf(body))
	}
}

func TestClient_RequestBodies_OmitUnsetFields(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"CreateAgent", func() error { _, err := c.CreateAgent(ctx, CreateAgentRequest{}); return err }},
		{"UpdateAgentDefinition", func() error {
			_, err := c.UpdateAgentDefinition(ctx, "a_1", UpdateAgentDefinitionRequest{Definition: map[string]interface{}{}})
			return err
		}},
		{"SetEmailTriggerConfig", func() error {
			_, err := c.SetEmailTriggerConfig(ctx, "a_1", "t_1", RoutersApiAgentsSetEmailTriggerConfigRequest{})
			return err
		}},
		{"UpdateCloudDrive", func() error {
			_, err := c.UpdateCloudDrive(ctx, "cd_1", CloudDriveUpdateRequest{})
			return err
		}},
		{"UpdateEvaluationCriteria", func() error {
			_, err := c.UpdateEvaluationCriteria(ctx, "ec_1", UpdateEvaluationCriteriaRequest{})
			return err
		}},
		{"UpdateSource", func() error { _, err := c.UpdateSource(ctx, "s_1", UpdateSourceBody{}); return err }},
	}
	for _, tc := range cases {
		rec = requestRecorder{}
		if err := tc.call(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for k, v := range rec.body {
			if v == nil {
				t.Errorf("%s sent %q as null: %s", tc.name, k, rec.rawBody)
			}
		}
	}

	// CreateSource has required fields; none of its optional ones may be null.
	rec = requestRecorder{}
	if _, err := c.CreateSource(ctx, CreateSourceBody{}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	for _, k := range []string{"document_images", "feed_attachments", "polling_max_items", "media_types", "retention"} {
		if _, sent := rec.body[k]; sent {
			t.Errorf("CreateSource sent unset %q: %s", k, rec.rawBody)
		}
	}
}

func TestClient_SetEmailTriggerConfig_ZeroValueIsEmptyObject(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	if _, err := c.SetEmailTriggerConfig(context.Background(), "a_1", "t_1", RoutersApiAgentsSetEmailTriggerConfigRequest{}); err != nil {
		t.Fatalf("SetEmailTriggerConfig: %v", err)
	}
	if string(rec.rawBody) != "{}" {
		t.Fatalf("expected {}, got %s", rec.rawBody)
	}
}

func TestClient_UpdateAPIVersion_NilSendsNull(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	if _, err := c.UpdateAPIVersion(context.Background(), nil); err != nil {
		t.Fatalf("UpdateAPIVersion: %v", err)
	}
	if string(rec.rawBody) != `{"version":null}` {
		t.Fatalf("expected an explicit null version, got %s", rec.rawBody)
	}
	v := "2026-10-19"
	if _, err := c.UpdateAPIVersion(context.Background(), &v); err != nil {
		t.Fatalf("UpdateAPIVersion: %v", err)
	}
	if string(rec.rawBody) != `{"version":"2026-10-19"}` {
		t.Fatalf("unexpected body %s", rec.rawBody)
	}
}

func TestClient_CreateMemoryBank_Retention(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	ctx := context.Background()
	conv := "conversation"

	// Unset: omitted, so the API's default applies.
	if _, err := c.CreateMemoryBank(ctx, CreateMemoryBankBody{Name: "m", Type: &conv}); err != nil {
		t.Fatalf("CreateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != `{"name":"m","type":"conversation"}` {
		t.Fatalf("expected retention_days omitted, got %s", rec.rawBody)
	}

	// RetentionIndefinite: an explicit null, the rest of the body unchanged.
	rec = requestRecorder{}
	indefinite := RetentionIndefinite
	if _, err := c.CreateMemoryBank(ctx, CreateMemoryBankBody{Name: "m", Type: &conv, RetentionDays: &indefinite}); err != nil {
		t.Fatalf("CreateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != `{"name":"m","retention_days":null,"type":"conversation"}` {
		t.Fatalf("expected an explicit null retention_days, got %s", rec.rawBody)
	}

	// A number of days is sent as is.
	rec = requestRecorder{}
	days := 30
	if _, err := c.CreateMemoryBank(ctx, CreateMemoryBankBody{Name: "m", Type: &conv, RetentionDays: &days}); err != nil {
		t.Fatalf("CreateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != `{"name":"m","retention_days":30,"type":"conversation"}` {
		t.Fatalf("expected retention_days 30, got %s", rec.rawBody)
	}
}

func TestClient_UpdateMemoryBank_Retention(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	ctx := context.Background()

	if _, err := c.UpdateMemoryBank(ctx, "mb_1", UpdateMemoryBankBody{}); err != nil {
		t.Fatalf("UpdateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != "{}" {
		t.Fatalf("expected {}, got %s", rec.rawBody)
	}

	rec = requestRecorder{}
	zero := 0
	if _, err := c.UpdateMemoryBank(ctx, "mb_1", UpdateMemoryBankBody{RetentionDays: &zero}); err != nil {
		t.Fatalf("UpdateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != `{"retention_days":0}` {
		t.Fatalf("expected retention_days 0, got %s", rec.rawBody)
	}

	// RetentionIndefinite is sent as 0, the update endpoint's "no window",
	// without changing the caller's value.
	rec = requestRecorder{}
	indefinite := RetentionIndefinite
	body := UpdateMemoryBankBody{RetentionDays: &indefinite}
	if _, err := c.UpdateMemoryBank(ctx, "mb_1", body); err != nil {
		t.Fatalf("UpdateMemoryBank: %v", err)
	}
	if string(rec.rawBody) != `{"retention_days":0}` {
		t.Fatalf("expected retention_days 0, got %s", rec.rawBody)
	}
	if indefinite != RetentionIndefinite {
		t.Fatalf("the caller's value was changed to %d", indefinite)
	}
}

func TestClient_ClearEvaluationCriteriaFields(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{"id":"6f1c2a4e-3b5d-4c7e-9a1f-2b3c4d5e6f70"}`)
	ctx := context.Background()

	out, err := c.ClearEvaluationCriteriaFields(ctx, "ec_1", EvaluationCriteriaEvaluationTier)
	if err != nil {
		t.Fatalf("ClearEvaluationCriteriaFields: %v", err)
	}
	rec.expect(t, http.MethodPatch, "/agents/evaluation-criteria/ec_1", "")
	if string(rec.rawBody) != `{"evaluation_tier":null}` {
		t.Fatalf("unexpected body %s", rec.rawBody)
	}
	if out == nil || out.Id.String() != "6f1c2a4e-3b5d-4c7e-9a1f-2b3c4d5e6f70" {
		t.Fatalf("expected the decoded criteria, got %+v", out)
	}

	rec = requestRecorder{}
	if _, err := c.ClearEvaluationCriteriaFields(ctx, "ec_1",
		EvaluationCriteriaDescription, EvaluationCriteriaEvaluationPrompt,
		EvaluationCriteriaExpectationConfig, EvaluationCriteriaEvaluationTier,
		EvaluationCriteriaDescription); err != nil {
		t.Fatalf("ClearEvaluationCriteriaFields: %v", err)
	}
	if string(rec.rawBody) != `{"description":null,"evaluation_prompt":null,"evaluation_tier":null,"expectation_config":null}` {
		t.Fatalf("unexpected body %s", rec.rawBody)
	}
}

func TestClient_ClearEvaluationCriteriaFields_Rejects(t *testing.T) {
	var rec requestRecorder
	c := newRecordingServer(t, &rec, 200, "application/json", `{}`)
	for name, fields := range map[string][]EvaluationCriteriaField{
		"none":    nil,
		"unknown": {EvaluationCriteriaDescription, EvaluationCriteriaField("step_id")},
	} {
		_, err := c.ClearEvaluationCriteriaFields(context.Background(), "ec_1", fields...)
		var cfg *ConfigurationError
		if !errors.As(err, &cfg) {
			t.Fatalf("%s: expected a *ConfigurationError, got %v", name, err)
		}
		if rec.method != "" {
			t.Fatalf("%s: the request must not be sent", name)
		}
	}
}
