package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
)

const pluginVersion = "0.1.0"

var (
	requestCount    atomic.Uint64
	changedCount    atomic.Uint64
	redactedCount   atomic.Uint64
	environmentLine = regexp.MustCompile(`(?im)^([\t ]*(?:[-*][\t ]+)?)(primary working directory|current working directory|working directory|project root|additional working directories|cwd|home directory|home|username|user name|hostname|host name|computer name|machine name|local timezone|time zone|timezone|preferred locale|system locale|locale|country|region|client ip|local ip|public ip|ip address|ip|os version)[\t ]*:[\t ]*([^\r\n]*)(\r?)$`)
	environmentTag  = regexp.MustCompile(`(?is)<(cwd|working_directory|home_directory|hostname|username|timezone|locale|country|region|client_ip|local_ip|os_version)>[^<]*</(?:cwd|working_directory|home_directory|hostname|username|timezone|locale|country|region|client_ip|local_ip|os_version)>`)
	windowsVersion  = regexp.MustCompile(`(?i)\bWindows\s+(?:Server(?:\s+\d{4})?|\d{1,2})\b`)
)

type transformRequest struct {
	FromFormat string
	ToFormat   string
	Model      string
	Stream     bool
	Body       []byte
}

type payloadResponse struct{ Body []byte }

type envelope struct {
	OK     bool         `json:"ok"`
	Result any          `json:"result,omitempty"`
	Error  *pluginError `json:"error,omitempty"`
}

type pluginError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {}

func keyName(value string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(value))
}

func privateMetadataKey(key string) bool {
	switch keyName(key) {
	case "ip", "ipaddress", "clientip", "localip", "publicip", "sourceip", "forwardedfor", "xforwardedfor",
		"timezone", "tz", "timezoneoffset", "utcoffset", "locale", "systemlocale", "language", "acceptlanguage", "country", "countrycode", "region", "regioncode",
		"location", "userlocation", "geolocation", "latitude", "longitude", "hostname", "computername", "machinename",
		"username", "usernickname", "osversion", "cwd", "workingdirectory", "homedirectory", "homedir":
		return true
	}
	return false
}

func publicOS(value string) string {
	lower := strings.ToLower(value)
	if version := windowsVersion.FindString(value); version != "" {
		return version
	}
	switch {
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "macos"), strings.Contains(lower, "mac os"), strings.Contains(lower, "darwin"):
		return "MacOS"
	case strings.Contains(lower, "linux"), strings.Contains(lower, "ubuntu"), strings.Contains(lower, "debian"), strings.Contains(lower, "fedora"):
		return "Linux"
	case strings.Contains(lower, "freebsd"):
		return "FreeBSD"
	default:
		return "[redacted]"
	}
}

func environmentValue(key, value string) string {
	switch keyName(key) {
	case "primaryworkingdirectory", "currentworkingdirectory", "workingdirectory", "projectroot", "cwd":
		return "."
	case "home", "homedirectory":
		return "~"
	case "username":
		return "user"
	case "hostname", "computername", "machinename":
		return "localhost"
	case "osversion":
		return publicOS(value)
	default:
		return "[redacted]"
	}
}

func generatedEnvironment(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "# environment") || strings.Contains(lower, "<env>") ||
		strings.Contains(lower, "<environment_context>") ||
		strings.Contains(lower, "you have been invoked in the following environment") ||
		strings.Contains(lower, "information about the environment you are running in")
}

func redactEnvironment(text string) (string, int) {
	if !generatedEnvironment(text) {
		return text, 0
	}
	changes := 0
	out := environmentLine.ReplaceAllStringFunc(text, func(line string) string {
		parts := environmentLine.FindStringSubmatch(line)
		next := parts[1] + parts[2] + ": " + environmentValue(parts[2], parts[3]) + parts[4]
		if next != line {
			changes++
		}
		return next
	})
	out = environmentTag.ReplaceAllStringFunc(out, func(tag string) string {
		parts := environmentTag.FindStringSubmatch(tag)
		next := "<" + parts[1] + ">" + environmentValue(parts[1], "") + "</" + parts[1] + ">"
		if next != tag {
			changes++
		}
		return next
	})
	return out, changes
}

func redactContent(content any, allowPlain bool) (any, int) {
	changes := 0
	switch v := content.(type) {
	case string:
		if allowPlain || strings.Contains(v, "<system-reminder>") {
			return redactEnvironment(v)
		}
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || block["type"] != "text" {
				continue
			}
			text, ok := block["text"].(string)
			if !ok || (!allowPlain && !strings.Contains(text, "<system-reminder>")) {
				continue
			}
			updated, count := redactEnvironment(text)
			if count > 0 {
				block["text"] = updated
				changes += count
			}
		}
	}
	return content, changes
}

func redactMetadata(object map[string]any) int {
	changes := 0
	for key, value := range object {
		if privateMetadataKey(key) {
			delete(object, key)
			changes++
			continue
		}
		if key == "user_id" {
			text, ok := value.(string)
			if !ok {
				continue
			}
			var identity map[string]any
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if decoder.Decode(&identity) != nil || identity == nil {
				continue
			}
			if count := redactMetadata(identity); count > 0 {
				if encoded, err := json.Marshal(identity); err == nil {
					object[key] = string(encoded)
					changes += count
				}
			}
		} else if child, ok := value.(map[string]any); ok {
			changes += redactMetadata(child)
		} else if children, ok := value.([]any); ok {
			for _, child := range children {
				if nested, ok := child.(map[string]any); ok {
					changes += redactMetadata(nested)
				}
			}
		}
	}
	return changes
}

func normalize(body []byte, target string) ([]byte, int, error) {
	if target != "claude" && target != "anthropic" {
		return body, 0, nil
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil || root == nil {
		return nil, 0, errors.New("invalid provider JSON")
	}
	changes := 0
	if content, ok := root["system"]; ok {
		updated, count := redactContent(content, true)
		if count > 0 {
			root["system"] = updated
			changes += count
		}
	}
	if messages, ok := root["messages"].([]any); ok {
		for _, item := range messages {
			message, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role, _ := message["role"].(string)
			if role != "system" && role != "developer" && role != "user" {
				continue
			}
			updated, count := redactContent(message["content"], role != "user")
			if count > 0 {
				message["content"] = updated
				changes += count
			}
		}
	}
	if metadata, ok := root["metadata"].(map[string]any); ok {
		changes += redactMetadata(metadata)
	}
	if diagnostics, ok := root["diagnostics"].(map[string]any); ok {
		changes += redactMetadata(diagnostics)
	}
	if tools, ok := root["tools"].([]any); ok {
		for _, item := range tools {
			tool, ok := item.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := tool["type"].(string)
			if strings.HasPrefix(kind, "web_search") {
				if _, present := tool["user_location"]; present {
					delete(tool, "user_location")
					changes++
				}
			}
		}
	}
	if changes == 0 {
		return body, 0, nil
	}
	encoded, err := json.Marshal(root)
	return encoded, changes, err
}

func result(value any) []byte {
	encoded, _ := json.Marshal(envelope{OK: true, Result: value})
	return encoded
}

func failure(code, message string) []byte {
	encoded, _ := json.Marshal(envelope{OK: false, Error: &pluginError{Code: code, Message: message}})
	return encoded
}

func handleMethod(method string, request []byte) []byte {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return result(map[string]any{"schema_version": 6,
			"metadata":     map[string]any{"Name": "Claude privacy redactor", "Version": pluginVersion, "Author": "Local deployment", "GitHubRepository": "local:privacy-redactor", "Logo": "", "ConfigFields": []any{}},
			"capabilities": map[string]any{"request_normalizer": true, "management_api": true}})
	case "plugin.quiesce", "plugin.shutdown":
		return result(map[string]any{})
	case "request.normalize":
		var req transformRequest
		if json.Unmarshal(request, &req) != nil {
			return failure("invalid_request", "Invalid transform envelope")
		}
		out, count, err := normalize(req.Body, req.ToFormat)
		if err != nil {
			return failure("invalid_request", "Invalid provider JSON")
		}
		requestCount.Add(1)
		if count > 0 {
			changedCount.Add(1)
			redactedCount.Add(uint64(count))
		}
		return result(payloadResponse{Body: out})
	case "management.register":
		return result(map[string]any{"routes": []any{map[string]any{"Method": "GET", "Path": "/privacy-redactor/status"}}})
	case "management.handle":
		var req struct {
			Method string
			Path   string
		}
		if json.Unmarshal(request, &req) != nil {
			return failure("invalid_request", "Invalid management envelope")
		}
		if req.Method != "GET" || !strings.HasSuffix(req.Path, "/privacy-redactor/status") {
			return result(map[string]any{"StatusCode": 404, "Body": []byte(`{"error":"not found"}`)})
		}
		status, _ := json.Marshal(map[string]any{"plugin": "privacy-redactor", "version": pluginVersion,
			"normalized_requests": requestCount.Load(), "changed_requests": changedCount.Load(), "fields_redacted": redactedCount.Load(),
			"scope": "generated environment context, location metadata, and web search user_location; user content and tool arguments remain unchanged"})
		return result(map[string]any{"StatusCode": 200, "Headers": map[string][]string{"Content-Type": {"application/json"}}, "Body": status})
	default:
		return failure("unknown_method", "Unsupported plugin method")
	}
}
