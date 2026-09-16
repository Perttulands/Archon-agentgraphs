package api

import (
	"net/http"
	"testing"
)

func TestFormationsAPIToolMutationErrorsNameTheProblem(t *testing.T) {
	create := func(title, rest string) string {
		return `"createTool":{"profileId":"json.normalize","profileVersion":"1","title":"` + title + `",` + rest + `}`
	}
	for _, test := range []struct {
		name    string
		body    func(h *formationsAPIToolAuthoringHarness) string
		message string
	}{
		{
			name: "store: exact placement needs both coordinates",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(create("Half", `"params":{"mode":"strict"},"placement":{"x":10}`))
			},
			message: "Tool exact placement requires both x and y",
		},
		{
			name: "store: unknown placement predecessor",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(create("Lost", `"params":{"mode":"strict"},"placement":{"predecessorNodeId":"node_missing"}`))
			},
			message: `unknown Tool placement predecessor "node_missing"`,
		},
		{
			name: "store: unknown profile tuple",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(`"createTool":{"profileId":"no.such","profileVersion":"1","title":"Unknown","params":{},"placement":{}}`)
			},
			message: `unknown Tool profile tuple "no.such"@"1"`,
		},
		{
			name: "frame: params are not a scalar object",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(create("Nested", `"params":{"mode":{"deep":true}},"placement":{}`))
			},
			message: "createTool params must be one duplicate-free JSON object of string, boolean, or signed 64-bit integer values",
		},
		{
			name: "frame: a board operation beside the Tool operation",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(`"deleteTool":{"id":"tool_normalize"},"title":"Renamed"`)
			},
			message: "a Tool request carries only createTool, updateTool or deleteTool with expectedRev, layoutExpectation and updatedBy",
		},
		{
			name: "frame: update with nothing to change",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return h.validFrame(`"updateTool":{"id":"tool_normalize"}`)
			},
			message: "updateTool needs title or params",
		},
		{
			name: "frame: unknown layout state",
			body: func(h *formationsAPIToolAuthoringHarness) string {
				return `{"deleteTool":{"id":"tool_normalize"},"expectedRev":` + jsonInt(h.board.Rev) + `,"layoutExpectation":{"state":"stale"},"updatedBy":"agent:api-test"}`
			},
			message: "layoutExpectation state must be absent or present",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newFormationsAPIToolAuthoringHarness(t, true)
			before := readFormationsAPIFile(t, harness.store.BoardPath(harness.slug))
			beforeLayout := readFormationsAPIFile(t, harness.store.LayoutPath(harness.slug))
			response := assertFormationsAPIToolError(t, harness.patch(test.body(harness), harness.board.ETag), http.StatusUnprocessableEntity, "INVALID_TOOL_MUTATION")
			if response.Error.Message != test.message {
				t.Fatalf("message = %q, want %q", response.Error.Message, test.message)
			}
			assertFormationsAPIToolPairBytes(t, harness, before, beforeLayout)
		})
	}
}
