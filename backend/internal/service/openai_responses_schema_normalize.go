package service

import "strings"

func normalizeOpenAIResponseFormatSchemas(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	modified := false
	normalizeFormat := func(format map[string]any) {
		if format == nil || strings.TrimSpace(firstNonEmptyString(format["type"])) != "json_schema" {
			return
		}
		if schema, ok := format["schema"].(map[string]any); ok && normalizeOpenAIResponseJSONSchema(schema) {
			modified = true
		}
		if jsonSchema, ok := format["json_schema"].(map[string]any); ok {
			if schema, ok := jsonSchema["schema"].(map[string]any); ok && normalizeOpenAIResponseJSONSchema(schema) {
				modified = true
			}
		}
	}
	if text, ok := reqBody["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok {
			normalizeFormat(format)
		}
	}
	if responseFormat, ok := reqBody["response_format"].(map[string]any); ok {
		normalizeFormat(responseFormat)
	}
	return modified
}

func normalizeOpenAIResponseJSONSchema(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	modified := false
	for _, key := range []string{"uniqueItems", "minProperties"} {
		if _, exists := schema[key]; exists {
			delete(schema, key)
			modified = true
		}
	}
	if rawType, exists := schema["type"]; !exists || rawType == nil {
		switch {
		case schema["properties"] != nil:
			schema["type"] = "object"
			modified = true
		case schema["items"] != nil:
			schema["type"] = "array"
			modified = true
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for _, raw := range properties {
			if child, ok := raw.(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
				modified = true
			}
		}
	}
	switch items := schema["items"].(type) {
	case map[string]any:
		if normalizeOpenAIResponseJSONSchema(items) {
			modified = true
		}
	case []any:
		for _, raw := range items {
			if child, ok := raw.(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
				modified = true
			}
		}
	}
	for _, key := range []string{"additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems"} {
		if child, ok := schema[key].(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
			modified = true
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		if children, ok := schema[key].([]any); ok {
			for _, raw := range children {
				if child, ok := raw.(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
					modified = true
				}
			}
		}
	}
	for _, key := range []string{"$defs", "definitions", "patternProperties", "dependentSchemas"} {
		if children, ok := schema[key].(map[string]any); ok {
			for _, raw := range children {
				if child, ok := raw.(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
					modified = true
				}
			}
		}
	}
	if dependencies, ok := schema["dependencies"].(map[string]any); ok {
		for _, raw := range dependencies {
			if child, ok := raw.(map[string]any); ok && normalizeOpenAIResponseJSONSchema(child) {
				modified = true
			}
		}
	}
	return modified
}
