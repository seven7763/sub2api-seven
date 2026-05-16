package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesToAnthropic_FunctionCallOutput_StringForm covers the legacy
// shape where `output` is a plain string — this was already supported.
func TestResponsesToAnthropic_FunctionCallOutput_StringForm(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-4-7",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"hi"},
			{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"pong"}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 3)

	// 3rd message is the tool_result user message.
	assert.Equal(t, "user", out.Messages[2].Role)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[2].Content, &blocks))
	require.Len(t, blocks, 1)
	assert.Equal(t, "tool_result", blocks[0].Type)
	var got string
	require.NoError(t, json.Unmarshal(blocks[0].Content, &got))
	assert.Equal(t, "pong", got)
}

// TestResponsesToAnthropic_FunctionCallOutput_ArrayForm is the regression test
// for the bug previously logged as
//
//	"json: cannot unmarshal array into Go struct field
//	 ResponsesInputItem.output of type string"
//
// Newer OpenAI Responses clients send `output` as an array of content parts.
// Sub2API must accept this and flatten the text parts into Anthropic's
// tool_result.content (which is a single string).
func TestResponsesToAnthropic_FunctionCallOutput_ArrayForm(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-4-7",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"hi"},
			{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":[
				{"type":"output_text","text":"hello "},
				{"type":"output_text","text":"world"},
				{"type":"input_image","image_url":"data:image/png;base64,AAAA"}
			]}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err, "array-form output must not produce a parse error")
	require.Len(t, out.Messages, 3)

	assert.Equal(t, "user", out.Messages[2].Role)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(out.Messages[2].Content, &blocks))
	require.Len(t, blocks, 1)
	assert.Equal(t, "tool_result", blocks[0].Type)

	var got string
	require.NoError(t, json.Unmarshal(blocks[0].Content, &got))
	assert.Equal(t, "hello world", got, "text parts must be concatenated; non-text parts ignored")
}

// TestResponsesToAnthropic_FunctionCallOutput_SingleObjectForm is a defensive
// case: some clients accidentally send a bare content-part object instead of
// wrapping it in an array. The flatten helper should still extract the text.
func TestResponsesToAnthropic_FunctionCallOutput_SingleObjectForm(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-4-7",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":
				{"type":"output_text","text":"single"}
			}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.NotEmpty(t, out.Messages)

	last := out.Messages[len(out.Messages)-1]
	assert.Equal(t, "user", last.Role)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(last.Content, &blocks))
	require.Len(t, blocks, 1)

	var got string
	require.NoError(t, json.Unmarshal(blocks[0].Content, &got))
	assert.Equal(t, "single", got)
}

// TestResponsesToAnthropic_FunctionCallOutput_EmptyArrayFallsBackToPlaceholder
// ensures a structurally valid but empty array does not crash and instead
// becomes the "(empty)" placeholder used elsewhere.
func TestResponsesToAnthropic_FunctionCallOutput_EmptyArrayFallsBackToPlaceholder(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-opus-4-7",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"ping","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":[]}
		]`),
	}

	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.NotEmpty(t, out.Messages)

	last := out.Messages[len(out.Messages)-1]
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(last.Content, &blocks))
	require.Len(t, blocks, 1)
	var got string
	require.NoError(t, json.Unmarshal(blocks[0].Content, &got))
	assert.Equal(t, "(empty)", got)
}

// TestFlattenResponsesFunctionCallOutput unit-tests the helper itself.
func TestFlattenResponsesFunctionCallOutput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"null", `null`, ""},
		{"plain-string", `"hello"`, "hello"},
		{"empty-string", `""`, ""},
		{"array-output-text", `[{"type":"output_text","text":"ab"},{"type":"output_text","text":"cd"}]`, "abcd"},
		{"array-mixed", `[{"type":"output_text","text":"x"},{"type":"input_image","image_url":"data:..."}]`, "x"},
		{"array-input-text", `[{"type":"input_text","text":"yz"}]`, "yz"},
		{"array-text", `[{"type":"text","text":"qw"}]`, "qw"},
		{"single-object", `{"type":"output_text","text":"solo"}`, "solo"},
		{"single-object-image-only", `{"type":"input_image","image_url":"data:..."}`, ""},
		{"unknown-shape", `123`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenResponsesFunctionCallOutput(json.RawMessage(tc.raw))
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResponsesFunctionCallOutputFromString round-trips a string through the
// helper and back via flattenResponsesFunctionCallOutput.
func TestResponsesFunctionCallOutputFromString(t *testing.T) {
	in := `hello "world" \n`
	raw := responsesFunctionCallOutputFromString(in)
	got := flattenResponsesFunctionCallOutput(raw)
	assert.Equal(t, in, got)
}
