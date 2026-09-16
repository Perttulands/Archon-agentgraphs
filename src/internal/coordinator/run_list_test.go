package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRunListFiltersByBoard(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	for path, want := range map[string]int{"/api/formations/runs": 1, "/api/formations/runs?board=proof": 1, "/api/formations/runs?board=other": 0} {
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var body struct {
			Data []Projection `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Data == nil || len(body.Data) != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if want == 1 && (body.Data[0].RunID != id || body.Data[0].Status != "waiting_human" || len(body.Data[0].WaitingGates) != 1) {
			t.Fatalf("%s: %+v", path, body.Data[0])
		}
	}
}
