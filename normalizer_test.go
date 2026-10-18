package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestNativeEnvironmentPrivacyPreservesProtocol(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-5-5","messages":[{"role":"user","content":"Please edit code about China in C:\\Users\\alice\\app"},{"role":"system","content":[{"type":"text","text":"# Environment\nYou have been invoked in the following environment:\n - Primary working directory: C:\\Users\\alice\\示例项目\n - Platform: win32\n - Shell: bash\n - OS Version: Windows 11 Home China 10.0.26200\n - Timezone: Asia/Shanghai\n - Hostname: CHINA-PC\nToday's date is 2026-10-18.","cache_control":{"type":"ephemeral"}}]},{"role":"assistant","content":[{"type":"thinking","thinking":"opaque China","signature":"signed-value"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"C:\\Users\\alice\\app\\main.go"}}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.302; cch=abc;"}],"metadata":{"user_id":"{\"device_id\":\"device\",\"account_uuid\":\"account\",\"session_id\":\"session\",\"timezone\":\"Asia/Shanghai\",\"country\":\"CN\"}","client_ip":"198.51.100.1"},"max_tokens":128000,"unknown_integer":9007199254740993}`)
	out, count, err := normalize(body, "claude")
	if err != nil || count < 5 {
		t.Fatalf("normalize failed: %v, count=%d", err, count)
	}
	for _, forbidden := range []string{"Home China", "Asia/Shanghai", "CHINA-PC", "示例项目", "198.51.100.1"} {
		if bytes.Contains(out, []byte(forbidden)) {
			t.Errorf("private environment remained: %s", forbidden)
		}
	}
	for _, needed := range []string{"Windows 11", "Platform: win32", "Shell: bash", "2026-10-18", "signed-value", "cc_version=2.1.302", "9007199254740993", "Please edit code about China", "file_path"} {
		if !bytes.Contains(out, []byte(needed)) {
			t.Errorf("protocol or task content lost: %s", needed)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	meta := decoded["metadata"].(map[string]any)
	var identity map[string]any
	if err := json.Unmarshal([]byte(meta["user_id"].(string)), &identity); err != nil {
		t.Fatal(err)
	}
	if len(identity) != 3 || identity["device_id"] != "device" || identity["session_id"] != "session" {
		t.Fatal("native client identity shape changed")
	}
	again, changes, err := normalize(out, "claude")
	if err != nil || changes != 0 || !bytes.Equal(again, out) {
		t.Fatalf("not idempotent: %v, %d", err, changes)
	}
}

func TestUserToolResultsAndSchemasUnchanged(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"# Environment\nOS Version: China is part of this document"},{"type":"tool_result","tool_use_id":"x","content":"Timezone: Asia/Shanghai; C:\\Users\\alice\\app"}]}],"tools":[{"name":"custom","input_schema":{"properties":{"country":{"type":"string"}}}}]}`)
	out, count, err := normalize(body, "claude")
	if err != nil || count != 0 || !bytes.Equal(out, body) {
		t.Fatal("user material or tools changed")
	}
}

func TestSearchLocationRemovedWithoutChangingQuery(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"Search China weather"}],"tools":[{"type":"web_search_20250305","name":"web_search","allowed_domains":["example.com"],"user_location":{"type":"approximate","country":"CN","timezone":"Asia/Shanghai"}}]}`)
	out, count, err := normalize(body, "claude")
	if err != nil || count != 1 || bytes.Contains(out, []byte("user_location")) || !bytes.Contains(out, []byte("Search China weather")) || !bytes.Contains(out, []byte("allowed_domains")) {
		t.Fatalf("search transform failed: %v", err)
	}
}

func TestGeneratedUserReminderAndXML(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"<system-reminder><environment_context><cwd>C:/Users/alice/项目</cwd><timezone>Asia/Shanghai</timezone><shell>bash</shell></environment_context></system-reminder>\nUser request stays here"}]}`)
	out, count, err := normalize(body, "claude")
	if err != nil || count != 2 || bytes.Contains(out, []byte("Asia/Shanghai")) || !bytes.Contains(out, []byte("bash")) || !bytes.Contains(out, []byte("User request stays here")) {
		t.Fatal("reminder transform failed")
	}
}

func TestOtherProvidersAndCleanRequestsRemainByteExact(t *testing.T) {
	for _, target := range []string{"openai", "openai-response", "claude"} {
		body := []byte(`{ "messages": [{"role":"user","content":"ordinary task"}], "tools": [] }`)
		out, count, err := normalize(body, target)
		if err != nil || count != 0 || !bytes.Equal(out, body) {
			t.Errorf("unexpected change for %s", target)
		}
	}
}

func TestABINormalizerEnvelopeAndConcurrentCalls(t *testing.T) {
	request, _ := json.Marshal(transformRequest{ToFormat: "claude", Body: []byte(`{"system":"# Environment\nOS Version: Windows 11 Home China"}`)})
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response := handleMethod("request.normalize", request)
			var result struct {
				OK     bool            `json:"ok"`
				Result payloadResponse `json:"result"`
			}
			if json.Unmarshal(response, &result) != nil || !result.OK || strings.Contains(string(result.Result.Body), "China") {
				t.Error("normalizer RPC failed")
			}
		}()
	}
	group.Wait()
	if requestCount.Load() < 20 || changedCount.Load() < 20 {
		t.Fatal("counters did not update")
	}
}
