package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolInfoOmitsDisabledDevelop(t *testing.T) {
	data, err := json.Marshal(ToolInfo{Version: "1.2.3", Commit: "abc1234"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "develop") {
		t.Fatalf("develop=false must be omitted: %s", data)
	}
}

func TestToolInfoKeepsDevelop(t *testing.T) {
	data, err := json.Marshal(ToolInfo{Version: "devel", Commit: "abc1234", Develop: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"develop":true`) {
		t.Fatalf("develop=true is missing: %s", data)
	}
}
