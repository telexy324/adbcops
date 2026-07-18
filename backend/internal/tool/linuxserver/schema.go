package linuxserver

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var forbiddenParameterCharacters = strings.NewReplacer(
	";", "", "|", "", "&", "", "`", "", "$", "", ">", "", "<", "", "\n", "", "\r", "",
)

type parameterSchema struct {
	Type                 string                    `json:"type"`
	AdditionalProperties bool                      `json:"additionalProperties"`
	Required             []string                  `json:"required"`
	Properties           map[string]propertySchema `json:"properties"`
}

type propertySchema struct {
	Type      string          `json:"type"`
	Enum      []string        `json:"enum"`
	Minimum   *int64          `json:"minimum"`
	Maximum   *int64          `json:"maximum"`
	Default   json.RawMessage `json:"default"`
	Pattern   string          `json:"pattern"`
	MaxLength int             `json:"maxLength"`
}

func validateParameters(rawSchema, rawParameters json.RawMessage) (map[string]any, error) {
	var schema parameterSchema
	if json.Unmarshal(rawSchema, &schema) != nil || schema.Type != "object" || schema.AdditionalProperties || schema.Properties == nil {
		return nil, ErrInvalidDefinition
	}
	if len(rawParameters) == 0 || string(rawParameters) == "null" {
		rawParameters = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(rawParameters))
	decoder.UseNumber()
	var supplied map[string]any
	if decoder.Decode(&supplied) != nil || supplied == nil {
		return nil, ErrInvalidParameters
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, ErrInvalidParameters
	}
	for name := range supplied {
		if _, exists := schema.Properties[name]; !exists {
			return nil, ErrInvalidParameters
		}
	}
	values := make(map[string]any, len(schema.Properties))
	for name, property := range schema.Properties {
		value, suppliedValue := supplied[name]
		if !suppliedValue && len(property.Default) > 0 {
			defaultDecoder := json.NewDecoder(bytes.NewReader(property.Default))
			defaultDecoder.UseNumber()
			if defaultDecoder.Decode(&value) != nil {
				return nil, ErrInvalidDefinition
			}
			suppliedValue = true
		}
		if !suppliedValue {
			if containsString(schema.Required, name) {
				return nil, ErrInvalidParameters
			}
			continue
		}
		normalized, err := validateParameterValue(value, property)
		if err != nil {
			return nil, err
		}
		values[name] = normalized
	}
	return values, nil
}

func validateParameterValue(value any, schema propertySchema) (any, error) {
	switch schema.Type {
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return nil, ErrInvalidParameters
		}
		integer, err := number.Int64()
		if err != nil || (schema.Minimum != nil && integer < *schema.Minimum) || (schema.Maximum != nil && integer > *schema.Maximum) {
			return nil, ErrInvalidParameters
		}
		return integer, nil
	case "string":
		text, ok := value.(string)
		if !ok || text == "" || containsForbiddenParameterCharacter(text) || (schema.MaxLength > 0 && len(text) > schema.MaxLength) {
			return nil, ErrInvalidParameters
		}
		if len(schema.Enum) > 0 && !containsString(schema.Enum, text) {
			return nil, ErrInvalidParameters
		}
		if schema.Pattern != "" {
			pattern, err := regexp.Compile(schema.Pattern)
			if err != nil {
				return nil, ErrInvalidDefinition
			}
			if !pattern.MatchString(text) {
				return nil, ErrInvalidParameters
			}
		}
		return text, nil
	default:
		return nil, ErrInvalidDefinition
	}
}

func containsForbiddenParameterCharacter(value string) bool {
	return forbiddenParameterCharacters.Replace(value) != value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
