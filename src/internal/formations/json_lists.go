package formations

import "encoding/json"

// Documents served as JSON always carry their lists, empty rather than null or
// missing, so a client can index any of them without a guard (archon-n7u.49).

func emptyIfNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func (b BoardDocument) MarshalJSON() ([]byte, error) {
	type document BoardDocument
	out := document(b)
	out.Missions = emptyIfNil(out.Missions)
	out.Formations = emptyIfNil(out.Formations)
	out.Gates = emptyIfNil(out.Gates)
	out.Tools = emptyIfNil(out.Tools)
	out.Ends = emptyIfNil(out.Ends)
	out.Connections = emptyIfNil(out.Connections)
	return json.Marshal(out)
}

func (f FormationNode) MarshalJSON() ([]byte, error) {
	type node FormationNode
	out := node(f)
	out.Inputs = emptyIfNil(out.Inputs)
	out.Outputs = emptyIfNil(out.Outputs)
	out.Slots = emptyIfNil(out.Slots)
	return json.Marshal(out)
}

func (g GateNode) MarshalJSON() ([]byte, error) {
	type node GateNode
	out := node(g)
	out.Kinds = emptyIfNil(out.Kinds)
	return json.Marshal(out)
}

func (t ToolNode) MarshalJSON() ([]byte, error) {
	type node ToolNode
	out := node(t)
	if out.Params == nil {
		out.Params = map[string]any{}
	}
	out.Inputs = emptyIfNil(out.Inputs)
	out.Outputs = emptyIfNil(out.Outputs)
	return json.Marshal(out)
}

func (p ToolPort) MarshalJSON() ([]byte, error) {
	type port ToolPort
	out := port(p)
	out.AcceptedMediaTypes = emptyIfNil(out.AcceptedMediaTypes)
	return json.Marshal(out)
}

func (l LayoutDocument) MarshalJSON() ([]byte, error) {
	type document LayoutDocument
	out := document(l)
	out.Nodes = emptyIfNil(out.Nodes)
	out.Edges = emptyIfNil(out.Edges)
	return json.Marshal(out)
}
