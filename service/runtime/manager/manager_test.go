package manager

import "testing"

func TestRuntimeServiceKeyFormatsKubernetesNames(t *testing.T) {
	got := runtimeServiceKey("Igaoshou.Match_Srv", "v0.6.90-release")
	want := "igaoshou-match-srv:v0-6-90-release"
	if got != want {
		t.Fatalf("runtimeServiceKey() = %q, want %q", got, want)
	}
}
