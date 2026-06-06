package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"

	"go.klarlabs.de/axi"
)

// TestServe_InitializeListCall drives the full dispatch chain through stdio
// semantics. Protects the example from silent rot as axi-go evolves.
func TestServe_InitializeListCall(t *testing.T) {
	kernel := axi.New().WithBudget(axi.Budget{MaxCapabilityInvocations: 10})
	kernel.RegisterActionExecutor("exec.echo.upper", &upperExecutor{})
	if err := kernel.RegisterPlugin(&echoPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo.upper","arguments":{"text":"hello"}}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	decoder := json.NewDecoder(&out)

	// 1. initialize
	var init rpcResponse
	if err := decoder.Decode(&init); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if init.Error != nil {
		t.Fatalf("initialize error: %+v", init.Error)
	}

	// 2. tools/list — must include echo.upper
	var list rpcResponse
	if err := decoder.Decode(&list); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	listJSON, _ := json.Marshal(list.Result)
	if !strings.Contains(string(listJSON), `"echo.upper"`) {
		t.Errorf("tools/list missing echo.upper; got: %s", listJSON)
	}
	if !strings.Contains(string(listJSON), `"required":["text"]`) {
		t.Errorf("tools/list schema missing required field; got: %s", listJSON)
	}

	// 3. tools/call — expect TOON-encoded uppercase result in content[0].text
	var call rpcResponse
	if err := decoder.Decode(&call); err != nil {
		t.Fatalf("decode tools/call: %v", err)
	}
	if call.Error != nil {
		t.Fatalf("tools/call error: %+v", call.Error)
	}
	callJSON, _ := json.Marshal(call.Result)
	if !strings.Contains(string(callJSON), "HELLO") {
		t.Errorf("tools/call result missing HELLO; got: %s", callJSON)
	}
}

func TestServe_UnknownMethod(t *testing.T) {
	kernel := axi.New()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"does.not.exist"}` + "\n")
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != rpcCodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpcCodeMethodNotFound)
	}
	if !strings.Contains(resp.Error.Message, "does.not.exist") {
		t.Errorf("error should mention the method: %s", resp.Error.Message)
	}
}

func TestServe_MalformedJSON(t *testing.T) {
	kernel := axi.New()

	// Missing closing brace.
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"` + "\n")
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != rpcCodeParseError {
		t.Errorf("error code = %d, want %d", resp.Error.Code, rpcCodeParseError)
	}
}

func TestServe_WrongJSONRPCVersion(t *testing.T) {
	kernel := axi.New()

	in := strings.NewReader(`{"jsonrpc":"1.0","id":1,"method":"initialize"}` + "\n")
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != rpcCodeInvalidRequest {
		t.Fatalf("expected InvalidRequest error, got %+v", resp.Error)
	}
}

func TestServe_CallMissingToolName(t *testing.T) {
	kernel := axi.New()

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"arguments":{}}}` + "\n")
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != rpcCodeInvalidParams {
		t.Fatalf("expected InvalidParams error, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "name") {
		t.Errorf("error should mention the missing name: %s", resp.Error.Message)
	}
}

func TestServe_CallTriggersApprovalPause(t *testing.T) {
	kernel := axi.New().WithBudget(axi.Budget{MaxCapabilityInvocations: 10})
	kernel.RegisterActionExecutor("exec.echo.upper", &upperExecutor{})
	kernel.RegisterActionExecutor("exec.notify.send", &notifyExecutor{})
	if err := kernel.RegisterPlugin(&echoPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"notify.send","arguments":{"to":"alice@example.com","message":"hi"}}}` + "\n")
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected success (pause is not an error), got %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(body), "awaiting_approval") {
		t.Errorf("response should signal the pause: %s", body)
	}
	if !strings.Contains(string(body), "session:") {
		t.Errorf("response should carry session id: %s", body)
	}
	// IsError must be false — a pause is a legitimate non-error state.
	if strings.Contains(string(body), `"isError":true`) {
		t.Errorf("isError should be false on approval pause: %s", body)
	}
}

func TestServe_CallTooManyArgumentEntries(t *testing.T) {
	kernel := axi.New().WithBudget(axi.Budget{MaxCapabilityInvocations: 10})
	kernel.RegisterActionExecutor("exec.echo.upper", &upperExecutor{})
	_ = kernel.RegisterPlugin(&echoPlugin{})

	// Build arguments map with maxArgumentEntries+1 keys.
	var args strings.Builder
	args.WriteString("{")
	for i := 0; i < maxArgumentEntries+1; i++ {
		if i > 0 {
			args.WriteString(",")
		}
		fmt.Fprintf(&args, `"k%d":"v"`, i)
	}
	args.WriteString("}")

	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo.upper","arguments":%s}}`+"\n", args.String())
	in := strings.NewReader(req)
	var out bytes.Buffer
	serve(in, &out, log.New(bytes.NewBuffer(nil), "", 0), kernel)

	var resp rpcResponse
	if err := json.NewDecoder(&out).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != rpcCodeInvalidParams {
		t.Fatalf("expected InvalidParams for oversized arguments, got %+v", resp.Error)
	}
}
