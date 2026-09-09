package runnerprotocol

import (
	"errors"
	"reflect"
	"testing"
)

func TestV2RejectsV1AndValidatesConcreteCapabilities(t *testing.T) {
	if _, err := Decode([]byte(`{"version":1,"type":"health"}`)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("v1 accepted: %v", err)
	}
	good := Capabilities{OS: "linux", Architecture: "amd64", RunnerVersion: "test", MaxActiveSessions: 1, Engines: []string{"opencode"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Capabilities){func(c *Capabilities) { c.OS = "" }, func(c *Capabilities) { c.Architecture = "" }, func(c *Capabilities) { c.MaxActiveSessions = 0 }, func(c *Capabilities) { c.Engines = []string{"invented"} }, func(c *Capabilities) { c.Engines = []string{"opencode", "opencode"} }} {
		bad := good
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid capabilities accepted")
		}
	}
}

func TestEngineDiscoveryUsesSupportedExecutablesOnly(t *testing.T) {
	var probed []string
	found := DiscoverEngines(func(name string) (string, error) {
		probed = append(probed, name)
		if name == "opencode" {
			return "/bin/opencode", nil
		}
		return "", errors.New("missing")
	})
	if !reflect.DeepEqual(found, []string{"opencode"}) {
		t.Fatal(found)
	}
	if !reflect.DeepEqual(probed, []string{"opencode", "sh"}) {
		t.Fatal(probed)
	}
}
