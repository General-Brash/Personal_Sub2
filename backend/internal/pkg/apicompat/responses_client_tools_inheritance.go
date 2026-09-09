package apicompat

// AdaptResponsesClientToolsWithInheritedMapping preserves client-only tool
// declarations for a WS continuation that omits tools. An explicit tools field
// (even empty or null) replaces the session declaration rather than inheriting it.
func AdaptResponsesClientToolsWithInheritedMapping(req map[string]any, inherited ResponsesClientToolMapping, inheritedLoweredTools ...[]any) (ResponsesClientToolMapping, bool, error) {
	if req == nil {
		return ResponsesClientToolMapping{}, false, nil
	}
	if _, present := req["tools"]; present {
		return AdaptResponsesClientTools(req)
	}
	if len(inherited.CustomTools) == 0 && !inherited.ToolSearch && len(inherited.NamespaceTools) == 0 {
		return ResponsesClientToolMapping{}, false, nil
	}
	if len(inheritedLoweredTools) > 0 && len(inheritedLoweredTools[0]) > 0 {
		req["tools"] = restoreInheritedResponsesClientToolDeclarations(inheritedLoweredTools[0], inherited)
		return AdaptResponsesClientTools(req)
	}
	changed := rewriteClientToolHistory(req["input"], &inherited)
	if len(inherited.NamespaceTools) > 0 {
		rewriteNamespaceQualifiedCalls(req["input"], inherited.NamespaceTools)
		if _, present := req["input"]; present {
			changed = true
		}
	}
	if rewriteClientToolChoice(req, &inherited) {
		changed = true
	}
	return inherited, changed, nil
}
