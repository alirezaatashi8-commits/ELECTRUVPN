package obfuscation

import (
	"encoding/json"
	"testing"

	"github.com/electruvpn/electruvpn/internal/models"
)

func TestApplyRealityBalanced(t *testing.T) {
	stream := models.StreamSettings{
		Network:  models.NetworkTCP,
		Security: models.SecurityReality,
		Reality: &models.RealitySettings{
			Dest: "www.microsoft.com:443", PrivateKey: "keep-me", PublicKey: "pub",
			ServerNames: []string{"www.microsoft.com"},
		},
	}
	ApplyToStream(&stream, ProfileBalanced)
	if stream.Fragment == nil || !stream.Fragment.Enabled {
		t.Fatal("fragment expected")
	}
	if stream.Mux == nil || !stream.Mux.Enabled || !stream.Mux.Padding {
		t.Fatal("mux padding expected")
	}
	if stream.Reality.PrivateKey != "keep-me" {
		t.Fatal("must preserve private key")
	}
}

func TestApplyHy2Aggressive(t *testing.T) {
	st := models.Hysteria2Settings{}
	ApplyToHysteria2(&st, ProfileAggressive)
	if st.ObfsType != "salamander" || st.ObfsPassword == "" {
		t.Fatal("obfs expected")
	}
	if st.MasqueradeType != "proxy" || st.MasqueradeURL == "" {
		t.Fatal("masquerade expected")
	}
}

func TestApplyInboundJSON(t *testing.T) {
	stream := models.StreamSettings{Network: models.NetworkTCP, Security: models.SecurityReality}
	b, _ := json.Marshal(stream)
	_, stOut, labels, err := ApplyToInboundJSON(models.ProtocolVLESS, "{}", string(b), "", ProfileLight)
	if err != nil {
		t.Fatal(err)
	}
	if ParseLabel(labels) != ProfileLight {
		t.Fatalf("label %q", labels)
	}
	var out models.StreamSettings
	if err := json.Unmarshal([]byte(stOut), &out); err != nil {
		t.Fatal(err)
	}
	if out.Fragment == nil || !out.Fragment.Enabled {
		t.Fatal("fragment missing in JSON")
	}
}
