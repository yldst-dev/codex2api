package gateway

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
)

//go:embed instructions.txt
var defaultInstructions string

func prepareBody(body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("request body is empty")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	needInstructions := !nonEmptyJSONString(root["instructions"])
	needEffort := effortIsMinimal(root["reasoning"])
	if !needInstructions && !needEffort {
		return body, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
		return nil, errors.New("request body must be a JSON object")
	}
	if needInstructions {
		obj["instructions"] = strings.TrimSpace(defaultInstructions)
	}
	if needEffort {
		reasoning, _ := obj["reasoning"].(map[string]any)
		if reasoning == nil {
			reasoning = map[string]any{}
			obj["reasoning"] = reasoning
		}
		reasoning["effort"] = "none"
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func nonEmptyJSONString(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return strings.TrimSpace(value) != ""
}

func effortIsMinimal(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(reasoning.Effort), "minimal")
}

func codexManifestToOpenAIList(body []byte) []byte {
	var envelope struct {
		Models []struct {
			Slug string `json:"slug"`
			ID   string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Models) == 0 {
		return body
	}
	type item struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]item, 0, len(envelope.Models))
	for _, model := range envelope.Models {
		id := strings.TrimSpace(model.Slug)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if id == "" {
			continue
		}
		data = append(data, item{ID: id, Object: "model", OwnedBy: "openai"})
	}
	if len(data) == 0 {
		return body
	}
	out, err := json.Marshal(map[string]any{"object": "list", "data": data})
	if err != nil {
		return body
	}
	return out
}
