package skillframework

import (
	"encoding/json"
	"fmt"
)

type schemaDefinition struct {
	Type       string                      `json:"type"`
	Required   []string                    `json:"required"`
	Properties map[string]schemaDefinition `json:"properties"`
	Items      *schemaDefinition           `json:"items"`
}

func ValidateJSONSchema(schemaRaw json.RawMessage, inputRaw json.RawMessage) error {
	if len(inputRaw) == 0 || !json.Valid(inputRaw) {
		return ErrInvalidInput
	}
	var schema schemaDefinition
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return ErrInvalidInput
	}
	var input any
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		return ErrInvalidInput
	}
	if err := validateValue(schema, input, "$"); err != nil {
		return err
	}
	return nil
}

func validateValue(schema schemaDefinition, value any, path string) error {
	if schema.Type == "" {
		return nil
	}
	if !matchesType(schema.Type, value) {
		return fmt.Errorf("%w: %s must be %s", ErrInvalidInput, path, schema.Type)
	}
	switch schema.Type {
	case "object":
		object, _ := value.(map[string]any)
		for _, key := range schema.Required {
			if _, ok := object[key]; !ok {
				return fmt.Errorf("%w: %s.%s is required", ErrInvalidInput, path, key)
			}
		}
		for key, childSchema := range schema.Properties {
			childValue, ok := object[key]
			if !ok {
				continue
			}
			if err := validateValue(childSchema, childValue, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		if schema.Items == nil {
			return nil
		}
		items, _ := value.([]any)
		for index, item := range items {
			if err := validateValue(*schema.Items, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func matchesType(schemaType string, value any) bool {
	switch schemaType {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}
