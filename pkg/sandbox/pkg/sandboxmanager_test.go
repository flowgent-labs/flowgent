package sandbox

import "testing"

func TestTryParseJSONUsesLastJSONObjectLine(t *testing.T) {
	parsed, ok := tryParseJSON("Cloning into repo...\n{\"path\":\"/tmp/repo\",\"status\":\"ready\"}\n")
	if !ok {
		t.Fatal("expected parser to accept final JSON object line")
	}
	if got := parsed["path"]; got != "/tmp/repo" {
		t.Fatalf("expected parsed path, got %v", got)
	}
}

func TestAttachParsedOutputFlattensSemanticFields(t *testing.T) {
	out := map[string]any{
		"stdout":    "{\"stdout\":\"semantic\",\"issues\":[1],\"path\":\"/repo\"}",
		"stderr":    "",
		"runtime":   "bash",
		"exit_code": 0,
	}
	attachParsedOutput(out, out["stdout"].(string))
	if got := out["path"]; got != "/repo" {
		t.Fatalf("expected flattened path, got %v", got)
	}
	if _, ok := out["issues"].([]any); !ok {
		t.Fatalf("expected flattened issues array, got %T", out["issues"])
	}
	if got := out["stdout"]; got != "{\"stdout\":\"semantic\",\"issues\":[1],\"path\":\"/repo\"}" {
		t.Fatalf("diagnostic stdout should not be overwritten, got %v", got)
	}
	if _, ok := out["parsed"].(map[string]any); !ok {
		t.Fatalf("expected parsed diagnostic map, got %T", out["parsed"])
	}
}
