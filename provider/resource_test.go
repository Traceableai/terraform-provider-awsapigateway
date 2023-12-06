package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlatten(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "empty map",
			input:    make(map[string]interface{}),
			expected: make(map[string]interface{}),
		},
		{
			name: "flat json map",
			input: map[string]interface{}{
				"stringKey":   "stringValue",
				"numericKey":  1234,
				"booleanKey":  true,
				"floatingKey": 123.4,
			},
			expected: map[string]interface{}{
				"stringKey":   "stringValue",
				"numericKey":  1234,
				"booleanKey":  true,
				"floatingKey": 123.4,
			},
		},
		{
			name: "nested json map",
			input: map[string]interface{}{
				"string.Key": "string.Value",
				"nestedKey": map[string]interface{}{
					"level1.a": map[string]interface{}{
						"numericKey": 1234,
						"booleanKey": true,
					},
					"stringKey": "StringValue",
				},
				"floatingKey": 123.4,
			},
			expected: map[string]interface{}{
				"string.Key":                    "string.Value",
				"nestedKey.level1.a.numericKey": 1234,
				"nestedKey.level1.a.booleanKey": true,
				"nestedKey.stringKey":           "StringValue",
				"floatingKey":                   123.4,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, Flatten(test.input))
		})
	}
}

func TestFixAccessLogFormatMissingQuotes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "not json",
			input:    "\"key1\": \"value1\", \"key2\", \"key3\"",
			expected: "\"key1\": \"value1\", \"key2\", \"key3\"",
		},
		{
			name:     "flat json: no values with missing quotes",
			input:    "{\"key1\":\"$context.requestTime\", \"key2\":\"$context.path\", \"key3\":\"$context.requestId\"}",
			expected: "{\"key1\":\"$context.requestTime\", \"key2\":\"$context.path\", \"key3\":\"$context.requestId\"}",
		},
		{
			name:     "nested json: no values with missing quotes",
			input:    "{\"key1\": \"$context.path\", \"nested.key\":{\"level1\":\"$context.url\", \"level1b\":\"$context.Status\"}}",
			expected: "{\"key1\": \"$context.path\", \"nested.key\":{\"level1\":\"$context.url\", \"level1b\":\"$context.Status\"}}",
		},
		{
			name:     "flat json: values with missing quotes",
			input:    "{\"key1\":\"$context.requestTime\", \"key2\":$context.path, \"key3\":  $context.requestId}",
			expected: "{\"key1\":\"$context.requestTime\", \"key2\":\"$context.path\", \"key3\":\"$context.requestId\"}",
		},
		{
			name:     "nested json: values with missing quotes",
			input:    "{\"key1\": \"$context.path\"  , \"nested.key\":{ \"level1\":  $context.Status, \"level1b\" :  $context.identity.sourceIp , \"level1c\":\"v\"}}",
			expected: "{\"key1\": \"$context.path\"  , \"nested.key\":{ \"level1\":\"$context.Status\", \"level1b\" :\"$context.identity.sourceIp\" , \"level1c\":\"v\"}}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, fixAccessLogFormatMissingQuotes(test.input))
		})
	}
}
