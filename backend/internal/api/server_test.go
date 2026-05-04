package api

import (
	"testing"

	"github.com/pengmide/lumi/web"
)

func TestResolveStaticPathPrefersFileOverDirectory(t *testing.T) {
	t.Parallel()

	staticFS := web.MustFS()
	got := resolveStaticPath("c", staticFS)
	if got != "c.html" {
		t.Fatalf("resolveStaticPath(\"c\") = %q, want %q", got, "c.html")
	}
}
