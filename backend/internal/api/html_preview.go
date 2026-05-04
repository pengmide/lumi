package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pengmide/lumi/internal/device"
	workspacepreview "github.com/pengmide/lumi/internal/workspace"
)

var (
	htmlHeadTagPattern   = regexp.MustCompile(`(?i)<head[^>]*>`)
	htmlOpenTagPattern   = regexp.MustCompile(`(?i)<html[^>]*>`)
	htmlAttributePattern = regexp.MustCompile(`(?i)\b(src|href|poster|action)\s*=\s*(?:"([^"]+)"|'([^']+)')`)
	htmlStyleTagPattern  = regexp.MustCompile(`(?is)<style([^>]*)>(.*?)</style>`)
	htmlStyleAttrPattern = regexp.MustCompile(`(?i)\bstyle\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	htmlCSSURLPattern    = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]+)"|'([^']+)'|([^)"']+))\s*\)`)
	htmlCSSImportPattern = regexp.MustCompile(`(?i)@import\s+(?:url\(\s*)?(?:"([^"]+)"|'([^']+)')\s*\)?`)
	htmlAssetRoutePrefix = "/api/workspaces/html-asset/"
)

func (s *Server) handleWorkspaceHTMLPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceID := r.URL.Query().Get("workspaceId")
	runtimeInfo, err := s.resolveWorkspaceRuntime(r.Context(), workspaceID, r)
	if err != nil {
		writeResolvedRuntimeError(w, err)
		return
	}
	if runtimeInfo.Mode != "local" {
		var textFile workspacepreview.TextFile
		if err := s.deviceWorkspacePayload(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceText, device.WorkspaceRequestPayload{Path: r.URL.Query().Get("path")}, &textFile); err != nil {
			writeRuntimeWorkspaceError(w, runtimeInfo, err)
			return
		}
		if textFile.Meta.PreviewKind != workspacepreview.PreviewKindHTML {
			writeError(w, "File does not support HTML preview", http.StatusUnsupportedMediaType)
			return
		}
		s.touchResolvedRuntime(runtimeInfo)
		s.serveHTMLPreviewContent(w, runtimeInfo.WorkspaceID, textFile.Meta.Path, textFile.Content)
		return
	}

	resolvedFile, err := s.workspaceSvc.ResolveFile(runtimeInfo.WorkspacePath, r.URL.Query().Get("path"))
	if err != nil {
		writeWorkspacePreviewError(w, err)
		return
	}
	if resolvedFile.Info.IsDir() {
		writeWorkspacePreviewError(w, workspacepreview.ErrIsDirectory)
		return
	}

	meta, err := s.workspaceSvc.StatFile(runtimeInfo.WorkspacePath, resolvedFile.RelativePath)
	if err != nil {
		writeWorkspacePreviewError(w, err)
		return
	}
	if meta.PreviewKind != workspacepreview.PreviewKindHTML {
		writeError(w, "File does not support HTML preview", http.StatusUnsupportedMediaType)
		return
	}

	s.serveHTMLPreviewResponse(w, r, runtimeInfo.WorkspaceID, runtimeInfo.WorkspacePath, resolvedFile.RelativePath)
}

func (s *Server) handleWorkspaceHTMLAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceID, relativePath, ok := parseHTMLAssetRoutePath(r.URL.Path)
	if !ok || relativePath == "" {
		writeError(w, "Invalid HTML asset path", http.StatusBadRequest)
		return
	}

	runtimeInfo, err := s.resolveWorkspaceRuntime(r.Context(), workspaceID, r)
	if err != nil {
		writeResolvedRuntimeError(w, err)
		return
	}
	if runtimeInfo.Mode != "local" {
		s.handleDeviceRuntimeHTMLAsset(w, r, runtimeInfo, relativePath)
		return
	}

	resolvedFile, err := s.workspaceSvc.ResolveFile(runtimeInfo.WorkspacePath, relativePath)
	if err != nil {
		writeWorkspacePreviewError(w, err)
		return
	}
	if resolvedFile.Info.IsDir() {
		writeWorkspacePreviewError(w, workspacepreview.ErrIsDirectory)
		return
	}

	meta, err := s.workspaceSvc.StatFile(runtimeInfo.WorkspacePath, resolvedFile.RelativePath)
	if err != nil {
		writeWorkspacePreviewError(w, err)
		return
	}

	if meta.PreviewKind == workspacepreview.PreviewKindHTML {
		s.serveHTMLPreviewResponse(w, r, runtimeInfo.WorkspaceID, runtimeInfo.WorkspacePath, resolvedFile.RelativePath)
		return
	}

	if strings.HasSuffix(strings.ToLower(meta.Name), ".css") || strings.Contains(meta.MIME, "text/css") {
		cssContent, err := os.ReadFile(resolvedFile.AbsolutePath)
		if err != nil {
			writeError(w, "Failed to read CSS asset", http.StatusInternalServerError)
			return
		}

		rootAssetBase := buildHTMLAssetBaseURL(runtimeInfo.WorkspaceID, "")
		rewrittenCSS := rewriteCSSRootRelativeURLs(string(cssContent), rootAssetBase)

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Type", meta.MIME)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rewrittenCSS))
		return
	}

	file, err := os.Open(resolvedFile.AbsolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeWorkspacePreviewError(w, workspacepreview.ErrNotFound)
			return
		}
		writeError(w, "Failed to open HTML asset", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if meta.MIME != "" {
		w.Header().Set("Content-Type", meta.MIME)
	}

	http.ServeContent(w, r, meta.Name, resolvedFile.Info.ModTime(), file)
}

func (s *Server) serveHTMLPreviewResponse(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	workspaceRoot string,
	relativePath string,
) {
	textFile, err := s.workspaceSvc.ReadTextFile(workspaceRoot, relativePath)
	if err != nil {
		writeWorkspacePreviewError(w, err)
		return
	}

	htmlContent := buildHTMLPreviewDocument(workspaceID, textFile.Meta.Path, textFile.Content)

	writeHTMLPreviewContent(w, htmlContent)
}

func (s *Server) serveHTMLPreviewContent(w http.ResponseWriter, workspaceID string, relativePath string, content string) {
	writeHTMLPreviewContent(w, buildHTMLPreviewDocument(workspaceID, relativePath, content))
}

func writeHTMLPreviewContent(w http.ResponseWriter, htmlContent string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(htmlContent))
}

func (s *Server) handleDeviceRuntimeHTMLAsset(w http.ResponseWriter, r *http.Request, runtimeInfo ResolvedRuntime, relativePath string) {
	var metaResponse struct {
		Meta workspacepreview.FileMeta `json:"meta"`
	}
	if err := s.deviceWorkspacePayload(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceMeta, device.WorkspaceRequestPayload{Path: relativePath}, &metaResponse); err != nil {
		writeRuntimeWorkspaceError(w, runtimeInfo, err)
		return
	}

	if metaResponse.Meta.PreviewKind == workspacepreview.PreviewKindHTML {
		var textFile workspacepreview.TextFile
		if err := s.deviceWorkspacePayload(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceText, device.WorkspaceRequestPayload{Path: relativePath}, &textFile); err != nil {
			writeRuntimeWorkspaceError(w, runtimeInfo, err)
			return
		}
		s.touchResolvedRuntime(runtimeInfo)
		s.serveHTMLPreviewContent(w, runtimeInfo.WorkspaceID, textFile.Meta.Path, textFile.Content)
		return
	}

	if strings.HasSuffix(strings.ToLower(metaResponse.Meta.Name), ".css") || strings.Contains(metaResponse.Meta.MIME, "text/css") {
		var textFile workspacepreview.TextFile
		if err := s.deviceWorkspacePayload(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, device.MsgWorkspaceText, device.WorkspaceRequestPayload{Path: relativePath}, &textFile); err != nil {
			writeRuntimeWorkspaceError(w, runtimeInfo, err)
			return
		}
		s.touchResolvedRuntime(runtimeInfo)
		rootAssetBase := buildHTMLAssetBaseURL(runtimeInfo.WorkspaceID, "")
		rewrittenCSS := rewriteCSSRootRelativeURLs(textFile.Content, rootAssetBase)

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Type", metaResponse.Meta.MIME)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(rewrittenCSS))
		return
	}

	meta, data, err := s.deviceWorkspaceBuffer(r.Context(), runtimeInfo.DeviceID, runtimeInfo.WorkspacePath, relativePath)
	if err != nil {
		writeRuntimeWorkspaceError(w, runtimeInfo, err)
		return
	}
	s.touchResolvedRuntime(runtimeInfo)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	if meta.MIME != "" {
		w.Header().Set("Content-Type", meta.MIME)
	}
	http.ServeContent(w, r, meta.Name, time.UnixMilli(meta.ModifiedAt), bytes.NewReader(data))
}

func buildHTMLPreviewDocument(workspaceID string, relativePath string, content string) string {
	rootAssetBase := buildHTMLAssetBaseURL(workspaceID, "")
	directoryBase := buildHTMLAssetBaseURL(workspaceID, filepath.ToSlash(filepath.Dir(relativePath)))

	transformed := rewriteRootRelativeAttributes(content, rootAssetBase)
	transformed = rewriteStyleBlocks(transformed, rootAssetBase)
	transformed = rewriteStyleAttributes(transformed, rootAssetBase)

	injection := buildHTMLPreviewInjection(rootAssetBase, directoryBase)
	return injectIntoHTMLHead(transformed, injection)
}

func rewriteRootRelativeAttributes(content string, rootAssetBase string) string {
	return htmlAttributePattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := htmlAttributePattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		value := parts[2]
		quote := `"`
		if value == "" {
			value = parts[3]
			quote = `'`
		}
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return match
		}

		rewritten := rootAssetBase + strings.TrimPrefix(value, "/")
		return fmt.Sprintf(`%s=%s%s%s`, parts[1], quote, rewritten, quote)
	})
}

func rewriteStyleBlocks(content string, rootAssetBase string) string {
	return htmlStyleTagPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := htmlStyleTagPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}

		rewrittenCSS := rewriteCSSRootRelativeURLs(parts[2], rootAssetBase)
		return strings.Replace(match, parts[2], rewrittenCSS, 1)
	})
}

func rewriteStyleAttributes(content string, rootAssetBase string) string {
	return htmlStyleAttrPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := htmlStyleAttrPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}

		styleValue := parts[1]
		quote := `"`
		if styleValue == "" {
			styleValue = parts[2]
			quote = `'`
		}

		rewrittenCSS := rewriteCSSRootRelativeURLs(styleValue, rootAssetBase)
		return fmt.Sprintf(`style=%s%s%s`, quote, rewrittenCSS, quote)
	})
}

func rewriteCSSRootRelativeURLs(css string, rootAssetBase string) string {
	rewrittenCSS := htmlCSSURLPattern.ReplaceAllStringFunc(css, func(match string) string {
		parts := htmlCSSURLPattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		value := parts[1]
		quote := `"`
		if value == "" {
			value = parts[2]
			quote = `'`
		}
		if value == "" {
			value = strings.TrimSpace(parts[3])
			quote = ""
		}
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return match
		}

		rewritten := rootAssetBase + strings.TrimPrefix(value, "/")
		return fmt.Sprintf(`url(%s%s%s)`, quote, rewritten, quote)
	})

	return htmlCSSImportPattern.ReplaceAllStringFunc(rewrittenCSS, func(match string) string {
		parts := htmlCSSImportPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}

		value := parts[1]
		quote := `"`
		if value == "" {
			value = parts[2]
			quote = `'`
		}
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return match
		}

		rewritten := rootAssetBase + strings.TrimPrefix(value, "/")
		return strings.Replace(match, quote+value+quote, quote+rewritten+quote, 1)
	})
}

func buildHTMLPreviewInjection(rootAssetBase string, directoryBase string) string {
	return fmt.Sprintf(
		`<base href=%q><script>(function(cfg){function rw(u){if(!u)return u;var s=String(u);if(s.charAt(0)==='/'&&s.charAt(1)!=='/')return cfg.rootBase+s.slice(1);return s;}var _f=window.fetch;if(_f){window.fetch=function(i,n){if(typeof i==='string')return _f.call(this,rw(i),n);return _f.call(this,i,n);};}var _x=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(){var a=Array.prototype.slice.call(arguments);if(typeof a[1]==='string')a[1]=rw(a[1]);return _x.apply(this,a);};var _a=window.location.assign.bind(window.location),_r=window.location.replace.bind(window.location);window.location.assign=function(u){return _a(rw(u));};window.location.replace=function(u){return _r(rw(u));};if(window.history&&history.pushState){var _ps=history.pushState.bind(history),_rs=history.replaceState.bind(history);history.pushState=function(s,t,u){return _ps(s,t,u?rw(u):u);};history.replaceState=function(s,t,u){return _rs(s,t,u?rw(u):u);};}if(window.open){var _wo=window.open;window.open=function(u,n,f){return _wo.call(window,rw(u),n,f);};}document.addEventListener('click',function(e){var t=e.target;while(t&&t.tagName!=='A')t=t.parentElement;if(!t)return;var h=t.getAttribute('href');if(h&&h.charAt(0)==='/'&&h.charAt(1)!=='/'){t.setAttribute('href',rw(h));}},true);document.documentElement.setAttribute('data-html-preview-base',cfg.directoryBase);})({rootBase:%q,directoryBase:%q});</script>`,
		directoryBase,
		rootAssetBase,
		directoryBase,
	)
}

func injectIntoHTMLHead(content string, injection string) string {
	if location := htmlHeadTagPattern.FindStringIndex(content); location != nil {
		return content[:location[1]] + injection + content[location[1]:]
	}
	if location := htmlOpenTagPattern.FindStringIndex(content); location != nil {
		return content[:location[1]] + "<head>" + injection + "</head>" + content[location[1]:]
	}
	return "<head>" + injection + "</head>" + content
}

func buildHTMLAssetBaseURL(workspaceID string, relativePath string) string {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(relativePath)), "/")
	parts := []string{url.PathEscape(workspaceID)}
	if trimmed != "" && trimmed != "." {
		for _, segment := range strings.Split(trimmed, "/") {
			if segment == "" {
				continue
			}
			parts = append(parts, url.PathEscape(segment))
		}
	}

	return htmlAssetRoutePrefix + strings.Join(parts, "/") + "/"
}

func parseHTMLAssetRoutePath(path string) (workspaceID string, relativePath string, ok bool) {
	if !strings.HasPrefix(path, htmlAssetRoutePrefix) {
		return "", "", false
	}

	trimmed := strings.TrimPrefix(path, htmlAssetRoutePrefix)
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", "", false
	}

	rawSegments := strings.Split(trimmed, "/")
	if len(rawSegments) < 2 {
		return "", "", false
	}

	workspaceID, err := url.PathUnescape(rawSegments[0])
	if err != nil || workspaceID == "" {
		return "", "", false
	}

	segments := make([]string, 0, len(rawSegments)-1)
	for _, raw := range rawSegments[1:] {
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			return "", "", false
		}
		segments = append(segments, decoded)
	}

	return workspaceID, strings.Join(segments, "/"), true
}
