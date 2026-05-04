package api

import (
	"strings"
	"testing"
)

func TestBuildHTMLPreviewDocumentInjectsBaseAndRewritesRootRelativeAssets(t *testing.T) {
	t.Parallel()

	source := `<!DOCTYPE html><html><head><link rel="stylesheet" href="/styles/app.css"><style>body{background:url('/images/bg.png')}</style></head><body><img src="images/photo.png"><script src="/scripts/app.js"></script></body></html>`

	result := buildHTMLPreviewDocument("ws-1", "pages/demo/index.html", source)

	if !containsAll(result,
		`<base href="/api/workspaces/html-asset/ws-1/pages/demo/">`,
		`href="/api/workspaces/html-asset/ws-1/styles/app.css"`,
		`src="/api/workspaces/html-asset/ws-1/scripts/app.js"`,
		`url('/api/workspaces/html-asset/ws-1/images/bg.png')`,
		`src="images/photo.png"`,
	) {
		t.Fatalf("buildHTMLPreviewDocument() did not inject expected HTML:\n%s", result)
	}
}

func TestParseHTMLAssetRoutePath(t *testing.T) {
	t.Parallel()

	workspaceID, relativePath, ok := parseHTMLAssetRoutePath("/api/workspaces/html-asset/for-acp/public/assets/app.js")
	if !ok {
		t.Fatalf("parseHTMLAssetRoutePath() expected ok=true")
	}
	if workspaceID != "for-acp" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "for-acp")
	}
	if relativePath != "public/assets/app.js" {
		t.Fatalf("relativePath = %q, want %q", relativePath, "public/assets/app.js")
	}
}

func containsAll(content string, expected ...string) bool {
	for _, value := range expected {
		if !strings.Contains(content, value) {
			return false
		}
	}

	return true
}
