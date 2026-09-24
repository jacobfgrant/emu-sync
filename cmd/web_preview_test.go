package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/manifest"
)

// previewServer returns a web server whose remote and local manifests both
// list the given keys (all selected and synced), each 1 MB.
func previewServer(t *testing.T, keys ...string) *webServer {
	t.Helper()
	ws, tmpDir := setupSyncWebServer(t)

	remote := manifest.New()
	local := manifest.New()
	group := &systemGroup{Dir: "roms/snes"}
	for _, key := range keys {
		entry := manifest.FileEntry{Size: 1024 * 1024, MD5: "a"}
		remote.Files[key] = entry
		local.Files[key] = entry
		group.Files = append(group.Files, fileInfo{Key: key, Name: filepath.Base(key), Size: entry.Size, Selected: true})
	}
	ws.remoteManifest = remote
	ws.groups = []*systemGroup{group}
	ws.cfg.Sync.SyncDirs = []string{"roms"}
	ws.cfg.Sync.Delete = true

	ws.localManifestPath = filepath.Join(tmpDir, "local.json")
	if err := local.SaveJSON(ws.localManifestPath); err != nil {
		t.Fatal(err)
	}
	return ws
}

func postPreview(t *testing.T, ws *webServer, body string) previewResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	ws.handlePreview(rec, httptest.NewRequest("POST", "/api/preview", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("preview: got %d: %s", rec.Code, rec.Body.String())
	}
	var resp previewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding preview: %v", err)
	}
	return resp
}

func TestPreviewReportsDeselectedFiles(t *testing.T) {
	ws := previewServer(t, "roms/snes/A.sfc", "roms/snes/B.sfc")

	resp := postPreview(t, ws, `{"selections":{"roms/snes/A.sfc":true,"roms/snes/B.sfc":false},"delete":true}`)

	if resp.DeleteCount != 1 || len(resp.DeleteFiles) != 1 || resp.DeleteFiles[0] != "roms/snes/B.sfc" {
		t.Errorf("preview = %+v, want B.sfc deleted", resp)
	}
	if resp.DeleteSize != 1024*1024 || resp.DeleteSizeFormatted != "1 MB" {
		t.Errorf("size = %d (%s), want 1 MB", resp.DeleteSize, resp.DeleteSizeFormatted)
	}
}

func TestPreviewDoesNotSaveOrApply(t *testing.T) {
	ws := previewServer(t, "roms/snes/A.sfc", "roms/snes/B.sfc")

	postPreview(t, ws, `{"selections":{"roms/snes/A.sfc":false,"roms/snes/B.sfc":false},"delete":true}`)

	if _, err := os.Stat(ws.cfgPath); !os.IsNotExist(err) {
		t.Error("preview should not write the config")
	}
	if !ws.cfg.ShouldSync("roms/snes/B.sfc") {
		t.Error("preview should not change the in-memory config")
	}
	for _, f := range ws.groups[0].Files {
		if !f.Selected {
			t.Errorf("preview should not change group selection for %s", f.Key)
		}
	}
}

func TestPreviewNothingWhenDeleteOff(t *testing.T) {
	ws := previewServer(t, "roms/snes/A.sfc", "roms/snes/B.sfc")

	resp := postPreview(t, ws, `{"selections":{"roms/snes/B.sfc":false},"delete":false}`)
	if resp.DeleteCount != 0 {
		t.Errorf("DeleteCount with delete off = %d, want 0", resp.DeleteCount)
	}
}

func TestPreviewNothingWhenSelectionUnchanged(t *testing.T) {
	ws := previewServer(t, "roms/snes/A.sfc")

	resp := postPreview(t, ws, `{"selections":{"roms/snes/A.sfc":true},"delete":true}`)
	if resp.DeleteCount != 0 {
		t.Errorf("DeleteCount = %d, want 0", resp.DeleteCount)
	}
}

func TestPreviewListsLimitedFiles(t *testing.T) {
	var keys []string
	selections := map[string]bool{}
	for i := 0; i < previewFileLimit+5; i++ {
		key := fmt.Sprintf("roms/snes/Game%02d.sfc", i)
		keys = append(keys, key)
		selections[key] = false
	}
	ws := previewServer(t, keys...)
	body, _ := json.Marshal(map[string]any{"selections": selections, "delete": true})

	resp := postPreview(t, ws, string(body))
	if resp.DeleteCount != len(keys) {
		t.Errorf("DeleteCount = %d, want %d", resp.DeleteCount, len(keys))
	}
	if len(resp.DeleteFiles) != previewFileLimit {
		t.Errorf("listed %d files, want %d", len(resp.DeleteFiles), previewFileLimit)
	}
}

func TestPreviewRejectsGet(t *testing.T) {
	ws := previewServer(t, "roms/snes/A.sfc")
	rec := httptest.NewRecorder()
	ws.handlePreview(rec, httptest.NewRequest("GET", "/api/preview", nil))
	if rec.Code != 405 {
		t.Errorf("got %d, want 405", rec.Code)
	}
}
